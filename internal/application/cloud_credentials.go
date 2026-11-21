package application

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
)

// ErrPortalCredentialsUnavailable: el proyecto pide las credenciales del portal y este vex no sabe obtenerlas.
var ErrPortalCredentialsUnavailable = errors.New(
	`cloud.credentials es "portal", pero esta compilación de vex no puede obtener credenciales del portal`)

// CloudCredentials obtiene la identidad de nube con la que corre un intento.
// La sesión debe cerrarse al terminar: detiene la renovación y borra el token del disco.
type CloudCredentials interface {
	Open(ctx context.Context, req CloudSessionRequest) (CloudSession, error)
}

type CloudSessionRequest struct {
	ProjectID   string
	Environment string
	AttemptHint string // para la auditoría; vacío si aún no existe
	Commit      string
	// CloudSteps son los pasos que reciben el token (cloud_steps del bootstrap del pipeline); vacío = ninguno.
	CloudSteps []string
	// Connection es la conexión ya consultada, para no preguntar dos veces; nil si aún no se consultó.
	Connection *CloudConnectionInfo
}

type CloudSession interface {
	// Env es el contrato de la nube: solo identificadores y rutas, nada secreto.
	Env() map[string]string
	// Mounts es el directorio del token, de solo lectura.
	Mounts() []Mount
	Close() error
}

// StepAwareSession la implementa la sesión cuyo token solo existe mientras corre un paso de nube. Es opcional:
// la sesión de las credenciales del usuario no tiene nada que poner ni quitar.
type StepAwareSession interface {
	// StepRunning avisa de que el motor está en step. Si es un paso de nube, al volver el token ya está
	// disponible; si no, ya no lo está. Se puede llamar varias veces por paso. Un error que cumple
	// errors.Is(err, ErrCloudTokenDenied) es definitivo: la sesión o la conexión dejaron de valer.
	StepRunning(ctx context.Context, step string) error
	// StepFinished avisa de que el paso terminó: al volver, el token ya no está disponible.
	StepFinished(step string) error
}

// CloudAccessLostError: a mitad de la ejecución el portal dejó de emitir la credencial de nube. La ejecución se
// detiene: seguir solo produciría fallos de autenticación en la nube.
type CloudAccessLostError struct {
	Environment string
	Step        string
	// Err es la causa clasificada; nunca lleva el token.
	Err error
}

func (e *CloudAccessLostError) Unwrap() error { return e.Err }

func (e *CloudAccessLostError) Error() string {
	stopped := fmt.Sprintf("La ejecución se detuvo en el paso «%s»", e.Step)
	switch {
	case errors.Is(e.Err, ErrPortalSessionInvalid):
		return stopped + ": tu sesión de vex venció o fue revocada.\n→ Ejecuta `vex login` y vuelve a intentarlo"
	case errors.Is(e.Err, ErrCloudConnectionInactive):
		return stopped + fmt.Sprintf(": la conexión de nube de '%s' ya no está activa.\n→ %s",
			e.Environment, bootstrapAdvice(e.Environment, ""))
	}
	return stopped + ": el portal ya no te permite obtener la credencial de nube de este proyecto.\n" +
		"→ Comprueba que sigues siendo miembro del proyecto y vuelve a intentarlo"
}

// stepWatcher sigue los pasos del motor para que la sesión ponga y quite el token. El motor puede descartar
// eventos si nadie lee su salida a tiempo, así que no depende de recibirlos todos: cualquier evento de un paso
// dice en cuál está el motor (ver StepAwareSession).
type stepWatcher struct {
	session     StepAwareSession // nil si la sesión no distingue pasos
	environment string
	warn        func(string)
	// stop detiene el intento cuando el acceso a la nube se pierde de forma definitiva.
	stop   func()
	lost   *CloudAccessLostError
	warned string // paso del que ya se avisó
}

func newStepWatcher(session CloudSession, environment string, warn func(string), stop func()) *stepWatcher {
	aware, _ := session.(StepAwareSession)
	return &stepWatcher{session: aware, environment: environment, warn: warn, stop: stop}
}

