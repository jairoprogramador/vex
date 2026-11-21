package vos

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	DefaultContainerImage = "Dockerfile"
	DefaultContainerTag   = "latest"
)

// imageTagRegex detecta el formato "<imagen>:<tag>" donde cada parte tiene al
// menos un carácter. El tag es la parte después del último ":".
var imageTagRegex = regexp.MustCompile(`^(.+):([^:]+)$`)

// digestRegex detecta "<repo>[:<tag>]@sha256:<64 hex>": una imagen fijada a su contenido.
var digestRegex = regexp.MustCompile(`^([^@]+?)(?::([^:@/]+))?@(sha256:[0-9a-f]{64})$`)

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
	digest      string
	tagExplicit bool
}

// NewImage construye un Image a partir de un único spec:
//   - ""                → Dockerfile por defecto ("Dockerfile:latest"), tagExplicit=false
//   - "ubuntu:22.04"    → imagen de registry, tagExplicit=true
//   - "a/b:1.2@sha256:…" → imagen de registry fijada a un digest (el tag es opcional)
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

	if m := digestRegex.FindStringSubmatch(imageSpec); m != nil {
		return Image{image: m[1], tag: m[2], digest: m[3], tagExplicit: true}, nil
	}
	// Una "@" que no introduce un digest completo es un digest mal escrito: aceptarlo como tag lo ocultaría.
	if strings.Contains(imageSpec, "@") {
		return Image{}, fmt.Errorf("imagen %q: después de @ va un digest sha256 de 64 caracteres hexadecimales", imageSpec)
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

// Tag devuelve el tag de la imagen. Para rutas de Dockerfile siempre es "latest"; una imagen fijada solo por
// digest no tiene tag.
func (c Image) Tag() string { return c.tag }

// Digest devuelve el digest fijado ("sha256:…"), o "" si la imagen solo dice un tag.
func (c Image) Digest() string { return c.digest }

// Pinned dice si la imagen está fijada a su contenido.
func (c Image) Pinned() bool { return c.digest != "" }

// TagExplicit indica si el usuario especificó explícitamente un "imagen:tag".
// false significa que el spec es una ruta de Dockerfile.
func (c Image) TagExplicit() bool { return c.tagExplicit }

// Spec devuelve la representación canónica para serializar a YAML:
//   - "imagen:tag", "imagen:tag@digest" o "imagen@digest" si es una imagen de registry
//   - ruta del Dockerfile si !tagExplicit
func (c Image) Spec() string {
	if c.tagExplicit {
		return c.String()
	}
	return c.image
}

// String devuelve la imagen con todo lo que se sabe de ella ("imagen:tag", con "@digest" si está fijada): sirve
// para logs y para pedírsela a Docker, que con digest ignora el tag.
func (c Image) String() string {
	name := c.image
	if c.tag != "" {
		name += ":" + c.tag
	}
	if c.digest != "" {
		name += "@" + c.digest
	}
	return name
}

// Reference es la forma más estricta de nombrar la imagen: por digest si está fijada, por tag si no.
func (c Image) Reference() string {
	if c.digest != "" {
		return c.image + "@" + c.digest
	}
	return c.image + ":" + c.tag
}
