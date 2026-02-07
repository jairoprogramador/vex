package state

import (
	"time"

	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type StateTableDTO struct {
	Name    string
	Entries []*StateEntryDTO
}

type StateEntryDTO struct {
	Code        string
	Instruction string
	Environment string
	Vars        string
	CreatedAt   time.Time
}

func toStateTableDTO(aggregate *aggregates.StateTable) *StateTableDTO {
	if aggregate == nil {
		return nil
	}

	dtoEntries := make([]*StateEntryDTO, 0, len(aggregate.Entries()))
	for _, entry := range aggregate.Entries() {
		dtoEntries = append(dtoEntries, &StateEntryDTO{
			Code:        entry.Code().String(),
			Instruction: entry.Instruction().String(),
			Environment: entry.Environment().String(),
			Vars:        entry.Vars().String(),
			CreatedAt:   entry.CreatedAt(),
		})
	}

	return &StateTableDTO{
		Name:    aggregate.Name(),
		Entries: dtoEntries,
	}
}

func fromDTO(dto *StateTableDTO) *aggregates.StateTable {
	if dto == nil {
		return nil
	}

	domainEntries := make([]*aggregates.StateEntry, 0, len(dto.Entries))
	for _, dtoEntry := range dto.Entries {

		codeFp, err := vos.NewFingerprint(dtoEntry.Code)
		if err != nil {
			codeFp = vos.Fingerprint{}
		}
		instFp, err := vos.NewFingerprint(dtoEntry.Instruction)
		if err != nil {
			instFp = vos.Fingerprint{}
		}
		varsFp, err := vos.NewFingerprint(dtoEntry.Vars)
		if err != nil {
			varsFp = vos.Fingerprint{}
		}
		env, err := vos.NewEnvironment(dtoEntry.Environment)
		if err != nil {
			env = vos.Environment{}
		}

		entry := aggregates.NewStateEntry(codeFp, instFp, varsFp, env)
		entry.SetCreatedAt(dtoEntry.CreatedAt)
		domainEntries = append(domainEntries, entry)
	}
	return aggregates.LoadStateTable(dto.Name, domainEntries)
}
