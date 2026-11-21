package application

// ExecutionPresenter muestra al usuario el avance de una ejecución. La aplicación
// no imprime: decide qué comunicar y delega cómo.
type ExecutionPresenter interface {
	// Info anuncia una etapa de preparación (clonar, construir la imagen).
	Info(message string)
	// Warn avisa de algo que no impide continuar.
	Warn(message string)
	// CloudConnected avisa de que el intento corre con una conexión de nube del portal y deja las instrucciones
	// con que el adaptador de esa nube explica sus errores típicos.
	CloudConnected(connection CloudNotice)
	// Event muestra el avance en vivo del motor.
	Event(event EngineEvent)
	// Result muestra el resumen final del intento.
	Result(result AttemptResult)
	// Failure muestra la salida de los comandos que fallaron y orienta sobre qué hacer después. outputs puede
	// venir vacío si no se pudo leer.
	Failure(attemptID string, outputs []CommandOutput)
}

// CloudNotice es lo que el presentador necesita saber de la conexión de nube de un intento. No lleva datos de
// la nube (suscripción, grupo de recursos): son opacos para la aplicación.
type CloudNotice struct {
	Environment  string
	Contract     ContractID
	ConnectionID string
	Hints        []ErrorHint
}
