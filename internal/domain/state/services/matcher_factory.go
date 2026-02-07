package services

import (
	"fmt"

	"github.com/jairoprogramador/vex/internal/domain/state/ports"
	"github.com/jairoprogramador/vex/internal/domain/state/services/matchers"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

func NewStateMatcherFactory(tableName string, policy vos.CachePolicy) (ports.StateMatcher, error) {
	switch tableName {
	case vos.StepTest:
		return &matchers.TestStateMatcher{Policy: policy}, nil
	case vos.StepSupply:
		return &matchers.SupplyStateMatcher{}, nil
	case vos.StepPackage:
		return &matchers.PackageStateMatcher{}, nil
	case vos.StepDeploy:
		return &matchers.DeployStateMatcher{}, nil
	default:
		return nil, fmt.Errorf("no state matcher found for name: %s", tableName)
	}
}
