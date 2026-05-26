package services_test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/docker/services"
	proAgg "github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// mockProject crea un Project de prueba a partir de un imageSpec (e.g. "my-image:latest"
// para imágenes de registry, o "Dockerfile" / "docker/MyDockerfile" para builds locales).
func mockProject(t *testing.T, imageSpec string) *proAgg.Project {
	t.Helper()
	data, err := proVos.NewProjectData("test-project", "org", "team", "", "https://github.com/test/repo.git", "")
	require.NoError(t, err)
	id := proVos.GenerateProjectID(data.Name(), data.Organization(), data.Team())
	pipeline, err := comVos.NewPipeline("http://test.com/repo.git", "main")
	require.NoError(t, err)

	imageObj, err := comVos.NewImage(imageSpec)
	require.NoError(t, err)
	runtimeObj := proVos.NewRuntime(proVos.WithImage(imageObj))
	project := proAgg.HydrateProject(id, data, pipeline, runtimeObj)
	return project
}

func TestImageBuilderService_CreateOptions(t *testing.T) {
	builder := services.NewImageBuilder()

	t.Run("should create options with linux specific args", func(t *testing.T) {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			t.Skip("Skipping linux specific test on non-linux OS")
		}

		// Arrange: proyecto con Dockerfile por defecto
		project := mockProject(t, "Dockerfile")

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

		// Arrange: proyecto con Dockerfile por defecto
		project := mockProject(t, "Dockerfile")

		// Act
		opts, err := builder.CreateOptions(project)

		// Assert
		require.NoError(t, err)
		assert.NotEmpty(t, opts.Image().Name())
		assert.Equal(t, "latest", opts.Image().Tag())
		_, exists := opts.Args()["DEV_GID"]
		assert.False(t, exists, "DEV_GID should not be present on non-linux OS")
	})

	t.Run("should set dockerfile path for default dockerfile", func(t *testing.T) {
		// Arrange
		project := mockProject(t, "Dockerfile")

		// Act
		opts, err := builder.CreateOptions(project)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "Dockerfile", opts.DockerfilePath())
	})

	t.Run("should set dockerfile path for custom dockerfile", func(t *testing.T) {
		// Arrange: Dockerfile en una sub-ruta
		project := mockProject(t, "docker/MyDockerfile")

		// Act
		opts, err := builder.CreateOptions(project)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "docker/MyDockerfile", opts.DockerfilePath())
	})
}

func TestImageBuilderService_BuildCommand(t *testing.T) {
	builder := services.NewImageBuilder()

	t.Run("should generate a correct build command for default dockerfile", func(t *testing.T) {
		// Arrange
		project := mockProject(t, "Dockerfile")
		opts, err := builder.CreateOptions(project)
		require.NoError(t, err)

		// Act
		command, err := builder.BuildCommand(opts)

		// Assert
		require.NoError(t, err)
		assert.Contains(t, command, "docker build")
		assert.Contains(t, command, "-t "+opts.Image().FullName())
		assert.Contains(t, command, "-f Dockerfile .")
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			assert.Contains(t, command, "--build-arg DEV_GID=$(id -g)")
		}
	})

	t.Run("should generate build command with custom dockerfile path", func(t *testing.T) {
		// Arrange: Dockerfile en sub-directorio
		project := mockProject(t, "docker/MyDockerfile")
		opts, err := builder.CreateOptions(project)
		require.NoError(t, err)

		// Act
		command, err := builder.BuildCommand(opts)

		// Assert
		require.NoError(t, err)
		assert.Contains(t, command, "-f docker/MyDockerfile docker")
	})
}
