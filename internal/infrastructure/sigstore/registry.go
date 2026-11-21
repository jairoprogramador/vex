package sigstore

import (
	"context"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	// maxBundles acota cuántas firmas se leen de una imagen: cualquiera puede asociarle artefactos en el registro.
	maxBundles = 16
	// maxBundleBytes acota cada firma; un bundle real pesa unos pocos KiB.
	maxBundleBytes = 1 << 20
)

// RemoteRegistry consulta el registro de imágenes con las credenciales de Docker de quien usa la CLI (o sin
// ellas, si no tiene): solo lee manifiestos y firmas, nunca capas de la imagen.
type RemoteRegistry struct{}

var _ Registry = RemoteRegistry{}

func (RemoteRegistry) options(ctx context.Context) []remote.Option {
	return []remote.Option{remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain)}
}

func (r RemoteRegistry) ResolveDigest(ctx context.Context, repository, tag string) (string, error) {
	ref, err := name.NewTag(repository+":"+tag, name.StrictValidation)
	if err != nil {
		return "", fmt.Errorf("la imagen no nombra un registro, un repositorio y un tag: %w", err)
	}
	descriptor, err := remote.Head(ref, r.options(ctx)...)
	if err != nil {
		return "", err
	}
	return descriptor.Digest.String(), nil
}

func (r RemoteRegistry) Bundles(ctx context.Context, repository, digest string) ([][]byte, error) {
	subject, err := name.NewDigest(repository+"@"+digest, name.StrictValidation)
	if err != nil {
		return nil, fmt.Errorf("la imagen no nombra un registro, un repositorio y un digest: %w", err)
	}
	index, err := remote.Referrers(subject, append(r.options(ctx), remote.WithFilter("artifactType", BundleArtifactType))...)
	if err != nil {
		return nil, err
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return nil, err
	}

	var bundles [][]byte
	for _, referrer := range manifest.Manifests {
		// El registro puede ignorar el filtro: se comprueba aquí.
		if referrer.ArtifactType != BundleArtifactType {
			continue
		}
		if len(bundles) == maxBundles {
			break
		}
		bundle, err := r.bundle(ctx, subject.Context().Digest(referrer.Digest.String()))
		if err != nil {
			// Una firma ilegible no impide probar las demás.
			continue
		}
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

// bundle lee la única capa del artefacto de firma, que es el bundle. go-containerregistry comprueba que lo
// leído tiene el digest que el manifiesto declara.
func (r RemoteRegistry) bundle(ctx context.Context, ref name.Digest) ([]byte, error) {
	artifact, err := remote.Image(ref, r.options(ctx)...)
	if err != nil {
		return nil, err
	}
	layers, err := artifact.Layers()
	if err != nil {
		return nil, err
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("el artefacto de firma tiene %d capas y se esperaba una", len(layers))
	}
	size, err := layers[0].Size()
	if err != nil {
		return nil, err
	}
	if size > maxBundleBytes {
		return nil, fmt.Errorf("el bundle de firma pesa %d bytes: demasiado", size)
	}
	content, err := layers[0].Compressed()
	if err != nil {
		return nil, err
	}
	defer content.Close()
	return io.ReadAll(io.LimitReader(content, maxBundleBytes))
}
