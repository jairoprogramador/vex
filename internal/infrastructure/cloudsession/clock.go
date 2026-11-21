// Package cloudsession mantiene en disco un token de nube de vida corta: lo escribe de forma atómica en un
// directorio privado y lo renueva antes de que venza. No conoce el portal (recibe de dónde sacar el token) ni
// ninguna nube (el archivo se llama igual para todas).
package cloudsession

import "time"

// Clock es el tiempo que ve el renovador; se inyecta para probarlo sin esperas.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock es el reloj real.
type SystemClock struct{}

func (SystemClock) Now() time.Time                         { return time.Now() }
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
