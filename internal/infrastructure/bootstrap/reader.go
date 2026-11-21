package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jairoprogramador/vex/internal/application"
)

const (
	rootDir          = "bootstrap"
	manifestName     = "bootstrap.yaml"
	maxManifestBytes = 16 << 10
	maxProviders     = 20
)

var (
	cloudPattern    = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)
	contractPattern = regexp.MustCompile(`^([a-z][a-z0-9]{1,15})/v[1-9][0-9]{0,2}$`)
	templatePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	providerPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.]{2,63}$`)
	stepPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
)

// Reader lee el bootstrap de un pipeline desde el disco.
type Reader struct{}

var _ application.BootstrapReader = Reader{}

// Read lee bootstrap/<provider>/ de pipelineDir: valida el manifiesto y calcula la huella de la carpeta. Con
// provider "" el pipeline debe traer exactamente una nube. Los enlaces simbólicos y los archivos especiales se
// rechazan: quien acepta un bootstrap debe poder ver todo lo que entra en la huella.
func (Reader) Read(pipelineDir, provider string) (application.PipelineBootstrap, error) {
	cloud, err := chooseCloud(filepath.Join(pipelineDir, rootDir), provider)
	if err != nil {
		return application.PipelineBootstrap{}, err
	}
	label := path.Join(rootDir, cloud)
	dir := filepath.Join(pipelineDir, rootDir, cloud)

	files, err := readFiles(dir)
	if err != nil {
		return application.PipelineBootstrap{}, fmt.Errorf("%s: %w", label, err)
	}
	manifest, err := readManifest(files, cloud)
	if err != nil {
		return application.PipelineBootstrap{}, fmt.Errorf("%s/%s: %w", label, manifestName, err)
	}
	fingerprint, err := Hash(files)
	if err != nil {
		return application.PipelineBootstrap{}, fmt.Errorf("%s: %w", label, err)
	}
	// validateManifest ya comprobó que la plantilla es uno de los archivos leídos.
	template := files[slices.IndexFunc(files, func(f File) bool { return f.Path == manifest.Template })].Content
	return application.PipelineBootstrap{
		Cloud: cloud, Dir: dir, Fingerprint: fingerprint, Manifest: manifest, Template: template,
	}, nil
}

// chooseCloud decide qué carpeta bootstrap/<nube>/ se lee.
func chooseCloud(root, provider string) (string, error) {
	clouds, err := listClouds(root)
	if err != nil {
		return "", err
	}
	switch {
	case provider != "":
		if !slices.Contains(clouds, provider) {
			return "", fmt.Errorf("cloud.provider es %q pero el pipeline %s: corrige cloud.provider en vexconfig.yaml o quítalo",
				provider, describeClouds(clouds))
		}
		return provider, nil
	case len(clouds) == 0:
		return "", fmt.Errorf("el pipeline no declara %s/<nube>/%s: no se puede usar con \"credentials: portal\"",
			rootDir, manifestName)
	case len(clouds) > 1:
		return "", fmt.Errorf("el pipeline soporta varias nubes (%s): declara cloud.provider en vexconfig.yaml",
			strings.Join(clouds, ", "))
	}
	return clouds[0], nil
}

func describeClouds(clouds []string) string {
	if len(clouds) == 0 {
		return "no trae ninguna carpeta " + rootDir + "/<nube>/"
	}
	folders := make([]string, len(clouds))
	for i, cloud := range clouds {
		folders[i] = path.Join(rootDir, cloud) + "/"
	}
	return "solo trae " + strings.Join(folders, ", ")
}

// listClouds devuelve, en orden, las carpetas de root con nombre de nube. Un enlace simbólico no cuenta.
func listClouds(root string) ([]string, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leer %s/: %w", rootDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s/ debe ser una carpeta del pipeline, no un enlace ni un archivo", rootDir)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("leer %s/: %w", rootDir, err)
	}
	var clouds []string
	for _, entry := range entries {
		if entry.IsDir() && cloudPattern.MatchString(entry.Name()) {
			clouds = append(clouds, entry.Name())
		}
	}
	return clouds, nil
}

// readFiles lee todos los archivos regulares de dir. Corta en cuanto la carpeta supera los límites de la
// huella, para no cargar en memoria una carpeta enorme.
func readFiles(dir string) ([]File, error) {
	var files []File
	total := int64(0)
	err := filepath.WalkDir(dir, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		switch {
		case entry.IsDir():
			return nil
		case !entry.Type().IsRegular():
			return fmt.Errorf("%s: no se permiten enlaces ni archivos especiales", relative)
		case len(files) == MaxFiles:
			return &HashError{Code: CodeTooManyFiles}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if total += info.Size(); total > MaxTotalBytes {
			return &HashError{Code: CodeTooLarge}
		}
		content, err := readRegular(current, info.Size())
		if err != nil {
			return fmt.Errorf("%s: %w", relative, err)
		}
		files = append(files, File{Path: relative, Content: content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// readRegular lee un archivo que hace un instante medía size bytes; si creció entretanto, no lee de más.
func readRegular(name string, size int64) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > size {
		return nil, errors.New("cambió mientras se leía")
	}
	return content, nil
}

// manifestFile es bootstrap.yaml tal como se escribe.
type manifestFile struct {
	Contract   string   `yaml:"contract"`
	Template   string   `yaml:"template"`
	Providers  []string `yaml:"providers"`
	CloudSteps []string `yaml:"cloud_steps"`
}

// readManifest valida el manifiesto de la carpeta de cloud. La lectura es estricta: una clave que esta versión
// no conoce se rechaza, porque todo lo que el manifiesto diga lo acepta quien ejecuta `vex bootstrap`.
func readManifest(files []File, cloud string) (application.BootstrapManifest, error) {
	index := slices.IndexFunc(files, func(f File) bool { return f.Path == manifestName })
	if index < 0 {
		return application.BootstrapManifest{}, errors.New("falta el manifiesto")
	}
	content := files[index].Content
	if len(content) > maxManifestBytes {
		return application.BootstrapManifest{}, fmt.Errorf("supera el límite de %d KiB", maxManifestBytes>>10)
	}

	var raw manifestFile
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return application.BootstrapManifest{}, errors.New("está vacío")
		}
		return application.BootstrapManifest{}, fmt.Errorf("no es un manifiesto válido: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return application.BootstrapManifest{}, errors.New("debe contener un solo documento YAML")
	}

	if err := validateManifest(raw, cloud, files); err != nil {
		return application.BootstrapManifest{}, err
	}
	return application.BootstrapManifest{
		Contract:   application.ContractID(raw.Contract),
		Template:   raw.Template,
		Providers:  raw.Providers,
		CloudSteps: raw.CloudSteps,
	}, nil
}

func validateManifest(raw manifestFile, cloud string, files []File) error {
	match := contractPattern.FindStringSubmatch(raw.Contract)
	switch {
	case raw.Contract == "":
		return errors.New("falta \"contract\"")
	case match == nil:
		return fmt.Errorf("contract %q no tiene la forma <nube>/v<n>", raw.Contract)
	case match[1] != cloud:
		return fmt.Errorf("contract %q no corresponde a la carpeta %s/%s/", raw.Contract, rootDir, cloud)
	}

	switch {
	case raw.Template == "":
		return errors.New("falta \"template\"")
	case !templatePattern.MatchString(raw.Template) || raw.Template == manifestName:
		return fmt.Errorf("template %q debe ser el nombre de un archivo de esta carpeta, sin separadores de ruta", raw.Template)
	case !slices.ContainsFunc(files, func(f File) bool { return f.Path == raw.Template }):
		return fmt.Errorf("template %q no existe en esta carpeta", raw.Template)
	}

	if len(raw.Providers) > maxProviders {
		return fmt.Errorf("providers admite como máximo %d entradas", maxProviders)
	}
	if err := validateNames("providers", raw.Providers, providerPattern); err != nil {
		return err
	}
	if len(raw.CloudSteps) == 0 {
		return errors.New("\"cloud_steps\" es obligatorio y no puede estar vacío: son los pasos que reciben el token")
	}
	return validateNames("cloud_steps", raw.CloudSteps, stepPattern)
}

// validateNames exige que cada entrada cumpla pattern y que ninguna se repita.
func validateNames(field string, names []string, pattern *regexp.Regexp) error {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		switch {
		case !pattern.MatchString(name):
			return fmt.Errorf("%s: %q no es un nombre válido", field, name)
		case seen[name]:
			return fmt.Errorf("%s: %q está repetido", field, name)
		}
		seen[name] = true
	}
	return nil
}
