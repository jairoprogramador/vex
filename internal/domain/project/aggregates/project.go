package aggregates

import (
	"github.com/jairoprogramador/vex/internal/domain/project/vos"
)

type Project struct {
	id               vos.ProjectID
	data             vos.ProjectData
	templateRepo     vos.TemplateRepository
	projectLocalPath string
	isIDDirty        bool
}

func NewProject(
	id vos.ProjectID,
	data vos.ProjectData,
	templateRepo vos.TemplateRepository,
	projectLocalPath string) *Project {
	return &Project{
		id:               id,
		data:             data,
		templateRepo:     templateRepo,
		projectLocalPath: projectLocalPath,
	}
}

func (p *Project) SyncID() bool {
	generatedID := vos.GenerateProjectID(p.data.Name(), p.data.Organization(), p.data.Team())
	if !p.id.Equals(generatedID) {
		p.id = generatedID
		p.isIDDirty = true
		return true
	}
	return false
}

func (p *Project) IsIDDirty() bool {
	return p.isIDDirty
}

func (p *Project) ID() vos.ProjectID {
	return p.id
}

func (p *Project) Data() vos.ProjectData {
	return p.data
}

func (p *Project) TemplateRepo() vos.TemplateRepository {
	return p.templateRepo
}
