package matchers

import (
	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type SupplyStateMatcher struct {
	BaseMatcher
}

func (m *SupplyStateMatcher) Match(entry *aggregates.StateEntry, current vos.CurrentStateFingerprints) bool {
	if !m.matchCommon(entry, current) {
		return false
	}
	return entry.Environment().String() == current.Environment().String()
}
