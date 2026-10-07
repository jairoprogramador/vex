package application

import "errors"

const MessageProjectNotInitialized = "project not initialized. Please run 'vex init' first"

var (
	// ErrEnvironmentRequired indica que se pidió un paso sin ambiente: el motor
	// necesita saber en cuál ejecutar.
	ErrEnvironmentRequired = errors.New("falta el ambiente")

	// ErrAttemptFailed indica que el pipeline corrió y terminó con un paso fallido.
	// El detalle ya se mostró al usuario; quien llama solo decide el código de salida.
	ErrAttemptFailed = errors.New("la ejecución terminó con errores")

	// ErrAttemptCanceled indica que la ejecución se canceló antes de terminar.
	ErrAttemptCanceled = errors.New("la ejecución fue cancelada")
)
