package ports

import "github.com/jairoprogramador/vex/internal/domain/execution/vos"

type FileProcessor interface {
	Process(absPathsFiles []string, vars vos.VariableSet) error
	Restore() error
}
