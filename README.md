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

**`vex`** es una CLI para equipos que quieren llevar un servicio a producción sin montar a mano toda la infraestructura. Lees un archivo, `vexconfig.yaml`, y `vex` se ocupa del resto: prepara el código y el pipeline de despliegue, ejecuta el motor [`vexd`](https://github.com/jairoprogramador/vex-engine) en un contenedor y te muestra el avance paso a paso.

```sh
vex init
vex deploy prod
```

## Cómo funciona

`vex` es la interfaz; el trabajo lo hace **vex-engine** (`vexd`), un motor que corre dentro de un contenedor y ejecuta un **pipeline** (pasos como `test`, `supply`, `package`, `deploy`) en un **ambiente** (`sand`, `stag`, `prod`…). Las responsabilidades están repartidas así:

| Quién | Qué hace |
| :--- | :--- |
| **`vex`** (esta CLI) | Lee `vexconfig.yaml`, clona el proyecto y el pipeline, crea el contenedor, le pide al motor la ejecución y presenta lo que el motor va contando. |
| **`vexd`** (el motor) | Decide qué pasos hay que ejecutar, los ejecuta, guarda el historial y responde con el avance y el resultado. |

La comunicación entre ambos es **JSON-RPC 2.0** por la entrada y salida estándar del contenedor: `vex` envía una petición y lee, línea a línea, las notificaciones de progreso y la respuesta final.

## Requisitos previos

- [Git](https://git-scm.com/downloads)
- [Docker](https://docs.docker.com/get-docker/)

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

### Windows

1. Descarga el archivo `.zip` correspondiente desde [Releases](https://github.com/jairoprogramador/vex/releases):
   - `vex_windows_amd64.zip` — Para PCs con procesador Intel o AMD de 64 bits.
   - `vex_windows_arm64.zip` — Para dispositivos con arquitectura ARM (ej: Surface Pro X, Copilot+ PCs).
2. Descomprime el archivo.
3. Copia `vex.exe` a una carpeta nueva llamada `vex` dentro de `Program Files` (`C:\Program Files\vex\vex.exe`).
4. Añade esa carpeta a tu variable de entorno `PATH` (**Configuración > Sistema > Acerca de > Configuración avanzada del sistema > Variables de entorno**).

Verifica la instalación abriendo una nueva ventana de PowerShell con `vex version`.

## Uso

**1. Inicializa el proyecto.** En el directorio de tu proyecto:

```sh
vex init
```

Responde unas preguntas breves (nombre, equipo, organización) y `vex` genera `vexconfig.yaml`. Con `-y` usa los valores por defecto sin preguntar.

**2. Ajusta la arquitectura (opcional).**

```sh
vex arq
```

**3. Ejecuta un paso en un ambiente.**

```sh
vex <paso> <ambiente>

vex test sand      # hasta el paso test, en sandbox
vex deploy prod    # todos los pasos hasta deploy, en producción
```

`<paso>` es hasta qué paso del pipeline se ejecuta (se hacen también los anteriores que hagan falta) y `<ambiente>` es el valor del ambiente que declara el pipeline. **El ambiente es obligatorio** en el modo local.

**4. Mira qué pasó y qué sigue.**

```sh
vex envs                  # ambientes del pipeline (y cuáles están protegidos)
vex steps                 # pasos del pipeline, en orden
vex check deploy sand     # ¿está todo listo? (no ejecuta nada)
vex ls sand               # últimos intentos
vex why                   # si el último falló: qué cambió desde la última vez que funcionó
vex log --failed          # la salida exacta del comando que falló
vex deployments prod      # despliegues de prod, y cuál está lanzado
vex rollback 91f4b55      # volver a los commits de ese despliegue
```

### Palabras que se repiten

| Palabra | Qué es |
| :--- | :--- |
| **paso** | Una etapa del pipeline (`test`, `package`, `deploy`…). `vex steps` los lista. |
| **ambiente** | Dónde se ejecuta (`sand`, `stag`, `prod`…). `vex envs` los lista. |
| **intento** | Cada vez que ejecutas un paso. Queda en el historial, salga bien o mal. |
| **despliegue** | Un intento que llegó hasta el final con éxito. Es lo que se puede lanzar o recuperar con `rollback`. |
| **lanzamiento** | Marcar un despliegue como el visible en su ambiente. Pasa solo con cada despliegue exitoso, salvo en los ambientes **protegidos** (`vex protect`), donde lo decides con `vex release`. |

Los ids que ves en las tablas son los últimos 7 caracteres; en cualquier comando vale ese id corto (mínimo 6) o el completo. Sin id, los comandos usan el último intento de este proyecto.

## `vexconfig.yaml`

```yaml
project:
  id: 9d8d4777-5707-43b3-a636-d6f8433b7a34   # lo genera vex init
  name: mi-servicio
  team: mi-equipo
  organization: mi-org
  description: mi despliegue con vex
  url: https://github.com/mi-org/mi-servicio.git   # repo del proyecto
  ref: main                                          # rama, tag o commit

pipeline:
  url: https://github.com/mi-org/mi-pipeline.git     # repo con el pipeline de despliegue
  ref: main

runtime:
  # Una imagen del registro ("imagen:tag") o la ruta de un Dockerfile que vex construye.
  image: jairoprogramador/vex-runtime-springboot-azure:latest
  build:                    # solo si image es un Dockerfile
    args:
      - name: MAVEN_VERSION
        value: 3.9.12
  run:
    volumes:                # carpetas extra que se montan en el contenedor
      - host: "/Users/yo/.m2"
        container: "/home/vex/.m2"
    envs:                   # credenciales que se leen de TU entorno
      - name: "ARM_CLIENT_ID"
        value: "$ARM_CLIENT_ID"
      - name: "ARM_CLIENT_SECRET"
        value: "$ARM_CLIENT_SECRET"

mode: local                 # opcional: "local" (por defecto) o "remote"
```

- **`runtime.image`**: si contiene `:` es una imagen del registro y se usa tal cual; si no, es la ruta de un Dockerfile y `vex` lo construye (`docker build`) con `build.args` y el uid/gid de tu usuario. La imagen tiene que incluir `vexd` y arrancarlo como `ENTRYPOINT ["vexd"]`.
- **`runtime.run.envs`**: el valor `"$NOMBRE"` se expande con **tu** entorno, así que los secretos nunca se escriben en el archivo. Esas variables viajan al motor por la entrada estándar (no por argumentos ni por `-e`), de modo que no aparecen en `docker inspect`. Si una variable no está definida, `vex` avisa y no la envía.
- **`runtime.run.volumes`**: se añaden a los montajes que `vex` hace por su cuenta.

## Modo local

Es el **modo por defecto**: ejecuta el motor en un contenedor de tu máquina, sin necesidad de configurar nada. Al ejecutar `vex <paso> <ambiente>`:

1. **Clona** `project.url` y `pipeline.url` en la caché del usuario y los fija al commit exacto de su `ref`.
2. **Prepara la imagen**: la construye si `runtime.image` es un Dockerfile, o usa la del registro.
3. **Crea el contenedor** y monta, de solo lectura, el proyecto (`/proyecto`) y el pipeline (`/pipeline`), más el historial y las carpetas de trabajo del motor.
4. **Pide la ejecución** al motor y muestra el avance en vivo:

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

Un paso que no cambió desde la última vez aparece como `reutilizado`: el motor solo hace el trabajo que hace falta. Si un comando falla, `vex` muestra su salida completa debajo del resumen.

> **Importante:** el motor trabaja sobre el **commit** de `project.ref` que `vex` clonó, no sobre tu directorio de trabajo. Los cambios sin commit (y sin push) no se despliegan.

**Ctrl+C** cancela de forma ordenada: `vex` le pide al motor que detenga el comando en curso, el intento queda registrado como `cancelado` y el contenedor no queda vivo.

**Códigos de salida:** `0` correcto · `1` fallo o error · `130` cancelado.

### Dónde guarda cosas

| Qué | Dónde |
| :--- | :--- |
| Historial del motor (la fuente de verdad, no se borra) | `~/.vex/almacen` |
| Clones del proyecto y del pipeline | `<caché del usuario>/vex/sources` |
| Espacio de trabajo y material del motor (derivables, se pueden borrar) | `<caché del usuario>/vex/espacio` y `…/material` |

La caché del usuario es `~/Library/Caches` en macOS y `~/.cache` en Linux.

## Modo y configuración

El modo de ejecución se resuelve, de mayor a menor prioridad, así: flag `--mode` › `vexconfig.yaml` (proyecto) › `~/.vex/config` (usuario) › ruta de sistema (global) › por defecto, **`local`**.

```sh
vex config mode                          # modo efectivo y de dónde viene
vex config mode=remote                   # lo fija en el proyecto (vexconfig.yaml)
vex config mode=remote --scope user      # lo fija para tu usuario
vex config unset mode                    # lo borra del proyecto
vex config list                          # todos los niveles
vex mode                                 # lo mismo, de forma interactiva
```

La ruta global es `/etc/vex/config` (Linux), `/usr/local/etc/vex/config` (macOS) o `%PROGRAMDATA%\Vex\config` (Windows).

## Referencia de comandos

**Ejecutar**

| Comando | Descripción |
| :--- | :--- |
| `vex <paso> <ambiente>` · `vex run <paso> <ambiente>` | Ejecuta el pipeline hasta ese paso en ese ambiente. `vex run` es la forma explícita: úsala si un paso se llama igual que un comando de vex. |
| `vex check <paso> <ambiente>` | Comprueba que todo está bien (ambiente, paso y variables) **sin ejecutar nada**. |

**Consultar** (solo lectura, modo local)

| Comando | Descripción |
| :--- | :--- |
| `vex envs` | Lista los ambientes del pipeline y cómo se lanza cada uno (automático o manual/protegido). |
| `vex steps` | Lista los pasos del pipeline, en orden, y avisa de los que chocan con un comando de vex. |
| `vex ls <ambiente>` | Últimos intentos de un ambiente (`-n` cuántos, `--all` todos). |
| `vex show [intento]` | Qué pasó en un intento, paso a paso. Sin argumentos, el último. |
| `vex log [intento]` | Salida de los comandos de un intento (`--failed` solo los fallidos). |
| `vex why [intento]` | Explica por qué falló un intento: qué cambió (código, instrucciones o variables) desde la última vez que funcionó. Sin argumentos, el último fallido. `--vs <despliegue>` compara con uno concreto. |
| `vex deployments <ambiente>` | Despliegues de un ambiente; marca el que está lanzado. |
| `vex releases <ambiente>` | Historial de lanzamientos de un ambiente. |

**Liberar un ambiente atascado**

| Comando | Descripción |
| :--- | :--- |
| `vex abandon [intento]` | Libera el ambiente que un intento muerto dejó ocupado. Normalmente no hace falta: el motor lo recupera solo al lanzar otro intento. Pide confirmación (`-y` para no preguntar) y **no detiene** comandos que el proceso siga ejecutando. |
| `vex rollback <despliegue>` | Vuelve a desplegar los mismos commits de un despliegue anterior (los ves con `vex deployments <ambiente>`). El motor recorre todos los pasos del pipeline, reutilizando los que no cambiaron; el despliegue nuevo queda como hijo del anterior. Clona proyecto y pipeline, así que ambos repositorios deben conservar esos commits. Pide confirmación (`-y` para no preguntar; sin terminal es obligatorio). |
| `vex release <ambiente> <despliegue> [--name v1]` | Hace visible un despliegue en su ambiente (el que `vex deployments` marca con `●`). Cada despliegue exitoso se lanza solo, salvo en los ambientes protegidos. Solo vale un despliegue del mismo ambiente. |
| `vex protect <ambiente>` / `vex unprotect <ambiente>` | `protect` evita que el ambiente lance solo: los lanzamientos los decides tú con `vex release`. **No** impide desplegar ni hacer rollback. `vex envs` muestra cuáles están protegidos. |

El id de un intento puede ser el completo o los últimos caracteres (mínimo 6), tal como lo ves en `vex ls`.

**Empezar y configurar**

| Comando | Descripción |
| :--- | :--- |
| `vex init` | Inicializa el proyecto y genera `vexconfig.yaml` (`-y` para usar los valores por defecto). |
| `vex arq` | Ajusta la arquitectura cloud según tus necesidades. |
| `vex config [clave \| clave=valor]` | Lee y escribe la configuración de la CLI (`list`, `unset`; `--scope project\|user\|global`). |
| `vex mode` | Elige el modo de ejecución de forma interactiva. |
| `vex version` | Muestra la versión instalada. |

**Modo remoto:** `vex login` · `vex logout` · `vex whoami` · `vex cancel <id>`.

| Flag | Descripción |
| :--- | :--- |
| `--mode local\|remote` | Modo de ejecución para esta invocación (por defecto, `local`). |
| `--no-check` | No comprueba antes de ejecutar que el paso está listo. Ahorra ~250 ms; un ambiente o paso mal escrito se explica igualmente, pero al fallar. |
| `--no-follow` | Solo en modo remoto: sale en cuanto la ejecución queda encolada, sin transmitir los logs. |

## Variables de entorno

| Variable | Descripción |
| :--- | :--- |
| `VEX_STORE_TEMPLATE` | URL del catálogo de plantillas de arquitectura que usan `vex init` y `vex arq`. Por defecto, el de `vex-template-store`. |
| `PROGRAMDATA` | Solo Windows: base de la ruta de configuración global. |
| _las que tú declares_ | Las que referencies con `$NOMBRE` en `runtime.run.envs`. |

## Modo remoto

En el modo remoto la ejecución no corre en tu máquina: `vex` se autentica contra el portal (`vex login`), dispara la ejecución y sigue los logs en vivo. Se activa con `--mode remote` o `vex config mode=remote`. Este modo todavía **no se ha migrado** al motor JSON-RPC descrito arriba; usa su propio protocolo con el portal.

## Solución de problemas

| Mensaje | Qué hacer |
| :--- | :--- |
| `Falta el ambiente. Uso: vex <paso> <ambiente>` | Indica el ambiente: `vex test sand`. |
| `El ambiente "x" ya tiene un intento en curso` | Otro intento usa ese ambiente. Espera a que termine. Si su proceso murió (por ejemplo, mataron el contenedor), un motor reciente lo recupera solo pasados unos 15 segundos; con un motor anterior, o si no quieres esperar, usa `vex abandon`. |
| `El ambiente «x» no existe en este pipeline` · `El paso «x» no existe…` | Error de tecleo: el mensaje sugiere el más parecido y lista los que hay (`vex envs`, `vex steps`). No se crea ningún intento. |
| `No encuentro ese id entre los que conozco` | `release` y `rollback` piden el id de un **despliegue** (`vex deployments <ambiente>`), no de un intento (`vex ls <ambiente>`). |
| `El pipeline no pasa la comprobación` | El pipeline tiene un error; el mensaje lista cada fallo con su archivo y su regla. |
| `La variable "x" no está disponible… (ambiente "y")` | Revisa que el ambiente exista en el pipeline y que la variable esté declarada (compartida o de ese ambiente). |
| `No se pudo ejecutar el motor en el contenedor` | Comprueba que Docker esté en ejecución y que la imagen exista. |
| `La imagen trae un vexd que no habla esta versión del protocolo` | Actualiza la imagen runtime: su `vexd` es anterior al protocolo JSON-RPC. |

## Desarrollo

```sh
go test ./...
```

Dos grupos de pruebas necesitan recursos externos y se omiten si no los hay:

```sh
# Contrato con el motor real: compara los tipos del CLI con lo que emite un vexd.
VEXD_BIN=/ruta/a/vexd go test ./internal/infrastructure/engine/...

# Integración con Docker real (usa una imagen con vexd y un pipeline de ejemplo).
VEX_IT_IMAGE=<imagen> VEX_IT_PIPELINE=<ruta> go test ./internal/infrastructure/engine/
```

> `vex` no importa código de `vex-engine`: los tipos del protocolo están copiados en `internal/infrastructure/engine/protocol` y el test de contrato avisa si el motor cambia algo.

## Licencia

Consulta el archivo [LICENSE](LICENSE) (Business Source License).
