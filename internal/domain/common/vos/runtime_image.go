package vos

import (
	"fmt"
	"regexp"
)

const (
	DefaultContainerImage = "Dockerfile"
	DefaultContainerTag   = "latest"
)

// imageTagRegex detecta el formato "<imagen>:<tag>" donde cada parte tiene al
// menos un carácter. El tag es la parte después del último ":".
var imageTagRegex = regexp.MustCompile(`^(.+):([^:]+)$`)

// Image representa la fuente de la imagen runtime del proyecto.
// Puede ser una referencia a un registry ("ubuntu:22.04") o la ruta a un
// Dockerfile ("Dockerfile", "docker/MyDockerfile").
//
// La distinción se hace a través de tagExplicit:
//   - true  → el usuario escribió "imagen:tag", es una imagen de registry.
//   - false → no hay ":" en el spec, se trata como ruta de Dockerfile.
type Image struct {
	image       string
	tag         string
	tagExplicit bool
}

// NewImage construye un Image a partir de un único spec:
//   - ""                → Dockerfile por defecto ("Dockerfile:latest"), tagExplicit=false
//   - "ubuntu:22.04"    → imagen de registry, tagExplicit=true
//   - "Dockerfile"      → ruta de Dockerfile, tag="latest", tagExplicit=false
//   - "docker/MyFile"   → ruta de Dockerfile, tag="latest", tagExplicit=false
func NewImage(imageSpec string) (Image, error) {
	if imageSpec == "" {
		return Image{
			image:       DefaultContainerImage,
			tag:         DefaultContainerTag,
			tagExplicit: false,
		}, nil
	}

	if m := imageTagRegex.FindStringSubmatch(imageSpec); m != nil {
		return Image{
			image:       m[1],
			tag:         m[2],
			tagExplicit: true,
		}, nil
	}

	// Sin ":" → ruta de Dockerfile.
	return Image{
		image:       imageSpec,
		tag:         DefaultContainerTag,
		tagExplicit: false,
	}, nil
}

// Image devuelve el nombre de la imagen o la ruta del Dockerfile.
func (c Image) Image() string { return c.image }

// Tag devuelve el tag de la imagen. Para rutas de Dockerfile siempre es "latest".
func (c Image) Tag() string { return c.tag }

// TagExplicit indica si el usuario especificó explícitamente un "imagen:tag".
// false significa que el spec es una ruta de Dockerfile.
func (c Image) TagExplicit() bool { return c.tagExplicit }

// Spec devuelve la representación canónica para serializar a YAML:
//   - "imagen:tag"  si tagExplicit (imagen de registry)
//   - ruta del Dockerfile si !tagExplicit
func (c Image) Spec() string {
	if c.tagExplicit {
		return fmt.Sprintf("%s:%s", c.image, c.tag)
	}
	return c.image
}

// String devuelve siempre el formato "imagen:tag", útil para logs y display.
func (c Image) String() string { return fmt.Sprintf("%s:%s", c.image, c.tag) }
