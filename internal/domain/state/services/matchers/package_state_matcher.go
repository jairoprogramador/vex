package matchers

import (
	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type PackageStateMatcher struct {
	BaseMatcher
}

func (m *PackageStateMatcher) Match(entry *aggregates.StateEntry, current vos.CurrentStateFingerprints) bool {
	if !m.matchCommon(entry, current) {
		return false
	}
	return entry.Code().Equals(current.Code())
}
