package application

// ExecutionPresenter muestra al usuario el avance de una ejecución. La aplicación
// no imprime: decide qué comunicar y delega cómo.
type ExecutionPresenter interface {
	// Info anuncia una etapa de preparación (clonar, construir la imagen).
	Info(message string)
	// Warn avisa de algo que no impide continuar.
	Warn(message string)
	// Event muestra el avance en vivo del motor.
	Event(event EngineEvent)
	// Result muestra el resumen final del intento.
	Result(result AttemptResult)
	// Failure muestra la salida de los comandos que fallaron y orienta sobre qué hacer después. outputs puede
	// venir vacío si no se pudo leer.
	Failure(attemptID string, outputs []CommandOutput)
}
