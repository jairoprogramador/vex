package aggregates

import (
	"errors"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

type Project struct {
	id       proVos.ProjectID
	data     proVos.ProjectData
	pipeline comVos.Pipeline
	runtime  proVos.Runtime
	cloud    proVos.Cloud
}

func NewProject(
	id proVos.ProjectID,
	data proVos.ProjectData,
	pipeline comVos.Pipeline,
	runtime proVos.Runtime,
) (*Project, error) {
	if pipeline.URL() == "" {
		return nil, errors.New("pipeline url is required")
	}
	if pipeline.Ref() == "" {
		return nil, errors.New("pipeline ref is required")
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
		pipeline: pipeline,
		runtime:  runtime,
	}, nil
}

// WithID reemplaza el ID del proyecto. Solo lo usa la adopción del ID asignado por el portal.
func (p *Project) WithID(id proVos.ProjectID) {
	p.id = id
}

func (p *Project) ID() proVos.ProjectID {
	return p.id
}

func (p *Project) Data() proVos.ProjectData {
	return p.data
}

func (p *Project) Pipeline() comVos.Pipeline {
	return p.pipeline
}

func (p *Project) Runtime() proVos.Runtime {
	return p.runtime
}

func (p *Project) SetPipeline(pipeline comVos.Pipeline) {
	p.pipeline = pipeline
}

func (p *Project) SetRuntime(runtime proVos.Runtime) {
	p.runtime = runtime
}

// Cloud dice de dónde salen las credenciales de nube; es el valor cero si vexconfig.yaml no trae el bloque cloud.
func (p *Project) Cloud() proVos.Cloud {
	return p.cloud
}

func (p *Project) SetCloud(cloud proVos.Cloud) {
	p.cloud = cloud
}

func (p *Project) SetData(data proVos.ProjectData) {
	p.data = data
}
