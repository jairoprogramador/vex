package services

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

var variableInterpolationRegex = regexp.MustCompile(`\$\{var\.`)

type VariableResolver struct {
	interpolator ports.Interpolator
}

func NewVariableResolver(interpolator ports.Interpolator) ports.VariableResolver {
	return &VariableResolver{interpolator: interpolator}
}

func (vr *VariableResolver) Resolve(initialVars, varsToResolve vos.VariableSet) (vos.VariableSet, error) {
	unresolvedVars := vos.NewVariableSet()
	finalResolvedSet := vos.NewVariableSet()
	for _, v := range varsToResolve {
		if variableInterpolationRegex.MatchString(v.Value()) {
			unresolvedVars.Add(v)
		} else {
			finalResolvedSet.Add(v)
		}
	}

	if len(unresolvedVars) == 0 {
		return varsToResolve, nil
	}

	varsForInterpolation := initialVars.Clone()
	varsForInterpolation.AddAll(finalResolvedSet)

	maxPasses := len(unresolvedVars) + 1
	for pass := 0; pass < maxPasses; pass++ {
		if len(unresolvedVars) == 0 {
			break
		}

		madeProgress := false
		varsStillUnresolved := vos.NewVariableSet()

		for _, unresolvedVar := range unresolvedVars {
			interpolatedValue, err := vr.interpolator.Interpolate(unresolvedVar.Value(), varsForInterpolation)
			if err != nil {
				varsStillUnresolved.Add(unresolvedVar)
				continue
			}

			resolvedVar, _ := vos.NewOutputVar(unresolvedVar.Name(), interpolatedValue, unresolvedVar.IsShared())
			finalResolvedSet.Add(resolvedVar)
			varsForInterpolation.Add(resolvedVar)
			madeProgress = true
		}

		unresolvedVars = varsStillUnresolved

		if !madeProgress {
			var missingVarNames []string
			for _, v := range unresolvedVars {
				missingVarNames = append(missingVarNames, v.Name())
			}
			return nil, fmt.Errorf("dependencia circular o variable faltante detectada. No se pudieron resolver: %s", strings.Join(missingVarNames, ", "))
		}
	}

	if len(unresolvedVars) > 0 {
		var missingVarNames []string
		for _, v := range unresolvedVars {
			missingVarNames = append(missingVarNames, v.Name())
		}
		return nil, fmt.Errorf("no se pudieron resolver todas las variables. Faltantes: %s", strings.Join(missingVarNames, ", "))
	}

	return finalResolvedSet, nil
}