// observe se llama con cada evento del motor, antes de mostrarlo: el token existe antes de anunciar que el paso
// empezó y ya no existe cuando se anuncia que terminó.
func (w *stepWatcher) observe(ctx context.Context, event EngineEvent) {
	if w.session == nil || event.Step == "" || w.lost != nil {
		return
	}
	if event.Kind == StepFinished {
		if err := w.session.StepFinished(event.Step); err != nil {
			w.warn(fmt.Sprintf("No se pudo retirar el token de nube tras el paso %q: %v", event.Step, err))
		}
		return
	}
	err := w.session.StepRunning(ctx, event.Step)
	switch {
	case err == nil || ctx.Err() != nil:
	case errors.Is(err, ErrCloudTokenDenied):
		w.lost = &CloudAccessLostError{Environment: w.environment, Step: event.Step, Err: err}
		w.stop()
	case w.warned != event.Step:
		w.warned = event.Step
		w.warn(fmt.Sprintf("No se pudo obtener el token de nube para el paso %q: %v. El paso fallará.", event.Step, err))
	}
}

// envCredentials es la fuente de siempre: las credenciales las pone el usuario en runtime.run.envs y llegan por
// Workspace.Secrets, así que la sesión no aporta nada.
type envCredentials struct{}

func (envCredentials) Open(context.Context, CloudSessionRequest) (CloudSession, error) {
	return emptySession{}, nil
}

type emptySession struct{}

func (emptySession) Env() map[string]string { return nil }
func (emptySession) Mounts() []Mount        { return nil }
func (emptySession) Close() error           { return nil }

// selectCredentials elige la fuente de credenciales según el proyecto. portal es nil si no se inyectó ninguna.
func selectCredentials(project *aggregates.Project, portal CloudCredentials) (CloudCredentials, error) {
	switch {
	case !project.Cloud().IsPortal():
		return envCredentials{}, nil
	case portal == nil:
		return nil, ErrPortalCredentialsUnavailable
	default:
		return portal, nil
	}
}

// cloudRun es un intento o un rollback visto desde la sesión: recibe el contexto del intento, la sesión abierta
// y por dónde avisar de cada evento del motor.
type cloudRun func(ctx context.Context, session CloudSession, observe func(EngineEvent)) (AttemptResult, error)

// withCloudSession abre la sesión de credenciales, ejecuta run con ella y la cierra en cuanto run vuelve: el
// directorio del token no sobrevive al motor, pase lo que pase (éxito, error o Ctrl+C). Si a mitad de camino el
// portal deja de emitir la credencial, cancela el intento y devuelve un *CloudAccessLostError.
func withCloudSession(
	ctx context.Context, credentials CloudCredentials, req CloudSessionRequest, presenter ExecutionPresenter, run cloudRun,
) (AttemptResult, error) {
	session, err := credentials.Open(ctx, req)
	if err != nil {
		return AttemptResult{}, fmt.Errorf("obtener la credencial de nube: %w", err)
	}
	defer closeCloudSession(session, presenter)

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	watcher := newStepWatcher(session, req.Environment, presenter.Warn, stop)

	result, err := run(runCtx, session, func(event EngineEvent) { watcher.observe(runCtx, event) })
	if watcher.lost != nil {
		return AttemptResult{}, watcher.lost
	}
	return result, err
}

// closeCloudSession cierra la sesión sin alterar el resultado del intento: un fallo al limpiar solo se avisa.
func closeCloudSession(session CloudSession, presenter ExecutionPresenter) {
	if err := session.Close(); err != nil {
		presenter.Warn(fmt.Sprintf("No se pudo limpiar la credencial de nube: %v", err))
	}
}

// withSessionMounts devuelve spec con los montajes de la sesión al final, sin modificar el original.
func withSessionMounts(spec ContainerSpec, session CloudSession) ContainerSpec {
	spec.Mounts = append(slices.Clone(spec.Mounts), session.Mounts()...)
	return spec
}

// mergeSecrets devuelve base + extra; gana extra. Nunca modifica los argumentos.
func mergeSecrets(base, extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(extra))
	maps.Copy(merged, base)
	maps.Copy(merged, extra)
	return merged
}
