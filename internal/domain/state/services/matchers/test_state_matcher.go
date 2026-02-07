package matchers

import (
	"time"

	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type TestStateMatcher struct {
	BaseMatcher
	Policy vos.CachePolicy
}

func (m *TestStateMatcher) Match(entry *aggregates.StateEntry, current vos.CurrentStateFingerprints) bool {
	if !m.matchCommon(entry, current) {
		return false
	}

	expirationTime := entry.CreatedAt().Add(m.Policy.TTL())
	if time.Now().After(expirationTime) {
		return false
	}

	return entry.Code().Equals(current.Code())
}
