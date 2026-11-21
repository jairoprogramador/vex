package application

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	arqAgg "github.com/jairoprogramador/vex/internal/domain/architecture/aggregates"
	arqPor "github.com/jairoprogramador/vex/internal/domain/architecture/ports"
	arqVos "github.com/jairoprogramador/vex/internal/domain/architecture/vos"
	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

type fakeLevels struct{}

func (fakeLevels) GetLevels() ([]arqVos.Level, error) {
	return []arqVos.Level{arqVos.NewLevel("básico", 0), arqVos.NewLevel("medio", 1), arqVos.NewLevel("crítico", 2)}, nil
}

type fakeQuestions struct{}

func (fakeQuestions) GetQuestions() ([]*arqAgg.Question, error) {
	question, err := arqAgg.NewQuestion("¿Cuánto?", []arqVos.Point{
		arqVos.NewPoint(1, 1, "poco", ""), arqVos.NewPoint(2, 1, "algo", ""), arqVos.NewPoint(3, 1, "mucho", ""),
	})
	return []*arqAgg.Question{question}, err
}

type fakeTemplates struct {
	unit arqVos.ExecutionUnit
	err  error
}

func (f fakeTemplates) GetExecutionUnit(arqVos.QueryTemplate) (arqVos.ExecutionUnit, error) {
	return f.unit, f.err
}

type fakeGit struct{}

func (fakeGit) RemoteURL(context.Context, string) (string, error) {
	return "https://github.com/acme/app.git", nil
}
func (fakeGit) CurrentRef(context.Context, string) (string, error) { return "main", nil }

type fakeInput struct{}

func (fakeInput) Ask(_, defaultValue string) (string, error)        { return defaultValue, nil }
func (fakeInput) AskSelect(string, []string, []string) (int, error) { return 0, nil }

const catalogCommit = "0123456789abcdef0123456789abcdef01234567"

var catalogImage = "docker.io/jairoprogramador/node:1.0.0@sha256:" + strings.Repeat("a", 64)

func catalogUnit(t *testing.T, contract string) arqVos.ExecutionUnit {
	t.Helper()
	image, err := comVos.NewImage(catalogImage)
	require.NoError(t, err)
	pipeline, err := comVos.NewPipeline("https://github.com/jairoprogramador/vex-tpl.git", catalogCommit)
	require.NoError(t, err)
	return arqVos.NewExecutionUnit(image, pipeline).WithContract(contract)
}

func runInit(t *testing.T, templates arqPor.TemplateRepository) (*stubProjectRepo, *fakePresenter, error) {
	t.Helper()
	repo, notify := &stubProjectRepo{}, &fakePresenter{}
	service := NewInitializeService("app", t.TempDir(), repo, fakeInput{}, nil, fakeLevels{}, fakeQuestions{},
		templates, fakeGit{}, NewCloudAdapters(fakeCloudAdapter{contract: "nube/v1"}), notify)
	return repo, notify, service.Run(context.Background(), false)
}

func TestSEC25_InitEscribeSHAyDigest(t *testing.T) {
	repo, _, err := runInit(t, fakeTemplates{unit: catalogUnit(t, "nube/v1")})

	require.NoError(t, err)
	require.NotNil(t, repo.saved)
	assert.Equal(t, catalogCommit, repo.saved.Pipeline().Ref())
	assert.Equal(t, catalogImage, repo.saved.Runtime().Image().Spec())
	assert.True(t, repo.saved.Runtime().Image().Pinned())
}

func TestInit_PlantillaConContratoEscribeCloud(t *testing.T) {
	repo, _, err := runInit(t, fakeTemplates{unit: catalogUnit(t, "nube/v1")})

	require.NoError(t, err)
	assert.Equal(t, "nube", repo.saved.Cloud().Provider())
	assert.True(t, repo.saved.Cloud().IsPortal())
}

func TestInit_PlantillaSinContratoNoEscribeCloud(t *testing.T) {
	repo, _, err := runInit(t, fakeTemplates{unit: catalogUnit(t, "")})

	require.NoError(t, err)
	assert.True(t, repo.saved.Cloud().IsZero())
}

