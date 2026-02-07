package aggregates

import (
	"path/filepath"

	"github.com/jairoprogramador/vex/internal/domain/workspace/vos"
)

type Workspace struct {
	rootPath     vos.RootPath
	projectName  vos.ProjectName
	templateName vos.TemplateName
}

func NewWorkspace(
	rootPath vos.RootPath,
	projectName vos.ProjectName,
	templateName vos.TemplateName) (*Workspace, error) {

	return &Workspace{
		rootPath:     rootPath,
		projectName:  projectName,
		templateName: templateName,
	}, nil
}

func (w *Workspace) TemplatePath() string {
	return filepath.Join(w.rootPath.Path(), "repositories", w.templateName.String())
}

func (w *Workspace) StepTemplatePath(stepNameDir string) string {
	return filepath.Join(w.TemplatePath(), "steps", stepNameDir)
}

func (w *Workspace) VarsTemplatePath(environment, stepName string) string {
	fileNameStep, err := vos.NewFileName(stepName, "yaml")
	if err != nil {
		return ""
	}
	return filepath.Join(w.TemplatePath(), "variables", environment, fileNameStep.String())
}

func (w *Workspace) WorkspacePath() string {
	return filepath.Join(w.rootPath.Path(), w.projectName.String(), w.templateName.String())
}

func (w *Workspace) VarsDirPath() string {
	return filepath.Join(w.WorkspacePath(), "vars")
}

func (w *Workspace) VarsFilePath(scopeName, stepName string) string {
	fileName, err := vos.NewVarsFileName(stepName)
	if err != nil {
		return ""
	}
	return filepath.Join(w.VarsDirPath(), scopeName, fileName.String())
}

func (w *Workspace) WorkdirPath() string {
	return filepath.Join(w.WorkspacePath(), "workdir")
}

func (w *Workspace) ScopeWorkdirPath(scope string, stepName string) string {
	return filepath.Join(w.WorkdirPath(), scope, stepName)
}

func (w *Workspace) StateDirPath() string {
	return filepath.Join(w.WorkspacePath(), "state")
}

func (w *Workspace) StateTablePath(stateName string) (string, error) {
	fileName, err := vos.NewStateFileName(stateName)
	if err != nil {
		return "", err
	}
	return filepath.Join(w.StateDirPath(), fileName.String()), nil
}
