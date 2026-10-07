# Contribuir a Vex

Las contribuciones son bienvenidas: errores, mejoras, documentación y código.

## Flujo

1. Abre un issue si el cambio es grande, para acordar el enfoque antes de escribir código.
2. Haz un fork y crea una rama a partir de `develop`.
3. Haz el cambio con pruebas.
4. Abre un pull request contra `develop`.

## Requisitos de desarrollo

- Go 1.25 o superior.
- Git y Docker (para las pruebas de integración).

```sh
go build ./cmd/vex
go test ./...
```

Dos grupos de pruebas necesitan recursos externos y se omiten si no los hay:

```sh
# Contrato con el motor real: compara los tipos del CLI con lo que emite un vexd.
VEXD_BIN=/ruta/a/vexd go test ./internal/infrastructure/engine/...

# Integración con Docker real (imagen con vexd y un pipeline de ejemplo).
VEX_IT_IMAGE=<imagen> VEX_IT_PIPELINE=<ruta> go test ./internal/infrastructure/engine/
```

> Los tests de `internal/infrastructure/portalauth` y `portalclient` fallan de antes y están fuera del alcance del MVP.

`vex` no importa código de `vex-engine`: los tipos del protocolo están copiados en `internal/infrastructure/engine/protocol` y el test de contrato avisa si el motor cambia algo.

## Commits

Se usan [commits convencionales](https://www.conventionalcommits.org/es/), porque determinan la versión que se publica al integrar en `main`:

| Prefijo | Efecto en la versión |
| :--- | :--- |
| `feat:` | sube minor |
| `fix:`, `chore:`, `docs:`, etc. | sube patch |
| `BREAKING CHANGE` | sube major |

## Licencia

Al contribuir aceptas que tu aporte se publique bajo la licencia [AGPL v3](LICENSE) del proyecto.
