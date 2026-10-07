package application

import (
	"context"
	"errors"
	"fmt"
)

// Lo que comparten quienes ejecutan un pipeline (un intento, un rollback): qué significa el resultado y cómo se
// cuenta un fallo.

// cancelAware convierte en cancelación el error que una herramienta devuelve cuando se pulsó Ctrl+C: durante el
// clonado, el build o el arranque llega como un error cualquiera de esas herramientas, pero para quien llama es
// una cancelación. Un intento que ya falló sigue siendo un fallo.
func cancelAware(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil && !errors.Is(err, ErrAttemptFailed) {
		return ErrAttemptCanceled
	}
	return err
}

// logsReader es lo que hace falta del motor para mostrar la salida de un fallo.
type logsReader interface {
	Logs(ctx context.Context, spec ContainerSpec, req LogsRequest) ([]CommandOutput, error)
}

// reportOutcome convierte el resultado de un intento en el error que ve quien llama. Si falló, muestra la salida
// de los comandos fallidos —el progreso no la trae— y orienta sobre qué hacer después.
func reportOutcome(ctx context.Context, engine logsReader, presenter ExecutionPresenter, spec ContainerSpec, result AttemptResult) error {
	switch result.Status {
	case AttemptSucceeded:
		return nil
	case AttemptCanceled:
		return ErrAttemptCanceled
	}

	outputs, err := engine.Logs(ctx, spec, LogsRequest{AttemptID: result.AttemptID, OnlyFailed: true})
	if err != nil {
		// El fallo del intento es lo importante; no se tapa con el de los logs.
		presenter.Warn(fmt.Sprintf("No se pudo leer la salida de los comandos fallidos: %v", err))
		presenter.Failure(result.AttemptID, nil)
		return ErrAttemptFailed
	}
	presenter.Failure(result.AttemptID, outputs)
	return ErrAttemptFailed
}
