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
