package services

import (
	"path/filepath"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/ports"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type StateManager struct {
	stateRepo ports.StateRepository
}

func NewStateManager(stateRepo ports.StateRepository) *StateManager {
	return &StateManager{
		stateRepo: stateRepo,
	}
}

func (sm *StateManager) HasStateChanged(
	filePath string,
	currentState vos.CurrentStateFingerprints,
	policy vos.CachePolicy,
) (bool, error) {

	stateTable, err := sm.stateRepo.Get(filePath)
	if err != nil {
		return true, err
	}
	if stateTable == nil {
		return true, nil
	}

	match, err := sm.findMatch(stateTable, currentState, policy)
	if err != nil {
		return true, err
	}

	return match == nil, nil
}

func (sm *StateManager) UpdateState(
	filePath string,
	currentState vos.CurrentStateFingerprints,
) error {
	stateTable, err := sm.stateRepo.Get(filePath)
	if err != nil {
		return err
	}
	if stateTable == nil {
		fileName := filepath.Base(filePath)
		tableName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		stateTable = aggregates.NewStateTable(tableName)
	}

	newEntry := aggregates.NewStateEntry(
		currentState.Code(),
		currentState.Instruction(),
		currentState.Vars(),
		currentState.Environment(),
	)
	stateTable.AddEntry(newEntry)

	return sm.stateRepo.Save(filePath, stateTable)
}

func (sm *StateManager) findMatch(
	st *aggregates.StateTable,
	currentState vos.CurrentStateFingerprints,
	policy vos.CachePolicy,
) (*aggregates.StateEntry, error) {
	matcher, err := NewStateMatcherFactory(st.Name(), policy)
	if err != nil {
		return nil, err
	}
	for _, entry := range st.Entries() {
		if matcher.Match(entry, currentState) {
			return entry, nil
		}
	}
	return nil, nil
}
