package mapper

import (
	"errors"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
)

// RequestInputJSON es el contrato JSON binario-compatible con el RequestInput
// que `vexd run` (vex-engine) deserializa desde --input / VEX_REQUEST_INPUT /
// stdin. Lo definimos localmente para respetar la regla de oro: vex no importa
// nada de vex-engine. El shape se mantiene en sync manualmente; cualquier cambio
// debe reflejarse en `internal/application/dto/request_input.go` del engine.
type RequestInputJSON struct {
	SchemaVersion int                `json:"schema_version"`
	Project       ProjectInputJSON   `json:"project"`
	Pipeline      PipelineInputJSON  `json:"pipeline"`
	Execution     ExecutionInputJSON `json:"execution"`
}

type ProjectInputJSON struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Team string `json:"team"`
	Org  string `json:"organization"`
	Url  string `json:"url"`
	Ref  string `json:"ref"`
}

type PipelineInputJSON struct {
	Url string `json:"url"`
	Ref string `json:"ref"`
}

type ExecutionInputJSON struct {
	Step         string `json:"step"`
	Environment  string `json:"environment,omitempty"`
	RuntimeImage string `json:"runtime_image,omitempty"`
	RuntimeTag   string `json:"runtime_tag,omitempty"`
}

// CurrentSchemaVersion es el número que el CLI emite en cada RequestInput.
// El engine valida igualdad estricta con su propia versión soportada.
//
// La 2 es el contrato de la spec 16: sin `--mode` y sin los seis
// `--step-*-endpoint`, con el destino del estado pasado como configuración
// explícita. Subirla aquí es lo que impide que un `vex` viejo hable con un
// `vexd` nuevo o al revés —por contrato, no por memoria de nadie—: la
// desigualdad estricta sale con exit code 2 y un mensaje que nombra la versión.
//
// La forma del DTO no cambió entre la 1 y la 2; lo que cambió es lo que el motor
// espera ADEMÁS del DTO. Por eso la versión sube sin que se mueva un solo campo.
const CurrentSchemaVersion = 2

// ToRequestInput convierte un Project de dominio + el step y el env solicitados
// en el JSON DTO que `vexd run` espera. Es el adaptador que reemplaza al viejo
// argv "<step> <env>" del modo local Docker.
func ToRequestInput(project *aggregates.Project, step, environment string) (RequestInputJSON, error) {
	if project == nil {
		return RequestInputJSON{}, errors.New("request input mapper: project is nil")
	}
	if step == "" {
		return RequestInputJSON{}, errors.New("request input mapper: step is required")
	}

	data := project.Data()
	pipeline := project.Pipeline()
	runtime := project.Runtime()

	return RequestInputJSON{
		SchemaVersion: CurrentSchemaVersion,
		Project: ProjectInputJSON{
			Id:   project.ID().String(),
			Name: data.Name(),
			Team: data.Team(),
			Org:  data.Organization(),
			Url:  data.URL(),
			Ref:  data.Ref(),
		},
		Pipeline: PipelineInputJSON{
			Url: pipeline.URL(),
			Ref: pipeline.Ref(),
		},
		Execution: ExecutionInputJSON{
			Step:         step,
			Environment:  environment,
			RuntimeImage: runtime.Image().Image(),
			RuntimeTag:   runtime.Image().Tag(),
		},
	}, nil
}
