package ports

import (
	"context"

	"github.com/jairoprogramador/vex/internal/domain/definition/aggregates"
)

// PlanBuilder es responsable de cargar y ensamblar una definición de plan de ejecución completa.
type PlanBuilder interface {
	Build(ctx context.Context, templatePath, stepName, envName string) (*aggregates.ExecutionPlanDefinition, error)
}
