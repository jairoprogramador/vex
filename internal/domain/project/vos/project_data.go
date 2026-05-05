package vos

import "errors"

const (
	DefaultProjectTeam         = "shikigami"
	DefaultProjectDescription  = "mi despliegue con vex"
	DefaultProjectOrganization = "vex"
	DefaultStack               = "springboot"
	DefaultPlatform            = "azure"
)

type ProjectData struct {
	name         string
	team         string
	description  string
	organization string
	url          string
	ref          string
}

func NewProjectData(name, organization, team, description, url, ref string) (ProjectData, error) {
	if name == "" {
		return ProjectData{}, errors.New("name is required")
	}
	if organization == "" {
		return ProjectData{}, errors.New("organization is required")
	}
	if team == "" {
		return ProjectData{}, errors.New("team is required")
	}
	if description == "" {
		description = DefaultProjectDescription
	}
	return ProjectData{
		name:         name,
		team:         team,
		description:  description,
		organization: organization,
		url:          url,
		ref:          ref,
	}, nil
}

func (p ProjectData) Name() string         { return p.name }
func (p ProjectData) Team() string         { return p.team }
func (p ProjectData) Description() string  { return p.description }
func (p ProjectData) Organization() string { return p.organization }
func (p ProjectData) URL() string          { return p.url }
func (p ProjectData) Ref() string          { return p.ref }
