package mapper

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/jairoprogramador/vex/internal/infrastructure/project/dto"
)

const baseConfig = `project:
  id: 9d8d4777-5707-43b3-a636-d6f8433b7a34
  name: demo
  team: t
  organization: o
  url: https://github.com/x/y
  ref: main
pipeline:
  url: https://github.com/x/pipe
  ref: main
runtime:
  image: local/runtime:v1
`

func parseConfig(t *testing.T, text string) dto.FDConfigDTO {
	t.Helper()
	var config dto.FDConfigDTO
	if err := yaml.Unmarshal([]byte(text), &config); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return config
}

func TestToDomainSinCloud(t *testing.T) {
	t.Parallel()

	project, err := ToDomain(parseConfig(t, baseConfig))
	if err != nil {
		t.Fatalf("ToDomain: %v", err)
	}
	if !project.Cloud().IsZero() {
		t.Fatalf("sin bloque cloud se esperaba el valor cero, got %+v", project.Cloud())
	}
	if ToDto(project).Cloud != nil {
		t.Fatal("ToDto no debe inventar el bloque cloud")
	}
}

func TestCloudIdaYVuelta(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"con provider": "cloud:\n  provider: azure\n  credentials: portal\n",
		"sin provider": "cloud:\n  credentials: env\n",
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			original := parseConfig(t, baseConfig+block)

			project, err := ToDomain(original)
			if err != nil {
				t.Fatalf("ToDomain: %v", err)
			}
			back := ToDto(project)

			if !reflect.DeepEqual(original.Cloud, back.Cloud) {
				t.Fatalf("cloud: got %+v, want %+v", back.Cloud, original.Cloud)
			}
			out, err := yaml.Marshal(back)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if reparsed := parseConfig(t, string(out)); !reflect.DeepEqual(original.Cloud, reparsed.Cloud) {
				t.Fatalf("el YAML no conserva el bloque:\n%s", out)
			}
			if original.Cloud.Provider == "" && strings.Contains(string(out), "provider:") {
				t.Fatalf("un provider ausente no debe escribirse:\n%s", out)
			}
		})
	}
}

func TestToDomainCloudInvalido(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"sin credentials":         "cloud:\n  provider: azure\n",
		"credentials inexistente": "cloud:\n  credentials: vault\n",
		"provider inválido":       "cloud:\n  provider: Azure!\n  credentials: portal\n",
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ToDomain(parseConfig(t, baseConfig+block)); err == nil {
				t.Fatal("se esperaba un error")
			}
		})
	}
}
