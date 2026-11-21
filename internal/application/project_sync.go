package application

import (
	"errors"
	"fmt"
	"strings"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// ProjectChange es un cambio que el portal propone para vexconfig.yaml y que decide qué código se ejecuta: de qué
// repositorio y commit sale el pipeline, o en qué imagen corre.
type ProjectChange struct {
	Field    string
	Current  string
	Proposed string
}

// ChangeApprover enseña esos cambios a la persona y devuelve si los acepta. Sin terminal devuelve false: nadie
// puede aceptarlos.
type ChangeApprover func(changes []ProjectChange) (bool, error)

// ErrPortalChangeNotAccepted: el portal propone cambiar el pipeline o la imagen y nadie lo aceptó.
var ErrPortalChangeNotAccepted = errors.New("el cambio que propone el portal no se aceptó")

// PortalChangeError es ErrPortalChangeNotAccepted con los cambios, para que quien lo lea vea qué se proponía.
type PortalChangeError struct {
	Changes []ProjectChange
}

func (e *PortalChangeError) Error() string {
	var text strings.Builder
	text.WriteString("El portal propone cambiar en vexconfig.yaml de dónde sale lo que se ejecuta, y nadie lo aceptó:")
	for _, change := range e.Changes {
		fmt.Fprintf(&text, "\n  %s: %s → %s", change.Field, change.Current, change.Proposed)
	}
	text.WriteString("\n→ No se cambió nada ni se clonó nada. Si el cambio es tuyo (lo hiciste en el portal), vuelve a ejecutar " +
		"en una terminal y acéptalo, o edita vexconfig.yaml a mano. Si no lo reconoces, no lo aceptes")
	return text.String()
}

func (e *PortalChangeError) Unwrap() error { return ErrPortalChangeNotAccepted }

// sensitiveChanges son los cambios del portal que exigen confirmación. Lo que dice el portal se muestra solo con
// ASCII imprimible: una URL con letras de otro alfabeto no puede pasar por la de siempre.
func sensitiveChanges(project *aggregates.Project, auth *portalclient.AuthoritativeData) []ProjectChange {
	pipeline, image := project.Pipeline(), project.Runtime().Image()
	var changes []ProjectChange
	add := func(field, current, proposed string) {
		if current != proposed {
			changes = append(changes, ProjectChange{Field: field, Current: ExternalText(current, 300), Proposed: ExternalText(proposed, 300)})
		}
	}
	add("pipeline.url", pipeline.URL(), auth.Pipeline.URL)
	add("pipeline.ref", pipeline.Ref(), auth.Pipeline.Ref)
	add("runtime.image", image.Image()+":"+image.Tag(), auth.Pipeline.RuntimeImage+":"+auth.Pipeline.RuntimeTag)
	return changes
}

// ApplyAuthoritative compares the portal-canonical view with the local
// aggregate, mutates the in-memory aggregate through its setters and
// persists the result through projects. It returns whether any field actually changed so
// the caller can decide whether to surface the rewrite to the user.
//
// The aggregate's value objects are immutable, so we rebuild Pipeline,
// Runtime and ProjectData from scratch.
//
// Un cambio de la URL o del ref del pipeline, o de la imagen de runtime, decide qué código corre con las
// credenciales de la persona: no se escribe sin que approve lo acepte. Sin approve, o si no se acepta, devuelve
// *PortalChangeError y no toca nada. Los demás campos se adoptan sin preguntar.
func ApplyAuthoritative(
	projects proPor.ProjectRepository, project *aggregates.Project, auth *portalclient.AuthoritativeData, approve ChangeApprover,
) (bool, error) {
	currentPipeline := project.Pipeline()
	currentRuntime := project.Runtime()
	currentData := project.Data()

	pipelineChanged := currentPipeline.URL() != auth.Pipeline.URL || currentPipeline.Ref() != auth.Pipeline.Ref
	runtimeChanged := currentRuntime.Image().Image() != auth.Pipeline.RuntimeImage || currentRuntime.Image().Tag() != auth.Pipeline.RuntimeTag
	dataChanged := currentData.URL() != auth.Project.RepoURL || currentData.Ref() != auth.Project.RepoRef
	idChanged := auth.Project.ID != "" && auth.Project.ID != project.ID().String()

	if !pipelineChanged && !runtimeChanged && !dataChanged && !idChanged {
		return false, nil
	}

	if changes := sensitiveChanges(project, auth); len(changes) > 0 {
		accepted := false
		if approve != nil {
			var err error
			if accepted, err = approve(changes); err != nil {
				return false, err
			}
		}
		if !accepted {
			return false, &PortalChangeError{Changes: changes}
		}
	}

	// Se valida antes de mutar nada: un ID inválido no debe dejar el agregado a medias.
	var newID proVos.ProjectID
	if idChanged {
		var err error
		newID, err = proVos.ParseProjectUUID(auth.Project.ID)
		if err != nil {
			return false, fmt.Errorf("id de proyecto inválido del portal: %w", err)
		}
	}

	if pipelineChanged {
		newPipeline, err := comVos.NewPipeline(auth.Pipeline.URL, auth.Pipeline.Ref)
		if err != nil {
			return false, fmt.Errorf("rebuild pipeline VO: %w", err)
		}
		project.SetPipeline(newPipeline)
	}

	if runtimeChanged {
		newImage, err := comVos.NewImage(auth.Pipeline.RuntimeImage + ":" + auth.Pipeline.RuntimeTag)
		if err != nil {
			return false, fmt.Errorf("rebuild runtime image VO: %w", err)
		}
		newRuntime := proVos.NewRuntime(
			proVos.WithImage(newImage),
			proVos.WithVolumes(currentRuntime.Volumes()),
			proVos.WithEnv(currentRuntime.Env()),
			proVos.WithArgs(currentRuntime.Args()),
		)
		project.SetRuntime(newRuntime)
	}

	if idChanged {
		project.WithID(newID)
	}

	if dataChanged {
		newData, err := proVos.NewProjectData(
			currentData.Name(),
			currentData.Organization(),
			currentData.Team(),
			currentData.Description(),
			auth.Project.RepoURL,
			auth.Project.RepoRef,
		)
		if err != nil {
			return false, fmt.Errorf("rebuild project data VO: %w", err)
		}
		project.SetData(newData)
	}

	if err := projects.Save(project); err != nil {
		return false, fmt.Errorf("save project: %w", err)
	}
	return true, nil
}
