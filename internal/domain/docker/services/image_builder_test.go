package services_test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	comVos "github.com/jairoprogramador/vex-client/internal/domain/common/vos"
	"github.com/jairoprogramador/vex-client/internal/domain/docker/services"
	proAgg "github.com/jairoprogramador/vex-client/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex-client/internal/domain/project/vos"
)

// mockProject es un helper para crear un agregado de proyecto para los tests.
// Construye el agregado manualmente para evitar la lógica de validación de los constructores
// y así poder probar los servicios de forma aislada.
func mockProject(t *testing.T, image, imageTag string) *proAgg.Project {
	data, err := proVos.NewProjectData("test-project", "org", "team", "")
	require.NoError(t, err)
	id := proVos.GenerateProjectID(data.Name(), data.Organization(), data.Team())
	template, err := comVos.NewTemplate("http://test.com/repo.git", "main")
	require.NoError(t, err)

	// Creamos los VOs manualmente para el test
	imageObj, _ := comVos.NewImage(image, imageTag)
	runtimeObj := proVos.NewRuntime(proVos.WithImage(imageObj))
	project := proAgg.HydrateProject(id, data, template, runtimeObj)
	return project
}

func TestImageBuilderService_CreateOptions(t *testing.T) {
	builder := services.NewImageBuilder()

	t.Run("should create options with linux specific args", func(t *testing.T) {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			t.Skip("Skipping linux specific test on non-linux OS")
		}

		// Arrange
		project := mockProject(t, "my-image", "latest")

		// Act
		opts, err := builder.CreateOptions(project)

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, opts.Image().Name())
		assert.Equal(t, "latest", opts.Image().Tag())
		assert.Equal(t, "$(id -g)", opts.Args()["DEV_GID"])
	})

	t.Run("should create options without linux specific args on other OS", func(t *testing.T) {
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			t.Skip("Skipping non-linux specific test on linux OS")
		}

		// Arrange
		project := mockProject(t, "my-image", "latest")

		// Act
		opts, err := builder.CreateOptions(project)

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, opts.Image().Name())
		assert.Equal(t, "latest", opts.Image().Tag())
		_, exists := opts.Args()["DEV_GID"]
		assert.False(t, exists, "DEV_GID should not be present on non-linux OS")
		assert.Equal(t, "1.0.0", opts.Args()["vex-client_VERSION"])
	})

	t.Run("should return error if image name is invalid", func(t *testing.T) {
		// Arrange
		project := mockProject(t, "my-image", "")

		// Act
		_, err := builder.CreateOptions(project)

		// Assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "image tag cannot be empty")
	})
}

func TestImageBuilderService_BuildCommand(t *testing.T) {
	builder := services.NewImageBuilder()

	t.Run("should generate a correct build command", func(t *testing.T) {
		// Arrange
		project := mockProject(t, "my-image", "latest")
		opts, err := builder.CreateOptions(project)
		require.NoError(t, err)

		// Act
		command, err := builder.BuildCommand(opts)

		// Assert
		require.NoError(t, err)
		assert.Contains(t, command, "docker build")
		assert.Contains(t, command, "-t "+opts.Image().FullName())
		assert.Contains(t, command, " .")
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			assert.Contains(t, command, "--build-arg DEV_GID=$(id -g)")
		}
	})
}
