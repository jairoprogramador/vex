// Package engine es el adaptador que conecta la CLI con vex-engine: ejecuta el
// motor en un contenedor descartable y le habla JSON-RPC por stdin/stdout.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

const (
	// Tiempo que se le da al motor para cancelar con orden antes de matar el contenedor.
	defaultCancelGrace = 10 * time.Second
	// stderr del contenedor solo explica fallos internos; no hace falta guardarlo entero.
	maxStderrBytes = 64 * 1024
)

// DockerEngine implementa application.EngineClient lanzando `docker run --rm -i`.
// Usa exec con argumentos (sin shell), así que ningún valor se interpreta.
type DockerEngine struct {
	newCommand  func(args ...string) *exec.Cmd
	newName     func() string
	cancelGrace time.Duration
}

var _ application.EngineClient = (*DockerEngine)(nil)

func NewDockerEngine() *DockerEngine {
	return &DockerEngine{
		newCommand:  func(args ...string) *exec.Cmd { return exec.Command("docker", args...) },
		newName:     randomContainerName,
		cancelGrace: defaultCancelGrace,
	}
}

func (d *DockerEngine) Attempt(
	ctx context.Context,
	spec application.ContainerSpec,
	req application.AttemptRequest,
	onEvent func(application.EngineEvent),
) (application.AttemptResult, error) {
	rpc := protocol.NewRequest("1", protocol.MethodIntentar, toAttemptParams(req), req.Secrets)

	reply, err := d.call(ctx, spec, rpc, func(msg *protocol.Message) {
		if event, ok := toEngineEvent(msg); ok && onEvent != nil {
			onEvent(event)
		}
	})
	if err != nil {
		return application.AttemptResult{}, err
	}

	var result protocol.Resultado
	if err := protocol.DecodeResult(reply.message, &result); err != nil {
		return application.AttemptResult{}, translateError(err, reply.stderr)
	}
	return toAttemptResult(result), nil
}

func (d *DockerEngine) Logs(
	ctx context.Context,
	spec application.ContainerSpec,
	req application.LogsRequest,
) ([]application.CommandOutput, error) {
	rpc := protocol.NewRequest("1", protocol.MethodLogs, toLogsParams(req), nil)

	reply, err := d.call(ctx, spec, rpc, nil)
	if err != nil {
		return nil, err
	}

	var logs protocol.Logs
	if err := protocol.DecodeResult(reply.message, &logs); err != nil {
		return nil, translateError(err, reply.stderr)
	}
	return toCommandOutputs(logs), nil
}

type reply struct {
	message *protocol.Message
	stderr  string
}

// call ejecuta un contenedor, le envía una petición y devuelve la respuesta final.
// Las notificaciones intermedias se entregan a onNotification en orden.
func (d *DockerEngine) call(
	ctx context.Context,
	spec application.ContainerSpec,
	req protocol.Request,
	onNotification func(*protocol.Message),
) (reply, error) {
	if err := ctx.Err(); err != nil {
		return reply{}, err
	}

	name := d.newName()
	cmd := d.newCommand(dockerRunArgs(name, spec)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return reply{}, fmt.Errorf("preparar stdin de docker: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return reply{}, fmt.Errorf("preparar stdout de docker: %w", err)
	}
	stderr := &cappedBuffer{limit: maxStderrBytes}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return reply{}, fmt.Errorf("iniciar docker: %w", err)
	}

	writer := protocol.NewWriter(stdin)
	stopWatching := d.watchCancel(ctx, name, writer)

	// Si la escritura falla el proceso ya murió; leer igual explica por qué.
	_ = writer.Write(req)

	final, readErr := readUntilResponse(protocol.NewReader(stdout), onNotification)
	if readErr != nil {
		d.kill(name)
	}
	stopWatching()
	_ = stdin.Close()
	waitErr := cmd.Wait()

	if readErr != nil {
		return reply{}, fmt.Errorf("leer respuesta del motor: %w", readErr)
	}
	if final == nil {
		return reply{}, &application.EngineError{
			Kind:    application.EngineDidNotRespond,
			Message: fmt.Sprintf("el contenedor terminó sin responder (%v)", waitErr),
			Stderr:  strings.TrimSpace(stderr.String()),
		}
	}
	return reply{message: final, stderr: strings.TrimSpace(stderr.String())}, nil
}

func readUntilResponse(r *protocol.Reader, onNotification func(*protocol.Message)) (*protocol.Message, error) {
	var final *protocol.Message
	for {
		msg, err := r.Read()
		if errors.Is(err, io.EOF) {
			return final, nil
		}
		if err != nil {
			return nil, err
		}
		if msg.IsNotification() {
			if onNotification != nil {
				onNotification(msg)
			}
			continue
		}
		final = msg
	}
}

// watchCancel avisa al motor cuando ctx se cancela y, si no termina a tiempo,
// mata el contenedor. La función devuelta detiene la vigilancia.
func (d *DockerEngine) watchCancel(ctx context.Context, name string, w *protocol.Writer) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		_ = w.Write(protocol.NewCancel())
		select {
		case <-done:
		case <-time.After(d.cancelGrace):
			d.kill(name)
		}
	}()

	return func() {
		close(done)
		<-finished
	}
}

func (d *DockerEngine) kill(name string) {
	_ = d.newCommand("kill", name).Run()
}

func dockerRunArgs(name string, spec application.ContainerSpec) []string {
	args := []string{"run", "--rm", "-i", "--name", name}
	for _, env := range spec.Env {
		args = append(args, "-e", env.Name+"="+env.Value)
	}
	for _, m := range spec.Mounts {
		args = append(args, "--mount", mountFlag(m))
	}
	return append(args, spec.Image)
}

// mountFlag arma el valor de --mount, que docker lee como CSV: un campo con
// coma o comillas se cita para que una ruta rara no inyecte opciones.
func mountFlag(m application.Mount) string {
	fields := []string{"type=bind", "source=" + m.Source, "target=" + m.Target}
	if m.ReadOnly {
		fields = append(fields, "readonly")
	}
	for i, f := range fields {
		if strings.ContainsAny(f, `,"`) {
			fields[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
		}
	}
	return strings.Join(fields, ",")
}

func randomContainerName() string {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		// crypto/rand no falla en plataformas soportadas; el nombre solo debe ser único.
		return fmt.Sprintf("vex-%d", time.Now().UnixNano())
	}
	return "vex-" + hex.EncodeToString(suffix)
}

// cappedBuffer guarda hasta limit bytes y descarta el resto sin devolver error,
// para no frenar al proceso que escribe.
type cappedBuffer struct {
	buf   []byte
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - len(c.buf); room > 0 {
		c.buf = append(c.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return string(c.buf) }
