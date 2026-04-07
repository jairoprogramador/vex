package aggregates

import (
	"errors"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

type Project struct {
	id       proVos.ProjectID
	data     proVos.ProjectData
	template comVos.Template
	runtime  proVos.Runtime
}

func NewProject(
	id proVos.ProjectID,
	data proVos.ProjectData,
	template comVos.Template,
	runtime proVos.Runtime,
) (*Project, error) {
	if template.URL() == "" {
		return nil, errors.New("template is required")
	}
	if template.Ref() == "" {
		return nil, errors.New("template ref is required")
	}
	if runtime.Image().Image() == "" {
		return nil, errors.New("runtime image is required")
	}
	if runtime.Image().Tag() == "" {
		return nil, errors.New("runtime image tag is required")
	}
	return &Project{
		id:       id,
		data:     data,
		template: template,
		runtime:  runtime,
	}, nil
}

func (p *Project) IsIDDirty() bool {
	generatedID := proVos.GenerateProjectID(p.data.Name(), p.data.Organization(), p.data.Team())
	if !p.id.Equals(generatedID) {
		p.id = generatedID
		return true
	}
	return false
}

func (p *Project) ID() proVos.ProjectID {
	return p.id
}

func (p *Project) Data() proVos.ProjectData {
	return p.data
}

func (p *Project) Template() comVos.Template {
	return p.template
}

func (p *Project) Runtime() proVos.Runtime {
	return p.runtime
}

func (p *Project) SetTemplate(template comVos.Template) {
	p.template = template
}

func (p *Project) SetRuntime(runtime proVos.Runtime) {
	p.runtime = runtime
}

func HydrateProject(
	id proVos.ProjectID,
	data proVos.ProjectData,
	template comVos.Template,
	runtime proVos.Runtime,
) *Project {
	return &Project{
		id:       id,
		data:     data,
		template: template,
		runtime:  runtime,
	}
}
