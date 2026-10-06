package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Writer escribe un mensaje JSON compacto por línea. Es seguro para uso
// concurrente: la cancelación puede escribir mientras se envía la petición.
type Writer struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func NewWriter(w io.Writer) *Writer {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Writer{enc: enc}
}

// Write codifica v y lo envía en una sola escritura terminada en '\n'.
func (w *Writer) Write(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(v); err != nil {
		return fmt.Errorf("escribir mensaje al motor: %w", err)
	}
	return nil
}

// Reader lee los mensajes NDJSON del motor. No usa bufio.Scanner: su límite de
// línea (64 KB) es menor que el que admite el motor (1 MiB).
type Reader struct {
	r *bufio.Reader
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(r)}
}

// Read devuelve el siguiente mensaje, ignorando líneas vacías. Al terminar el
// flujo devuelve io.EOF; una última línea sin '\n' todavía se procesa.
func (r *Reader) Read() (*Message, error) {
	for {
		line, err := r.r.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			return decodeMessage(line)
		}
		if err != nil {
			return nil, err
		}
	}
}

func decodeMessage(line []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("línea ilegible del motor: %w", err)
	}
	return &msg, nil
}

// DecodeResult interpreta una respuesta final: si es un error del motor lo
// devuelve como *EngineError; si no, decodifica result en out.
func DecodeResult(msg *Message, out any) error {
	if msg.Error != nil {
		return newEngineError(msg.Error)
	}
	if err := json.Unmarshal(msg.Result, out); err != nil {
		return fmt.Errorf("resultado inesperado del motor: %w", err)
	}
	return nil
}

// DecodeProgress interpreta la notificación "progreso".
func DecodeProgress(msg *Message) (Progreso, error) {
	var p Progreso
	if msg.Method != MethodProgreso {
		return p, fmt.Errorf("notificación desconocida del motor: %q", msg.Method)
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return p, fmt.Errorf("progreso ilegible: %w", err)
	}
	return p, nil
}
