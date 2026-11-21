# Verificar la descarga

Cada release de `vex` publica, junto a los archivos:

- `vex_<versión>_checksums.txt`: el sha256 de cada archivo, paquete y SBOM del release.
- `vex_<versión>_checksums.txt.sigstore.json`: la firma de ese archivo (Sigstore, sin llaves), hecha por el workflow de release de este repositorio.
- `multiple.intoto.jsonl`: la procedencia SLSA (nivel 3) de los mismos archivos.
- Un SBOM (`*.sbom.json`, SPDX) por archivo comprimido.

Verificar son dos pasos: que el archivo de checksums lo firmó el workflow de release, y que tu descarga tiene el sha256 que ese archivo dice.

## Con cosign

Necesitas [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) 3 o posterior (lee el formato de firma que usa el release). Sustituye `X.Y.Z` por la versión.

```sh
V=X.Y.Z
BASE=https://github.com/jairoprogramador/vex/releases/download/v$V
curl -fsSLO "$BASE/vex_${V}_checksums.txt"
curl -fsSLO "$BASE/vex_${V}_checksums.txt.sigstore.json"
curl -fsSLO "$BASE/vex_linux_amd64.tar.gz"        # el archivo de tu sistema

cosign verify-blob \
  --bundle "vex_${V}_checksums.txt.sigstore.json" \
  --certificate-identity "https://github.com/jairoprogramador/vex/.github/workflows/release.yml@refs/tags/v$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "vex_${V}_checksums.txt"
# Verified OK

sha256sum --check --ignore-missing "vex_${V}_checksums.txt"    # macOS: shasum -a 256 --check --ignore-missing
# vex_linux_amd64.tar.gz: OK
```

La identidad es exacta: el workflow `release.yml` de `jairoprogramador/vex`, ejecutado desde el tag de esa versión. Una firma válida de cualquier otro repositorio, workflow o rama no pasa. Si alguno de los dos comandos falla, no instales ese archivo.

## Procedencia SLSA

Con [slsa-verifier](https://github.com/slsa-framework/slsa-verifier):

```sh
curl -fsSLO "$BASE/multiple.intoto.jsonl"
slsa-verifier verify-artifact vex_linux_amd64.tar.gz \
  --provenance-path multiple.intoto.jsonl \
  --source-uri github.com/jairoprogramador/vex \
  --source-tag "v$V"
# PASSED: SLSA verification passed
```

Dice que ese archivo se construyó en GitHub Actions a partir de ese tag de este repositorio.

## Homebrew y paquetes

`brew install --cask jairoprogramador/vex/vex` comprueba el sha256 que el release escribió en el cask. Los paquetes `.deb`, `.rpm` y `.apk` están en el archivo de checksums: se verifican igual que un `.tar.gz`.

## Qué cubre y qué no

- Cubre el binario: que es el que publicó el workflow de release, sin cambios. Los releases se aprueban a mano.
- El binario lleva dentro el emisor de tokens en el que confiará tu nube y la identidad que exige a las firmas del catálogo y de las imágenes: verificar el binario es verificar eso.
- No cubre las plantillas ni las imágenes de runtime: esas las verifica `vex` cada vez que las usa ([Seguridad](seguridad.md)).
- En macOS el binario no está notarizado por Apple: Gatekeeper puede avisar si lo descargas con el navegador. Homebrew y `curl` no ponen la marca de cuarentena.