func TestInit_ContratoDesconocidoNoCreaElProyecto(t *testing.T) {
	repo, _, err := runInit(t, fakeTemplates{unit: catalogUnit(t, "otra/v9")})

	require.ErrorContains(t, err, `"otra/v9"`)
	assert.ErrorContains(t, err, "actualiza vex")
	assert.Zero(t, repo.saveCalls, "no nace un proyecto sin forma de obtener credenciales")
}

func TestInit_SinCatalogoUsaLaPlantillaGenericaYLoAvisa(t *testing.T) {
	repo, notify, err := runInit(t, fakeTemplates{err: arqPor.ErrCatalogUnavailable})

	require.NoError(t, err)
	assert.Equal(t, comVos.DefaultPipelineRef, repo.saved.Pipeline().Ref())
	assert.True(t, repo.saved.Cloud().IsZero())
	require.Len(t, notify.warnings, 1)
	assert.Contains(t, notify.warnings[0], "plantilla genérica sin ref fijado")
}

func TestInit_CatalogoConFirmaInvalidaNoCaeALaGenerica(t *testing.T) {
	for _, cause := range []error{arqPor.ErrCatalogSignature, arqPor.ErrCatalogRollback} {
		repo, _, err := runInit(t, fakeTemplates{err: cause})

		require.ErrorIs(t, err, cause)
		assert.Zero(t, repo.saveCalls)
	}
}

func runArq(t *testing.T, opts []projectOption, unit arqVos.ExecutionUnit) (*stubProjectRepo, error) {
	t.Helper()
	repo := &stubProjectRepo{loaded: newProject(t, opts...), existsBool: true}
	service := NewArchitectureService(fakeQuestions{}, fakeLevels{}, fakeTemplates{unit: unit}, repo, fakeInput{},
		NewCloudAdapters(fakeCloudAdapter{contract: "nube/v1"}))
	return repo, service.Run()
}

func TestArq_ReconciliaElBloqueCloudConLaPlantilla(t *testing.T) {
	t.Run("plantilla con contrato añade el bloque y fija ref e imagen", func(t *testing.T) {
		repo, err := runArq(t, nil, catalogUnit(t, "nube/v1"))
		require.NoError(t, err)
		assert.True(t, repo.saved.Cloud().IsPortal())
		assert.Equal(t, "nube", repo.saved.Cloud().Provider())
		assert.Equal(t, catalogCommit, repo.saved.Pipeline().Ref())
		assert.Equal(t, catalogImage, repo.saved.Runtime().Image().Spec())
	})
	t.Run("no pisa credentials: env", func(t *testing.T) {
		repo, err := runArq(t, []projectOption{withCloud(t, "", proVos.CredentialsEnv)}, catalogUnit(t, "nube/v1"))
		require.NoError(t, err)
		assert.Equal(t, proVos.CredentialsEnv, repo.saved.Cloud().Credentials())
	})
	t.Run("plantilla sin contrato quita el portal", func(t *testing.T) {
		repo, err := runArq(t, []projectOption{withCloud(t, "", proVos.CredentialsPortal)}, catalogUnit(t, ""))
		require.NoError(t, err)
		assert.True(t, repo.saved.Cloud().IsZero())
	})
	t.Run("contrato desconocido no guarda nada", func(t *testing.T) {
		repo, err := runArq(t, nil, catalogUnit(t, "otra/v9"))
		require.Error(t, err)
		assert.Zero(t, repo.saveCalls)
	})
}

func TestCloudAdapters_Supports(t *testing.T) {
	adapters := NewCloudAdapters(fakeCloudAdapter{contract: "nube/v1"})
	assert.True(t, adapters.Supports("nube/v1"))
	assert.False(t, adapters.Supports("nube/v2"))
	assert.False(t, adapters.Supports("NUBE/v1"))
	assert.False(t, adapters.Supports(""))
}
