package services_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/docker/services"
	docVos "github.com/jairoprogramador/vex/internal/domain/docker/vos"
	proAgg "github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// mockProjectWithVolumesAndEnv es un helper para crear un proyecto con volúmenes y variables de entorno.
func mockProjectWithVolumesAndEnv(t *testing.T) *proAgg.Project {
	data, err := proVos.NewProjectData("test-project", "org", "team", "", "https://github.com/test/repo.git", "")
	require.NoError(t, err)
	id := proVos.GenerateProjectID(data.Name(), data.Organization(), data.Team())

	pipeline, err := comVos.NewPipeline("http://test.com/repo.git", "main")
	require.NoError(t, err)

	image, err := comVos.NewImage("my-image:latest")
	require.NoError(t, err)

	// Creamos volúmenes
	vol1, err := proVos.NewVolume("/host/path1", "/container/path1")
	require.NoError(t, err)
	vol2, err := proVos.NewVolume("/host/path2", "/container/path2")
	require.NoError(t, err)
	volumes := []proVos.Volume{vol1, vol2}

	// Creamos variables de entorno
	env1, err := proVos.NewEnvVar("VAR1", "value1")
	require.NoError(t, err)
	env2, err := proVos.NewEnvVar("VAR2", "value2")
	require.NoError(t, err)
	envVars := []proVos.EnvVar{env1, env2}

	// Creamos argumentos
	arg1, err := proVos.NewArgument("ARG1", "value1")
	require.NoError(t, err)
	arg2, err := proVos.NewArgument("ARG2", "value2")
	require.NoError(t, err)
	args := []proVos.Argument{arg1, arg2}

	runtimeObj := proVos.NewRuntime(
		proVos.WithImage(image),
		proVos.WithVolumes(volumes),
		proVos.WithEnv(envVars),
		proVos.WithArgs(args),
	)

	project := proAgg.HydrateProject(id, data, pipeline, runtimeObj)
	return project
}

// mockProjectWithoutVolumesAndEnv es un helper para crear un proyecto sin volúmenes ni variables de entorno.
func mockProjectWithoutVolumesAndEnv(t *testing.T) *proAgg.Project {
	data, err := proVos.NewProjectData("test-project", "org", "team", "", "https://github.com/test/repo.git", "")
	require.NoError(t, err)
	id := proVos.GenerateProjectID(data.Name(), data.Organization(), data.Team())

	pipeline, err := comVos.NewPipeline("http://test.com/repo.git", "main")
	require.NoError(t, err)

	image, err := comVos.NewImage("my-image:latest")
	require.NoError(t, err)

	runtimeObj := proVos.NewRuntime(proVos.WithImage(image))

	project := proAgg.HydrateProject(id, data, pipeline, runtimeObj)
	return project
}

func TestContainerBuilderService_CreateOptions(t *testing.T) {
	builder := services.NewContainerBuilder()

	t.Run("should create options with volumes and env vars", func(t *testing.T) {
		// Arrange
		project := mockProjectWithVolumesAndEnv(t)
		command := "test sand"
		imageName, err := docVos.NewImageName("simple-image", "v1")

		require.NoError(t, err)

		// Act
		opts, err := builder.CreateOptions(project, command, imageName)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "simple-image:v1", opts.Image().FullName())
		assert.Equal(t, command, opts.Command())
		assert.True(t, opts.RemoveOnExit())
		assert.Len(t, opts.Volumes(), 2)
		assert.Equal(t, "/container/path1", opts.Volumes()["/host/path1"])
		assert.Equal(t, "/container/path2", opts.Volumes()["/host/path2"])
		assert.Len(t, opts.EnvVars(), 2)
		assert.Equal(t, "value1", opts.EnvVars()["VAR1"])
		assert.Equal(t, "value2", opts.EnvVars()["VAR2"])
	})

	t.Run("should create options without volumes and env vars", func(t *testing.T) {
		// Arrange
		project := mockProjectWithoutVolumesAndEnv(t)
		command := "test sand"

		imageName, err := docVos.NewImageName("simple-image", "v1")
		require.NoError(t, err)

		// Act
		opts, err := builder.CreateOptions(project, command, imageName)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "simple-image:v1", opts.Image().FullName())
		assert.Equal(t, command, opts.Command())
		assert.True(t, opts.RemoveOnExit())
		assert.Len(t, opts.Volumes(), 0)
		assert.Len(t, opts.EnvVars(), 0)
	})

}

func TestContainerBuilderService_BuildCommand(t *testing.T) {
	builder := services.NewContainerBuilder()

	t.Run("should generate correct docker run command with all options", func(t *testing.T) {
		// Arrange
		project := mockProjectWithVolumesAndEnv(t)
		imageName, err := docVos.NewImageName("my-image", "latest")
		require.NoError(t, err)
		opts, err := builder.CreateOptions(project, "test sand", imageName)
		require.NoError(t, err)

		// Act
		command, err := builder.BuildCommand(opts)

		// Assert
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(command, "docker run"))
		assert.Contains(t, command, "--rm")
		assert.Contains(t, command, "--mount type=bind,source=/host/path1,target=/container/path1")
		assert.Contains(t, command, "--mount type=bind,source=/host/path2,target=/container/path2")
		assert.Contains(t, command, "-e VAR1=value1")
		assert.Contains(t, command, "-e VAR2=value2")
		assert.Contains(t, command, "my-image:latest")
		assert.Contains(t, command, "test sand")
	})

	t.Run("should generate correct docker run command without volumes and env vars", func(t *testing.T) {
		// Arrange
		project := mockProjectWithoutVolumesAndEnv(t)
		imageName, err := docVos.NewImageName("simple-image", "v1")
		require.NoError(t, err)
		opts, err := builder.CreateOptions(project, "test sand", imageName)
		require.NoError(t, err)

		// Act
		command, err := builder.BuildCommand(opts)

		// Assert
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(command, "docker run"))
		assert.Contains(t, command, "--rm")
		assert.NotContains(t, command, "--mount")
		assert.NotContains(t, command, "-e")
		assert.Contains(t, command, "simple-image:v1")
		assert.Contains(t, command, "test sand")
	})

	t.Run("should include image name passed as parameter", func(t *testing.T) {
		// Arrange: Creamos opciones con una imagen, pero pasamos otra diferente
		project := mockProjectWithoutVolumesAndEnv(t)
		imageName, err := docVos.NewImageName("my-image", "latest")
		require.NoError(t, err)
		opts, err := builder.CreateOptions(project, "test sand", imageName)
		require.NoError(t, err)

		// Act
		command, err := builder.BuildCommand(opts)

		// Assert
		require.NoError(t, err)
		// Debe usar la imagen pasada como parámetro, no la de opts
		assert.Contains(t, command, "my-image:latest")
		assert.NotContains(t, command, "other-image:latest")
	})
}
