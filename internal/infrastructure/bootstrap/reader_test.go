package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

const starterManifest = `contract: azure/v1
template: main.json
providers: [Microsoft.App]
cloud_steps: [supply, package, deploy]
`

// writePipeline crea un pipeline con los archivos dados (ruta relativa → contenido).
func writePipeline(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return dir
}

func starterPipeline(t *testing.T) string {
	t.Helper()
	return writePipeline(t, map[string]string{
		"bootstrap/azure/bootstrap.yaml": starterManifest,
		"bootstrap/azure/main.json":      "{}\n",
		"bootstrap/azure/main.bicep":     "targetScope = 'subscription'\n",
	})
}

func TestRead_LeeElManifiestoYLaHuella(t *testing.T) {
	dir := starterPipeline(t)

	for _, provider := range []string{"", "azure"} {
		got, err := Reader{}.Read(dir, provider)

		require.NoError(t, err)
		assert.Equal(t, "azure", got.Cloud)
		assert.Equal(t, filepath.Join(dir, "bootstrap", "azure"), got.Dir)
		assert.Equal(t, application.BootstrapManifest{
			Contract: "azure/v1", Template: "main.json",
			Providers: []string{"Microsoft.App"}, CloudSteps: []string{"supply", "package", "deploy"},
		}, got.Manifest)

		want, err := Hash([]File{
			{Path: "bootstrap.yaml", Content: []byte(starterManifest)},
			{Path: "main.json", Content: []byte("{}\n")},
			{Path: "main.bicep", Content: []byte("targetScope = 'subscription'\n")},
		})
		require.NoError(t, err)
		assert.Equal(t, want, got.Fingerprint)
	}
}

func TestRead_LaHuellaCubreTodaLaCarpeta(t *testing.T) {
	base, err := Reader{}.Read(starterPipeline(t), "")
	require.NoError(t, err)

	changes := map[string]map[string]string{
		"cambia la plantilla": {
			"bootstrap/azure/bootstrap.yaml": starterManifest,
			"bootstrap/azure/main.json":      `{"resources":[]}` + "\n",
			"bootstrap/azure/main.bicep":     "targetScope = 'subscription'\n",
		},
		"cambian los pasos de nube": {
			"bootstrap/azure/bootstrap.yaml": strings.Replace(starterManifest, "[supply, package, deploy]", "[test, deploy]", 1),
			"bootstrap/azure/main.json":      "{}\n",
			"bootstrap/azure/main.bicep":     "targetScope = 'subscription'\n",
		},
		"aparece un módulo": {
			"bootstrap/azure/bootstrap.yaml":    starterManifest,
			"bootstrap/azure/main.json":         "{}\n",
			"bootstrap/azure/main.bicep":        "targetScope = 'subscription'\n",
			"bootstrap/azure/modules/red.bicep": "// red\n",
		},
	}
	for name, files := range changes {
		t.Run(name, func(t *testing.T) {
			got, err := Reader{}.Read(writePipeline(t, files), "")

			require.NoError(t, err)
			assert.NotEqual(t, base.Fingerprint, got.Fingerprint)
		})
	}

	t.Run("los finales de línea de Windows no la cambian", func(t *testing.T) {
		got, err := Reader{}.Read(writePipeline(t, map[string]string{
			"bootstrap/azure/bootstrap.yaml": strings.ReplaceAll(starterManifest, "\n", "\r\n"),
			"bootstrap/azure/main.json":      "{}\r\n",
			"bootstrap/azure/main.bicep":     "targetScope = 'subscription'\r\n",
		}), "")

		require.NoError(t, err)
		assert.Equal(t, base.Fingerprint, got.Fingerprint)
	})
}

func TestRead_EligeLaCarpetaDeLaNube(t *testing.T) {
	twoClouds := map[string]string{
		"bootstrap/azure/bootstrap.yaml": starterManifest,
		"bootstrap/azure/main.json":      "{}",
		"bootstrap/aws/bootstrap.yaml":   "contract: aws/v1\ntemplate: main.json\ncloud_steps: [deploy]\n",
		"bootstrap/aws/main.json":        "{}",
	}

	t.Run("con provider lee esa", func(t *testing.T) {
		got, err := Reader{}.Read(writePipeline(t, twoClouds), "aws")

		require.NoError(t, err)
		assert.Equal(t, application.ContractID("aws/v1"), got.Manifest.Contract)
	})
	t.Run("provider que el pipeline no trae", func(t *testing.T) {
		_, err := Reader{}.Read(starterPipeline(t), "aws")

		assert.ErrorContains(t, err, `cloud.provider es "aws" pero el pipeline solo trae bootstrap/azure/`)
	})
	t.Run("varias nubes sin provider", func(t *testing.T) {
		_, err := Reader{}.Read(writePipeline(t, twoClouds), "")

		assert.ErrorContains(t, err, "varias nubes (aws, azure): declara cloud.provider")
	})
	t.Run("sin carpeta de bootstrap", func(t *testing.T) {
		_, err := Reader{}.Read(writePipeline(t, map[string]string{"pipeline.yaml": "x"}), "")

		assert.ErrorContains(t, err, "el pipeline no declara bootstrap/<nube>/bootstrap.yaml")
	})
	t.Run("provider sin ninguna carpeta", func(t *testing.T) {
		_, err := Reader{}.Read(writePipeline(t, map[string]string{"pipeline.yaml": "x"}), "azure")

		assert.ErrorContains(t, err, "no trae ninguna carpeta bootstrap/<nube>/")
	})
}

