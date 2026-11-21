package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

func TestSavePreservesModeAndWritesNewID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, configFileName)
	original := "project:\n  id: old-id\n  name: demo\n  team: t\n  organization: o\n  url: u\nmode: remote\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	id, _ := proVos.NewProjectID("9d8d4777-5707-43b3-a636-d6f8433b7a34")
	data, err := proVos.NewProjectData("demo", "o", "t", "", "https://github.com/x/y", "main")
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := comVos.NewPipeline("https://github.com/x/pipe", "main")
	if err != nil {
		t.Fatal(err)
	}
	image, err := comVos.NewImage("local/runtime:v1")
	if err != nil {
		t.Fatal(err)
	}
	project, err := aggregates.NewProject(id, data, pipeline, proVos.NewRuntime(proVos.WithImage(image)))
	if err != nil {
		t.Fatal(err)
	}

	if err := NewYAMLProjectRepository(dir).Save(project); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "mode: remote") {
		t.Fatalf("mode se perdió:\n%s", got)
	}
	if !strings.Contains(string(got), "9d8d4777-5707-43b3-a636-d6f8433b7a34") {
		t.Fatalf("el id nuevo no se guardó:\n%s", got)
	}
}

const configWithCloud = `project:
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
cloud:
  provider: azure
  credentials: portal
`

func TestLoadSaveLoadConservaCloudYMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		mode     string
		wantMode bool
	}{
		{name: "con mode", mode: "mode: remote\n", wantMode: true},
		{name: "sin mode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, configFileName)
			if err := os.WriteFile(path, []byte(configWithCloud+c.mode), 0o644); err != nil {
				t.Fatal(err)
			}
			repo := NewYAMLProjectRepository(dir)

			loaded, err := repo.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if err := repo.Save(loaded); err != nil {
				t.Fatalf("Save: %v", err)
			}
			reloaded, err := repo.Load()
			if err != nil {
				t.Fatalf("Load tras Save: %v", err)
			}

			if reloaded.Cloud() != loaded.Cloud() || reloaded.Cloud().Provider() != "azure" || !reloaded.Cloud().IsPortal() {
				t.Fatalf("cloud cambió: antes %+v, después %+v", loaded.Cloud(), reloaded.Cloud())
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), "cloud:\n    provider: azure\n    credentials: portal\n") {
				t.Fatalf("cloud se perdió:\n%s", got)
			}
			if strings.Contains(string(got), "mode:") != c.wantMode {
				t.Fatalf("mode: se esperaba presente=%v:\n%s", c.wantMode, got)
			}
			if c.wantMode && !strings.Contains(string(got), "mode: remote") {
				t.Fatalf("mode cambió:\n%s", got)
			}
		})
	}
}
