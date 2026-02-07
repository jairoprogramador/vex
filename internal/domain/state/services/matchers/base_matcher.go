package matchers

import (
	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type BaseMatcher struct{}

func (b *BaseMatcher) matchCommon(entry *aggregates.StateEntry, current vos.CurrentStateFingerprints) bool {
	return entry.Instruction().Equals(current.Instruction()) &&
		entry.Vars().Equals(current.Vars())
}