func TestRead_ManifiestosInvalidos(t *testing.T) {
	tests := map[string]struct {
		manifest string
		want     string
	}{
		"sin contract":                  {"template: main.json\ncloud_steps: [deploy]\n", `falta "contract"`},
		"contract mal formado":          {"contract: azure\ntemplate: main.json\ncloud_steps: [deploy]\n", "<nube>/v<n>"},
		"contract de otra nube":         {"contract: aws/v1\ntemplate: main.json\ncloud_steps: [deploy]\n", "no corresponde a la carpeta bootstrap/azure/"},
		"sin template":                  {"contract: azure/v1\ncloud_steps: [deploy]\n", `falta "template"`},
		"template con ruta":             {"contract: azure/v1\ntemplate: modules/main.json\ncloud_steps: [deploy]\n", "sin separadores de ruta"},
		"template que sube":             {"contract: azure/v1\ntemplate: ../main.json\ncloud_steps: [deploy]\n", "sin separadores de ruta"},
		"template que es el manifiesto": {"contract: azure/v1\ntemplate: bootstrap.yaml\ncloud_steps: [deploy]\n", "sin separadores de ruta"},
		"template inexistente":          {"contract: azure/v1\ntemplate: otro.json\ncloud_steps: [deploy]\n", `template "otro.json" no existe`},
		"sin cloud_steps":               {"contract: azure/v1\ntemplate: main.json\n", `"cloud_steps" es obligatorio`},
		"cloud_steps vacío":             {"contract: azure/v1\ntemplate: main.json\ncloud_steps: []\n", `"cloud_steps" es obligatorio`},
		"cloud_steps repetido":          {"contract: azure/v1\ntemplate: main.json\ncloud_steps: [deploy, deploy]\n", `"deploy" está repetido`},
		"cloud_steps con mayúsculas":    {"contract: azure/v1\ntemplate: main.json\ncloud_steps: [Deploy]\n", `"Deploy" no es un nombre válido`},
		"provider inválido":             {"contract: azure/v1\ntemplate: main.json\nproviders: ['a b']\ncloud_steps: [deploy]\n", `providers: "a b"`},
		"clave desconocida":             {starterManifest + "identities: {pull: {}}\n", "identities"},
		"clave repetida":                {starterManifest + "contract: azure/v2\n", "no es un manifiesto válido"},
		"tipo equivocado":               {"contract: azure/v1\ntemplate: main.json\ncloud_steps: deploy\n", "no es un manifiesto válido"},
		"vacío":                         {"", "está vacío"},
		"dos documentos":                {starterManifest + "---\ncontract: azure/v1\n", "un solo documento"},
		"más de 16 KiB":                 {starterManifest + "# " + strings.Repeat("x", 16<<10) + "\n", "16 KiB"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := writePipeline(t, map[string]string{
				"bootstrap/azure/bootstrap.yaml": tt.manifest,
				"bootstrap/azure/main.json":      "{}",
			})

			_, err := Reader{}.Read(dir, "")

			require.Error(t, err)
			assert.ErrorContains(t, err, "bootstrap/azure/bootstrap.yaml")
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRead_DemasiadosProviders(t *testing.T) {
	providers := make([]string, 21)
	for i := range providers {
		providers[i] = "Microsoft.App" + strings.Repeat("x", i)
	}
	dir := writePipeline(t, map[string]string{
		"bootstrap/azure/bootstrap.yaml": "contract: azure/v1\ntemplate: main.json\nproviders: [" +
			strings.Join(providers, ", ") + "]\ncloud_steps: [deploy]\n",
		"bootstrap/azure/main.json": "{}",
	})

	_, err := Reader{}.Read(dir, "")

	assert.ErrorContains(t, err, "como máximo 20")
}

func TestRead_SinManifiesto(t *testing.T) {
	dir := writePipeline(t, map[string]string{"bootstrap/azure/main.json": "{}"})

	_, err := Reader{}.Read(dir, "")

	assert.ErrorContains(t, err, "bootstrap/azure/bootstrap.yaml: falta el manifiesto")
}

func TestRead_RechazaEnlacesSimbolicos(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secreto.json")
	require.NoError(t, os.WriteFile(outside, []byte("{}"), 0o600))

	t.Run("la plantilla es un enlace", func(t *testing.T) {
		dir := writePipeline(t, map[string]string{"bootstrap/azure/bootstrap.yaml": starterManifest})
		require.NoError(t, os.Symlink(outside, filepath.Join(dir, "bootstrap", "azure", "main.json")))

		_, err := Reader{}.Read(dir, "")

		assert.ErrorContains(t, err, "main.json: no se permiten enlaces ni archivos especiales")
	})
	t.Run("el manifiesto es un enlace", func(t *testing.T) {
		dir := writePipeline(t, map[string]string{"bootstrap/azure/main.json": "{}"})
		target := filepath.Join(t.TempDir(), "bootstrap.yaml")
		require.NoError(t, os.WriteFile(target, []byte(starterManifest), 0o600))
		require.NoError(t, os.Symlink(target, filepath.Join(dir, "bootstrap", "azure", "bootstrap.yaml")))

		_, err := Reader{}.Read(dir, "")

		assert.ErrorContains(t, err, "bootstrap.yaml: no se permiten enlaces ni archivos especiales")
	})
	t.Run("la carpeta de la nube es un enlace", func(t *testing.T) {
		real := starterPipeline(t)
		dir := writePipeline(t, map[string]string{"pipeline.yaml": "x"})
		require.NoError(t, os.Mkdir(filepath.Join(dir, "bootstrap"), 0o755))
		require.NoError(t, os.Symlink(filepath.Join(real, "bootstrap", "azure"), filepath.Join(dir, "bootstrap", "azure")))

		_, err := Reader{}.Read(dir, "azure")

		assert.ErrorContains(t, err, "no trae ninguna carpeta", "un enlace no cuenta como carpeta de nube")
	})
	t.Run("bootstrap es un enlace", func(t *testing.T) {
		real := starterPipeline(t)
		dir := writePipeline(t, map[string]string{"pipeline.yaml": "x"})
		require.NoError(t, os.Symlink(filepath.Join(real, "bootstrap"), filepath.Join(dir, "bootstrap")))

		_, err := Reader{}.Read(dir, "")

		assert.ErrorContains(t, err, "bootstrap/ debe ser una carpeta del pipeline")
	})
}

func TestRead_LimitesDeLaCarpeta(t *testing.T) {
	t.Run("más de 1 MiB", func(t *testing.T) {
		dir := writePipeline(t, map[string]string{
			"bootstrap/azure/bootstrap.yaml": starterManifest,
			"bootstrap/azure/main.json":      strings.Repeat("a", MaxTotalBytes),
		})

		_, err := Reader{}.Read(dir, "")

		var hashErr *HashError
		require.ErrorAs(t, err, &hashErr)
		assert.Equal(t, CodeTooLarge, hashErr.Code)
		assert.ErrorContains(t, err, "bootstrap/azure")
	})
	t.Run("más de 64 archivos", func(t *testing.T) {
		files := map[string]string{"bootstrap/azure/bootstrap.yaml": starterManifest, "bootstrap/azure/main.json": "{}"}
		for i := range 63 {
			files[filepath.Join("bootstrap/azure/modules", strings.Repeat("m", i+1)+".json")] = "{}"
		}

		_, err := Reader{}.Read(writePipeline(t, files), "")

		var hashErr *HashError
		require.ErrorAs(t, err, &hashErr)
		assert.Equal(t, CodeTooManyFiles, hashErr.Code)
	})
	t.Run("un archivo que no es texto", func(t *testing.T) {
		dir := writePipeline(t, map[string]string{
			"bootstrap/azure/bootstrap.yaml": starterManifest,
			"bootstrap/azure/main.json":      "{}",
			"bootstrap/azure/logo.png":       "\xff\xfe\xfa",
		})

		_, err := Reader{}.Read(dir, "")

		var hashErr *HashError
		require.ErrorAs(t, err, &hashErr)
		assert.Equal(t, CodeNotUTF8, hashErr.Code)
	})
}

// TestHashDePlantilla imprime la huella de una carpeta bootstrap/<nube>/ real, para compararla a mano con la que
// guarda el backend:
//
//	VEX_HASH_DIR=<pipeline> go test ./internal/infrastructure/bootstrap -run TestHashDePlantilla -v
func TestHashDePlantilla(t *testing.T) {
	dir := os.Getenv("VEX_HASH_DIR")
	if dir == "" {
		t.Skip("VEX_HASH_DIR no definido")
	}
	got, err := Reader{}.Read(dir, os.Getenv("VEX_HASH_CLOUD"))
	require.NoError(t, err)
	t.Logf("bootstrap/%s (%s): %s", got.Cloud, got.Manifest.Contract, got.Fingerprint)
}
