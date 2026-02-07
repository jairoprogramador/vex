package matchers

import (
	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type DeployStateMatcher struct {
	BaseMatcher
}

func (m *DeployStateMatcher) Match(entry *aggregates.StateEntry, current vos.CurrentStateFingerprints) bool {
	if !m.matchCommon(entry, current) {
		return false
	}
	return entry.Code().Equals(current.Code()) &&
		entry.Environment().String() == current.Environment().String()
}
