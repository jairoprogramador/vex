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

**`vex`** es una CLI open source para equipos que quieren desplegar en producción sin necesitar un equipo de DevOps ni arquitectos de nube. Sin Terraform. Sin Dockerfiles. Sin configuraciones interminables. Solo dos comandos:

```sh
vex init
vex deploy prod
```

Eso es todo. `vex` selecciona automáticamente una arquitectura cloud probada, empaqueta tu aplicación y la despliega. Lo que normalmente toma días de configuración manual, con `vex` toma 5 minutos.

`vex` puede ejecutar el despliegue de dos maneras:

- **Modo local** (default): construye una imagen Docker y corre el motor `vexd` en tu máquina.
- **Modo remoto** (`--remote` o `VEX_MODE=remote`): autentica contra el portal Vex (device flow), arma la solicitud y dispara una _Fly Machine_ efímera que ejecuta `vexd`. Los logs se transmiten en vivo por SSE.

## ¿Para quién es?

Para equipos pequeños de desarrollo que:

- Necesitan llevar un microservicio a producción **rápido**.
- No tienen un equipo de DevOps dedicado.
- No quieren perder días configurando infraestructura cloud.
- Prefieren enfocarse en escribir código.

## ¿Cómo funciona?

`vex` es la interfaz con la que interactúas. Por debajo, se apoya en [vex-core](https://github.com/jairoprogramador/vex) (el motor de ejecución) y [template store](https://github.com/jairoprogramador/vex-template-store) (plantillas de despliegue probadas para distintos escenarios). Tú solo necesitas conocer dos comandos.

### Flujo en 3 pasos

**1. Inicializa tu proyecto**

Navega al directorio de tu proyecto y ejecuta:

```sh
vex init
```

Responde unas preguntas breves (nombre, equipo, organización) y `vex` genera un archivo `vexconfig.yaml` con la configuración lista para desplegar. Se asigna automáticamente una arquitectura cloud básica.

**2. Ajusta la arquitectura (Opcional)**

Si necesitas una arquitectura más robusta, ejecuta:

```sh
vex arq
```

`vex` te hace 3 preguntas sobre escalabilidad, recuperación y presupuesto, y selecciona la arquitectura cloud que mejor se adapta a tu caso.

**3. Despliega**

```sh
vex deploy sand    # sandbox
vex deploy stag    # staging
vex deploy prod    # producción
```

Listo. Tu microservicio está en producción.

## Requisitos previos

- Instalar [Git](https://git-scm.com/downloads)
- Instalar [Docker](https://docs.docker.com/get-docker/)

## Instalación

### macOS (Homebrew)

```sh
brew install --cask jairoprogramador/vex/vex
```

> Si macOS indica que no puede verificar el desarrollador, permite la ejecución en **Ajustes del sistema > Privacidad y seguridad > Abrir de todos modos**, o ejecuta: `xattr -cr $(which vex)`.

Verifica la instalación abriendo una nueva terminal:

```sh
vex version
```

### Linux

Descarga el paquete desde [Releases](https://github.com/jairoprogramador/vex/releases):

```sh
# Debian / Ubuntu
sudo dpkg -i vex_*.deb

# Red Hat / Fedora
sudo rpm -i vex_*.rpm
```

O directamente el binario:

```sh
curl -sL https://github.com/jairoprogramador/vex/releases/latest/download/vex_linux_amd64.tar.gz | tar xz
sudo mv vex /usr/local/bin/
```

Verifica la instalación abriendo una nueva terminal:

```sh
vex version
```

### Windows

1. Descarga el archivo `.zip` correspondiente desde [Releases](https://github.com/jairoprogramador/vex/releases):
   - `vex_windows_amd64.zip` — Para PCs con procesador Intel o AMD de 64 bits.
   - `vex_windows_arm64.zip` — Para dispositivos con arquitectura ARM (ej: Surface Pro X, Copilot+ PCs).
2. Descomprime el archivo.
3. Copia `vex.exe` a una carpeta nueva llamada `vex` dentro de `Program Files` o `Program Files (x86)`:
   ```
   C:\Program Files\vex\vex.exe
   ```
4. Añade esa carpeta a tu variable de entorno `PATH`:
   - Abre **Configuración > Sistema > Acerca de > Configuración avanzada del sistema > Variables de entorno**.
   - En **Variables del sistema**, selecciona `Path` y haz clic en **Editar**.
   - Agrega `C:\Program Files\vex`.

Verifica la instalación abriendo una nueva ventana de PowerShell:

```sh
vex version
```

## Referencia de comandos

| Comando | Descripción |
| :--- | :--- |
| `vex init` | Inicializa el proyecto y genera `vexconfig.yaml`. |
| `vex arq` | Ajusta la arquitectura cloud según tus necesidades (opcional). |
| `vex <step> [env]` | Ejecuta el step indicado (`test`, `supply`, `package`, `deploy`) en el entorno (`sand`, `stag`, `prod`). |
| `vex auth login` | Autentica el CLI contra el portal mediante device flow y persiste el token. |
| `vex auth logout` | Borra el token guardado. |
| `vex auth whoami` | Muestra el usuario autenticado. |
| `vex execution cancel <id>` | Cancela una ejecución remota en curso. |
| `vex version` | Muestra la versión instalada. |

### Flags relevantes

| Flag | Descripción |
| :--- | :--- |
| `--remote` | Ejecuta el step en la infraestructura del portal (Fly Machines) en vez del Docker local. Equivalente a `VEX_MODE=remote`. |
| `--no-follow` | Solo válido con `--remote`. Sale apenas la ejecución queda encolada (no streamea logs). |

> `vex init` acepta el flag `--yes` / `-y` para usar valores por defecto sin preguntas interactivas.

## Modo local vs modo remoto

| | Modo local | Modo remoto |
| :--- | :--- | :--- |
| Disparador | default | `--remote` o `VEX_MODE=remote` |
| Auth | no requerida | `vex auth login` (device flow OAuth) |
| Ejecución | imagen Docker construida y corrida en tu host | Fly Machine efímera (`auto_destroy=true`) creada por el portal |
| Logs | stdout local | SSE en vivo desde el portal (salvo `--no-follow`) |
| Estado | en memoria del proceso | persistido en el portal — visible y auditado |
| Concurrencia | sin límite (limitado por tu máquina) | hasta 3 ejecuciones simultáneas por usuario |

Ante un 503 con `Retry-After`, `--remote --follow` reintenta una vez automáticamente. `--no-follow` falla directo para que decidas cuándo reintentar. Un 429 (límite de concurrencia del usuario) nunca se reintenta — espera a que termine alguna o cancela con `vex execution cancel <id>`.

## Variables de entorno

| Variable | Default | Descripción |
| :--- | :--- | :--- |
| `VEX_PORTAL_URL` | `https://vexportal.app` | URL base del portal Vex (útil para apuntar a staging o un fork). |
| `VEX_MODE` | _(unset)_ | `remote` activa el modo remoto sin pasar `--remote`. Cualquier otro valor se ignora. |
| `XDG_CONFIG_HOME` | _(unset)_ | Si se establece, `vex` guarda credenciales bajo `$XDG_CONFIG_HOME/.vex/credentials.json`. |

## Credenciales

El token del portal se persiste con permisos `0600` en, por orden de prioridad:

1. `$XDG_CONFIG_HOME/.vex/credentials.json` (si la variable está seteada).
2. `%APPDATA%\.vex\credentials.json` (Windows).
3. `$HOME/.vex/.config/credentials.json` (POSIX, fallback).

Borrar este archivo equivale a `vex auth logout`.

## Contribuciones

Las contribuciones son bienvenidas. Si tienes ideas, encuentras un error o quieres mejorar algo, abre un [issue](https://github.com/jairoprogramador/vex/issues) o enviá un [pull request](https://github.com/jairoprogramador/vex/pulls).

## Licencia

`vex` está distribuido bajo la [Business Source License 1.1](https://github.com/jairoprogramador/vex/blob/main/LICENSE).
