<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo-light.png">
    <img alt="vex" src="assets/logo-dark.png" width="200">
  </picture>
  <p><strong>De código a producción en dos comandos.</strong></p>

  <p>
    <a href="https://github.com/jairoprogramador/vex/releases">
      <img src="https://img.shields.io/github/v/release/jairoprogramador/vex?style=for-the-badge" alt="Latest Release">
    </a>
    <a href="https://github.com/jairoprogramador/vex/blob/main/LICENSE">
      <img src="https://img.shields.io/github/license/jairoprogramador/vex?style=for-the-badge" alt="License">
    </a>
  </p>
</div>

---

**Vex** es un orquestador de despliegues de código abierto que lleva un servicio a producción mediante pipelines reutilizables y parametrizables, sin montar la infraestructura a mano. Está pensado para equipos de desarrollo que quieren desplegar en la nube con comandos simples.

```sh
vex init
vex deploy sand
```

## Descripción general

Vex ejecuta un **pipeline** (pasos como `test`, `supply`, `package` y `deploy`) en un **ambiente** (`sand`, `stag`, `prod`…). El pipeline vive en un repositorio git, de modo que se versiona, se comparte entre proyectos y se puede revisar.

Vex se compone de dos piezas:

| Pieza | Qué hace |
| :--- | :--- |
| **`vex`** (esta CLI) | Lee `vexconfig.yaml`, clona el proyecto y el pipeline, crea el contenedor y muestra el avance. |
| **`vexd`** ([vex-engine](https://github.com/jairoprogramador/vex-engine)) | Corre en un contenedor Docker, decide qué pasos ejecutar, los ejecuta y guarda el historial. |

Ambas se comunican por JSON-RPC 2.0 sobre la entrada y salida estándar del contenedor.

## Características

- Pipelines en repositorios git, reutilizables entre proyectos.
- Ejecución aislada en un contenedor Docker.
- Reutiliza los pasos que no cambiaron desde la última ejecución.
- Historial de intentos y despliegues, con diagnóstico de fallos (`vex why`, `vex log`).
- Rollback a un despliegue anterior con `vex rollback`.
- Ambientes protegidos, donde los lanzamientos se deciden de forma manual.
- Credenciales tomadas del entorno local; no se escriben en ningún archivo.

## ¿Por qué Vex?

Desplegar una aplicación en la nube exige conocer infraestructura, servicios cloud, redes, seguridad y CI/CD.

Vex reduce esa barrera sin ocultar la infraestructura. En lugar de ofrecer una plataforma cerrada, orquesta pipelines que se pueden inspeccionar, personalizar y ejecutar en la infraestructura del propio equipo.

## Instalación

Requisitos: [Git](https://git-scm.com/downloads) y [Docker](https://docs.docker.com/get-docker/).

La guía de instalación para macOS, Linux y Windows está en **https://vexja.com/install**.

Verifica la instalación con:

```sh
vex version
```

## Inicio rápido

Un pipeline típico tiene cuatro pasos (`test` → `supply` → `package` → `deploy`) y varios ambientes (`sand`, `stag`, `prod`). Los nombres los define el pipeline, no Vex.

**1. Inicializa el proyecto.** En el directorio del proyecto:

```sh
vex init
```

Vex pregunta nombre, equipo y organización, y genera `vexconfig.yaml`. Con `-y` usa los valores por defecto.

**2. Revisa qué define el pipeline.**

```sh
vex steps    # pasos, en orden
vex envs     # ambientes disponibles
```

**3. Despliega.**

```sh
vex deploy sand
```

Vex ejecuta todos los pasos hasta `deploy` en el ambiente `sand` y muestra el avance en vivo:

```text
• Preparando proyecto: https://github.com/mi-org/mi-servicio.git@main
• Preparando pipeline: https://github.com/mi-org/mi-pipeline.git@main
Intento 01a1120d-aaf5-70ce-a003-91f4b5553056
▶ test
  ✔ compilar
  ✔ pruebas
  ✔ test: ejecutado

✔ Intento 01a1120d-aaf5-70ce-a003-91f4b5553056: exitoso (3s)
```

**4. Si algo falla.**

```sh
vex why           # qué cambió desde la última vez que funcionó
vex log --failed  # salida exacta del comando que falló
```

> Vex despliega el **commit** de la rama configurada, no el directorio de trabajo. Haz commit y push antes de ejecutar.

## Uso

```sh
vex <paso> <ambiente>
```

El paso indica hasta dónde se ejecuta el pipeline; se hacen también los pasos anteriores que hagan falta. El ambiente es obligatorio.

| Comando | Descripción |
| :--- | :--- |
| `vex <step> <env>` | Ejecuta el pipeline hasta ese paso en ese ambiente. |
| `vex check <step> <env>` | Valida ambiente, paso y variables sin ejecutar nada. |
| `vex steps` · `vex envs` | Lista los pasos y los ambientes del pipeline. |
| `vex ls <env>` | Últimos intentos de un ambiente. |
| `vex why` · `vex log --failed` | Diagnostican el último fallo. |
| `vex deployments <env>` | Despliegues de un ambiente y cuál está lanzado. |
| `vex rollback <despliegue>` | Vuelve a desplegar los commits de un despliegue anterior. |
| `vex release <env> <despliegue>` | Hace visible un despliegue en un ambiente protegido. |

Consulta la [referencia completa de comandos](docs/comandos.md).

## Configuración

`vex init` genera `vexconfig.yaml`:

```yaml
project:
  name: mi-servicio
  team: mi-equipo
  organization: mi-org
  url: https://github.com/mi-org/mi-servicio.git   # repo del proyecto
  ref: main

pipeline:
  url: https://github.com/mi-org/mi-pipeline.git   # repo con el pipeline
  ref: main

runtime:
  image: jairoprogramador/vex-runtime-springboot-azure:latest
  run:
    envs:
      - name: "ARM_CLIENT_SECRET"
        value: "$ARM_CLIENT_SECRET"                # se lee de tu entorno
```

- `runtime.image` es una imagen del registro (`imagen:tag`) o la ruta de un Dockerfile que Vex construye. Debe incluir `vexd`.
- `"$NOMBRE"` toma el valor del entorno local y viaja al motor por la entrada estándar; no aparece en `docker inspect`.
- El motor corre en un contenedor Docker de la máquina local.

Consulta la [guía de configuración](docs/configuracion.md).

## Ejemplos

**Promover a producción** tras validar en `sand`:

```sh
vex deploy sand
vex deploy prod
```

**Volver a un despliegue anterior:**

```sh
vex deployments prod       # lista los despliegues y sus ids
vex rollback 91f4b55       # redespliega los commits de ese despliegue
```

## Contribuir

Las contribuciones son bienvenidas. Abre un issue para acordar el cambio, crea una rama desde `develop`, ejecuta `go test ./...` y abre un pull request. Los commits siguen la convención `feat:`, `fix:`, `docs:`… porque determinan la versión publicada.

Consulta [CONTRIBUTING.md](CONTRIBUTING.md).

## Más información

- Sitio web e instalación: https://vexja.com
- Motor de ejecución: [vex-engine](https://github.com/jairoprogramador/vex-engine)
- Plantillas: [vex-template-store](https://github.com/jairoprogramador/vex-template-store)
- [Comandos](docs/comandos.md) · [Configuración](docs/configuracion.md) · [Solución de problemas](docs/solucion-de-problemas.md)
- Errores y sugerencias: [Issues](https://github.com/jairoprogramador/vex/issues)

## Licencia

GNU Affero General Public License v3.0. Consulta el archivo [LICENSE](LICENSE).
