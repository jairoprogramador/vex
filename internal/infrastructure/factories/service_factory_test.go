package factories

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	app "github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/config"
)

func TestBuildRunner_ElModoLocalEsElPorDefecto(t *testing.T) {
	tests := []struct {
		name       string
		mode       config.ExecutionMode
		wantRemote bool
	}{
		{"local explícito", config.ModeLocal, false},
		{"sin modo (ModeUnset) cae en local", config.ModeUnset, false},
		{"el modo por defecto es local", config.DefaultMode, false},
		{"remoto solo si se pide explícitamente", config.ModeRemote, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, err := NewServiceFactory().BuildRunner(tt.mode, true, true)

			assert.NoError(t, err)
			_, isRemote := runner.(*app.RemoteExecutorService)
			_, isLocal := runner.(*app.LocalExecutorService)
			assert.Equal(t, tt.wantRemote, isRemote)
			assert.Equal(t, !tt.wantRemote, isLocal)
		})
	}
}

type recordingNotifier struct{ warnings []string }

func (n *recordingNotifier) Info(string)         {}
func (n *recordingNotifier) Warn(message string) { n.warnings = append(n.warnings, message) }

// El catálogo firmado solo se puede dejar de lado en un build de desarrollo, a propósito y con ruido.
func TestTemplateCatalog_SoloUnBuildDeDesarrolloPuedePrescindirDelCatalogoFirmado(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	restore := buildChannel
	t.Cleanup(func() { buildChannel = restore })

	t.Run("sin pedirlo al enlazar, el binario es un release", func(t *testing.T) {
		// B7: un `go build` sin más no abre atajos de desarrollo.
		if buildChannel != "release" || devBuild() {
			t.Fatalf("buildChannel = %q", buildChannel)
		}
		t.Setenv(envUnsignedTemplates, "1")
		if templateCatalog(&recordingNotifier{}) == nil {
			t.Fatal("el build por defecto no puede prescindir del catálogo firmado")
		}
	})
	t.Run("por defecto se compara con el catálogo firmado, sin avisos", func(t *testing.T) {
		t.Setenv(envUnsignedTemplates, "")
		notify := &recordingNotifier{}
		if templateCatalog(notify) == nil || len(notify.warnings) != 0 {
			t.Fatalf("catálogo nil o avisos: %v", notify.warnings)
		}
	})
	t.Run("un release ignora la variable y lo dice", func(t *testing.T) {
		buildChannel = "release"
		t.Setenv(envUnsignedTemplates, "1")
		notify := &recordingNotifier{}
		if templateCatalog(notify) == nil {
			t.Fatal("un release no puede prescindir del catálogo firmado")
		}
		if len(notify.warnings) != 1 {
			t.Fatalf("avisos = %v", notify.warnings)
		}
	})
	t.Run("un build de desarrollo puede, con aviso", func(t *testing.T) {
		buildChannel = "dev"
		t.Setenv(envUnsignedTemplates, "1")
		notify := &recordingNotifier{}
		if templateCatalog(notify) != nil {
			t.Fatal("se esperaba que no hubiera catálogo")
		}
		if len(notify.warnings) != 1 || !strings.Contains(notify.warnings[0], "MODO DE DESARROLLO") {
			t.Fatalf("avisos = %v", notify.warnings)
		}
	})
}

// El catálogo propio nunca se guarda donde el verificado, ni lleva verificador.
func TestTemplateRepository_ElCatalogoPropioAvisa(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	factory := &serviceFactory{}

	t.Setenv(envOwnCatalog, "")
	notify := &recordingNotifier{}
	factory.templateRepository(notify)
	if len(notify.warnings) != 0 {
		t.Fatalf("el catálogo oficial no avisa: %v", notify.warnings)
	}

	t.Setenv(envOwnCatalog, "/tmp/propio.json")
	factory.templateRepository(notify)
	if len(notify.warnings) != 1 || !strings.Contains(notify.warnings[0], "sin verificar su firma") {
		t.Fatalf("avisos = %v", notify.warnings)
	}
}
