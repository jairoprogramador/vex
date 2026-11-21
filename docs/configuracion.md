# Configuración

## `vexconfig.yaml`

`vex init` genera este archivo en la raíz del proyecto.

```yaml
project:
  id: 9d8d4777-5707-43b3-a636-d6f8433b7a34   # lo genera vex init (provisional); el portal puede reemplazarlo con el suyo
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
    envs:                   # credenciales que se leen del entorno del usuario
      - name: "ARM_CLIENT_ID"
        value: "$ARM_CLIENT_ID"
      - name: "ARM_CLIENT_SECRET"
        value: "$ARM_CLIENT_SECRET"

cloud:                      # opcional: de dónde salen las credenciales de nube
  credentials: portal       # portal (OIDC, sin secretos) | env (runtime.run.envs, como siempre)
  provider: azure           # opcional; si se escribe, debe coincidir con bootstrap/<nube>/ del pipeline
```

- **`project.id`, `project.name`, `project.team`, `project.organization`**: el motor los pone tal cual en los comandos del pipeline (`${var.project_name}`…), así que solo admiten letras sin acentos, números, `.`, `_` y `-`, sin espacios, empezando por letra o número (128 como mucho). Lo mismo vale para el nombre del ambiente. `vex` lo comprueba antes de lanzar nada y dice qué clave corregir.
- **`pipeline.ref` y `runtime.image` con el catálogo firmado**: `vex init` y `vex arq` escriben lo que fija el catálogo: `ref: <commit de 40 caracteres>` y `image: <repo>:<versión>@sha256:<digest>`. Si la plantilla declara un contrato de nube que tu `vex` conoce, escriben también `cloud: {provider: …, credentials: portal}`; no pisan un bloque `cloud` que ya exista, y `vex arq` quita el `credentials: portal` al pasar a una plantilla sin contrato. Un contrato que tu versión no conoce detiene el comando («actualiza vex»).
- **`runtime.image` por digest**: `repo:tag@sha256:…` (o `repo@sha256:…`) fija el contenido. Con `cloud.credentials: portal`, `vex` verifica la firma de la imagen antes de darle credenciales y la ejecuta por digest ([Seguridad](seguridad.md#la-imagen-de-runtime)).
- **`runtime.image`**: si contiene `:` es una imagen del registro y se usa tal cual; si no, es la ruta de un Dockerfile y `vex` lo construye (`docker build`) con `build.args` y el uid/gid del usuario. La imagen tiene que incluir `vexd` y arrancarlo como `ENTRYPOINT ["vexd"]`.
- **`runtime.run.envs`**: el valor `"$NOMBRE"` se expande con el entorno del usuario, así que los secretos nunca se escriben en el archivo. Esas variables viajan al motor por la entrada estándar (no por argumentos ni por `-e`), de modo que no aparecen en `docker inspect`. Si una variable no está definida, `vex` avisa y no la envía.
- **`runtime.run.volumes`**: se añaden a los montajes que `vex` hace por su cuenta.
- **`cloud`**: si el bloque existe, `credentials` es obligatorio. Con `portal` no se pueden definir `VEX_CLOUD_*`, `VEX_BOOTSTRAP_*` ni las variables propias del contrato de la nube (para `azure/v1`: `ARM_*`, `AZURE_*`, `VEX_AZURE_*`) en `runtime.run.envs`: `vex` se detiene con un mensaje que lo explica. Sin el bloque, o con `env`, todo funciona como siempre.

## Cómo se ejecuta

El motor corre en un contenedor de la máquina, sin configurar nada. Al ejecutar `vex <step> <environment>`:

1. **Clona** `project.url` y `pipeline.url` en la caché del usuario y los fija al commit exacto de su `ref`.
2. **Prepara la imagen**: la construye si `runtime.image` es un Dockerfile, o usa la del registro.
3. **Crea el contenedor** y monta, de solo lectura, el proyecto (`/proyecto`) y el pipeline (`/pipeline`), más el historial y las carpetas de trabajo del motor.
4. **Pide la ejecución** al motor y muestra el avance en vivo.

Un paso que no cambió desde la última vez aparece como `reutilizado`: el motor solo hace el trabajo que hace falta. Si un comando falla, `vex` muestra su salida completa debajo del resumen.

> El motor trabaja sobre el **commit** de `project.ref` que `vex` clonó, no sobre el directorio de trabajo. Los cambios sin commit (y sin push) no se despliegan.

**Ctrl+C** cancela de forma ordenada: `vex` le pide al motor que detenga el comando en curso, el intento queda registrado como `cancelado` y el contenedor no queda vivo.

### Dónde se guardan los datos

| Qué | Dónde |
| :--- | :--- |
| Historial del motor (la fuente de verdad, no se borra) | `~/.vex/almacen` |
| Clones del proyecto y del pipeline | `<caché del usuario>/vex/sources` |
| Espacio de trabajo y material del motor (derivables, se pueden borrar) | `<caché del usuario>/vex/espacio` y `…/material` |
| Token de nube de la ejecución en curso (solo con `credentials: portal`; se borra al terminar) | `<caché del usuario>/vex/credenciales/<id>` |
| Tiempos de cada ejecución (una línea JSON por ejecución; solo medidas, nada de la nube) | `<caché del usuario>/vex/tiempos.jsonl` |

La caché del usuario es `~/Library/Caches` en macOS y `~/.cache` en Linux.

## Credenciales de nube del portal

Con `cloud: {credentials: portal}`, `vex` no necesita ningún secreto: le pide al portal una identidad de vida corta para cada paso que la usa. Antes de crear ningún contenedor:

1. **Inicia sesión** si no hay una sesión válida (abre el navegador en el portal, igual que `vex login`).
2. **Registra el proyecto** en el portal y adopta lo que el portal diga de él (puede actualizar `vexconfig.yaml`).
3. **Lee `bootstrap/<nube>/`** del pipeline: su `bootstrap.yaml` dice qué contrato de nube exige y qué pasos reciben el token (`cloud_steps`).
4. **Consulta la conexión de nube** del ambiente. Si no hay una activa, o si la carpeta `bootstrap/<nube>/` cambió desde que alguien la aceptó, termina con la instrucción: ``Ejecuta `vex bootstrap <ambiente>` ``.

### Conectar la nube: `vex bootstrap`

La conexión la crea quien administra el proyecto, una vez por ambiente, con su propia sesión de la nube. Para Azure:

```
az login
vex bootstrap sand --location eastus2
```

La sesión de `az` no sale de tu equipo: al portal solo viajan identificadores (directorio, suscripción y lo que la plantilla devuelve). `vex` llama a `az` directamente, sin shell.

**Lo que define el pipeline y lo que impone `vex`.** Los recursos, las identidades y los permisos los decide la plantilla `bootstrap/<nube>/` del pipeline, en el commit que tienes configurado. Si esa plantilla es una de las de `vex` (vive en `github.com/jairoprogramador/…`), su bootstrap tiene que ser, byte a byte, el que publica el **catálogo firmado** para ese commit: si no coincide, o si el catálogo no se puede verificar, `vex bootstrap` y `vex run` se detienen. Cualquier otra plantilla (la de tu empresa, un fork) se permite, con el aviso **«Plantilla sin firma de vex: la revisas tú»** en la vista previa y en cada ejecución: lo único que te protege entonces es revisar lo que `vex` te enseña. Sea cual sea la plantilla, `vex` se niega a aplicarla si:

| Regla | Qué rechaza |
| :--- | :--- |
| S1 · S2 | La plantilla (o cualquier plantilla anidada) crea una credencial federada o un script de despliegue. La confianza la crea `vex`, nunca la plantilla. |
| S3 | Trae plantillas de fuera (`templateLink`, `relativePath`) o un despliegue anidado sin plantilla en línea. |
| S4 | No es JSON estricto: claves repetidas (también con otras mayúsculas), comentarios, más de 1 MiB o recursos sin tipo. |
| S5 | No es una plantilla de suscripción. |
| S6 | Pide parámetros sin valor por defecto distintos de `projectId`, `projectSlug`, `environment` y `location`; le falta una salida obligatoria (`deployIdentityId`, `deployClientId`, `deployPrincipalId`, `resourceGroup`); o devuelve salidas seguras. |
| V1 | La vista previa (`what-if`) borraría algo. |
| V2 | La vista previa crea o modifica una credencial federada o un script de despliegue. |
| V4 | La vista previa tiene una forma que `vex` no conoce. Falla cerrado: no se aplica nada. |

**Lo que ves antes de aceptar.** La cuenta y la suscripción de tu sesión, el proyecto y su dueño (con un aviso si no eres tú), el repositorio y el commit del pipeline, la huella del bootstrap, los permisos que declara la plantilla y la vista previa de la nube, con cada permiso como «identidad → rol → ámbito». Lo que la nube no supo previsualizar se lista aparte: nunca se esconde. Los nombres que vienen de la plantilla se muestran sin caracteres de control.

Un permiso sale **resaltado** (regla V3) si el rol es `Owner`, `User Access Administrator` o `Role Based Access Control Administrator`, si su ámbito es la suscripción o un grupo de administración, o si `vex` no puede determinar el rol o el ámbito. Con permisos resaltados no vale responder «sí»: hay que teclear el nombre del ambiente.

**Después de aceptar.** `vex` vuelve a calcular la huella (si la carpeta cambió, no aplica), despliega exactamente la plantilla que comprobó, valida lo que devuelve contra el contrato, comprueba que la identidad de despliegue no confíe en nadie más (si confía, se detiene) y crea la credencial federada `vex-oidc` con el emisor de tu versión de `vex`, el subject de esta conexión y la audiencia `api://AzureADTokenExchange`. Luego espera, hasta 5 minutos y mostrando el progreso, a que el portal canjee un token real. Si se agota el tiempo o pulsas Ctrl+C, vuelve a ejecutar `vex bootstrap <ambiente>`: retoma donde quedó.

El despliegue se llama `vex-bootstrap-<proyecto>-<ambiente>`. Azure admite 64 caracteres: si el nombre no cabe, `vex` recorta el ambiente y le añade seis caracteres de su sha256 (`vex-bootstrap-<proyecto>-<comienzo del ambiente>-<hash>`). El nombre es siempre el mismo para el mismo ambiente, así que `--check` y `vex disconnect` encuentran el mismo despliegue.

De cada aceptación y de cada desconexión queda una línea en `~/.vex/nube/aceptaciones.jsonl` (solo legible por ti): cuándo, cuenta, proyecto, ambiente, commit, huella y permisos resaltados aceptados. No guarda secretos.

**Reconectar corta el acceso del ambiente hasta que la conexión nueva se verifica.** Si el bootstrap del pipeline cambió, `vex bootstrap <ambiente>` crea una conexión nueva que sustituye a la anterior (`--reconnect` lo fuerza sin cambios). Una identidad solo confía en una conexión: al aplicar, `vex-oidc` pasa **de inmediato** a la conexión nueva y la anterior deja de obtener tokens, aunque el portal la siga mostrando activa. Mientras dura la verificación (normalmente menos de un minuto; hasta cinco) los `vex run` de ese ambiente fallan, y si la verificación no pasa siguen fallando hasta que `vex bootstrap <ambiente>` termine bien. La vista previa lo avisa en mayúsculas; no reconectes en mitad de un despliegue.

**Auditar.** `vex bootstrap <ambiente> --check` no cambia nada y sale con error si la conexión no está como `vex` la dejó. Necesita `az login` en la misma suscripción para mirar la nube; sin eso solo prueba el canje y lo dice.

**Desconectar.** `vex disconnect <ambiente>` revoca primero en el portal y solo después borra `vex-oidc`. Lo que debes saber:

- El corte no es instantáneo: un token de vex ya emitido vale hasta 10 minutos, y un token de Azure ya canjeado con él, hasta que venza (alrededor de una hora).
- No borra recursos, permisos ni el historial de despliegues: imprime los comandos (`az group delete …`, `az role assignment list …`).
- Si un bloqueo (`CanNotDelete`) protege las identidades, `vex` no lo quita: imprime, en orden, cómo quitarlo, borrar la credencial y volver a ponerlo.

### El manifiesto `bootstrap/<nube>/bootstrap.yaml`

```yaml
contract: azure/v1                       # obligatorio; la nube debe ser la de la carpeta
template: main.json                      # obligatorio; un archivo de esta carpeta
providers: [Microsoft.App]               # opcional
cloud_steps: [supply, package, deploy]   # obligatorio; los pasos que reciben el token
```

La lectura es estricta: cualquier otra clave se rechaza, el manifiesto no puede pasar de 16 KiB y la carpeta no admite enlaces simbólicos. La **huella** de la carpeta (un sha256 de todos sus archivos, hasta 64 archivos y 1 MiB) se compara con la que guardó la conexión: los finales de línea de Windows no la cambian; cualquier otro cambio, sí.

### Qué recibe el contenedor

- **El token**, como archivo: `/vex/credenciales/oidc.jwt`, en un directorio montado de **solo lectura**. El archivo solo existe mientras corre un paso de `cloud_steps`; fuera de ellos el directorio está vacío. Nunca viaja por argumentos, por `-e` ni por la petición al motor.
- **Las variables del contrato**, por la entrada estándar (como `runtime.run.envs`). No son secretas: identificadores y rutas. Valen para **todo el intento**: las ven también los pasos que no son de nube (`test`, por ejemplo). Lo único acotado por paso es el archivo del token.

Para cualquier nube: `VEX_CLOUD_CONTRACT` (el contrato, p. ej. `azure/v1`).

Para `azure/v1`:

| Variable | Qué es |
| :--- | :--- |
| `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID` | La identidad de despliegue, con los nombres estándar de Azure |
| `AZURE_FEDERATED_TOKEN_FILE` | `/vex/credenciales/oidc.jwt` |
| `AZURE_CONFIG_DIR` | `/tmp/vex-step-tmp/azure`: la sesión de `az` vive en el directorio efímero del paso (ver abajo) |
| `ARM_USE_OIDC` (`true`), `ARM_CLIENT_ID`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID`, `ARM_OIDC_TOKEN_FILE_PATH` | Los mismos valores, con los nombres que lee Terraform |
| `VEX_AZURE_RESOURCE_GROUP`, `VEX_AZURE_LOCATION` | Dónde se despliega |
| `VEX_AZURE_DEPLOY_IDENTITY_ID`, `VEX_AZURE_DEPLOY_PRINCIPAL_ID` | La identidad de despliegue como recurso |
| `VEX_AZURE_IDENTITY_RESOURCE_GROUP` | Grupo de recursos de las identidades, si la plantilla lo devuelve |
| `VEX_AZURE_PULL_IDENTITY_ID`, `VEX_AZURE_PULL_CLIENT_ID` | Identidad de descarga de imágenes, si la plantilla la crea |
| `VEX_AZURE_STATE_ACCOUNT`, `VEX_AZURE_STATE_CONTAINER` | Almacenamiento del estado, si la plantilla lo crea |
| `VEX_BOOTSTRAP_<NOMBRE>` | Cada salida extra de la plantilla del pipeline: `registryLoginServer` → `VEX_BOOTSTRAP_REGISTRY_LOGIN_SERVER` (un guion bajo antes de cada mayúscula) |

**La sesión de la nube no pasa de un paso a otro.** `vex` lanza el contenedor con `VEX_EFIMERO=/tmp`: el motor crea `/tmp/vex-step-tmp` vacío (solo para su usuario) antes de cada paso, lo borra al terminar y se lo anuncia a cada comando en `VEX_STEP_TMP`. Para `azure/v1`, `AZURE_CONFIG_DIR` apunta ahí dentro, así que lo que `az login` guarda desaparece con el paso. Además, al terminar cada paso el motor detiene todos los procesos que el paso dejó vivos; por eso `vex` no usa `docker run --init`, que lo impediría. Lo que una herramienta escriba fuera de ese directorio (en `HOME`, por ejemplo) sí lo ven los pasos siguientes.

Solo llegan valores con el formato esperado: si la conexión trae un dato que no lo cumple, `vex` se detiene antes de ejecutar.

En **Linux**, `vex` ejecuta el contenedor con tu usuario (`--user <uid>:<gid>`, o `0:0` con Docker *rootless*) y `HOME=/home/vex`, para que pueda leer el token sin ampliar los permisos del directorio (`0700`) ni del archivo (`0600`). En macOS y Windows no cambia nada.

### Límites conocidos

- El motor avisa de cada paso con eventos que puede descartar si nadie lee su salida a tiempo. `vex` no depende de recibirlos todos: cualquier evento de un paso le dice en cuál está el motor, y al terminar el intento borra el directorio pase lo que pase (también con Ctrl+C). Lo que no se puede recuperar: si se pierden a la vez el final de un paso de nube y todos los eventos del paso siguiente, ese paso corre con el token todavía montado.
- Si se pierde el comienzo de un paso de nube, el token aparece cuando termina su primer comando. Los pipelines llaman a `vex-azure-login` al inicio del paso, que espera unos segundos al archivo.
- Si el portal rechaza emitir el token (sesión vencida o conexión revocada), `vex` cancela el intento y dice qué ejecutar.
- El directorio efímero del paso necesita un `vexd` reciente. Uno anterior ignora `VEX_EFIMERO` sin avisar y `vex` no lo detecta (no consulta la versión del motor antes de ejecutar): con esa imagen, `az` crea él mismo `/tmp/vex-step-tmp/azure` y la sesión dura lo que dure el contenedor del intento, como antes. Actualiza `runtime.image`.
- De la salida de cada comando el motor conserva 16 MiB (termina con `[SALIDA TRUNCADA: …]` si había más) y de las salidas visibles, 32 valores de 4 KiB.
- Tras reconectar la nube a otro destino, los pasos se vuelven a ejecutar aunque nada más haya cambiado: `vex` le declara al motor un contexto derivado de la conexión. Hace falta una imagen de runtime con un `vexd` que lo conozca.

## Catálogo firmado de plantillas

`vex init` y `vex arq` eligen la plantilla y la imagen de un catálogo (`templates.json`) que se descarga del último release de [`vex-template-store`](https://github.com/jairoprogramador/vex-template-store/releases/latest) junto con su firma (`templates.json.sigstore.json`).

- **Qué se exige.** La firma tiene que ser de Sigstore (certificado de Fulcio registrado en un log de transparencia de certificados, firma en Rekor y hora atestiguada) y el certificado tiene que ser del workflow `publish-templates.yml` de ese repositorio, ejecutado desde un tag `vAAAA.MM.DD-N`, con emisor `https://token.actions.githubusercontent.com`. La raíz de confianza de Sigstore se obtiene por TUF y se guarda en `~/.vex/sigstore`.
- **Firma inválida o ausente: no se usa.** No se cae a la copia guardada ni a ninguna rama. `vex init` no crea el proyecto.
- **Retroceso.** `vex` recuerda cuándo se firmó el último catálogo aceptado (`~/.vex/pipelines.json.meta`) y rechaza uno más antiguo, aunque esté bien firmado.
- **Copia local.** El catálogo y su firma se guardan en `~/.vex/pipelines.json` y `.sigstore.json` durante 24 horas, y se vuelven a verificar cada vez que se leen. Sin red se usa esa copia aunque esté vencida; sin red y sin copia, `vex init` avisa y usa una plantilla genérica sin `ref` fijado ni bloque `cloud`.
- **Qué fija.** Cada entrada trae `ref` (commit), la imagen con su digest y, si declara `contract`, `bootstrap_sha256`: la huella de `bootstrap/<nube>/` en ese commit. Una entrada del catálogo firmado a la que le falte algo de eso no se usa.
- **Mientras el catálogo firmado no esté publicado**, un binario oficial no puede usar plantillas de `vex` con `cloud.credentials: portal` (ni elegirlas con `vex init`): falla cerrado. Para pruebas propias, compila `vex` tú mismo y usa `VEX_STORE_TEMPLATE` (elegir plantilla) y `VEX_DEV_UNSIGNED_TEMPLATES` (ejecutarla); las dos avisan.

## Variables de entorno

| Variable | Descripción |
| :--- | :--- |
| `VEX_STORE_TEMPLATE` | Catálogo **propio** de plantillas para `vex init` y `vex arq`: una URL `http(s)` o la ruta de un archivo. No se verifica ninguna firma y `vex` lo avisa cada vez; sus plantillas son «plantillas sin firma de vex». Se guarda aparte (`~/.vex/pipelines-propio.json`) y nunca sustituye al catálogo firmado al comparar una plantilla de `vex`. Sin esta variable se usa el catálogo firmado. |
| `VEX_DEV_UNSIGNED_TEMPLATES` | **Solo builds de desarrollo**, que hay que pedir al compilar (ver [Construir `vex` desde el código](#construir-vex-desde-el-código)); un `go build` sin más se comporta como un release. Con cualquier valor, `vex bootstrap` y `vex run` no consultan el catálogo firmado y tratan todas las plantillas, también las de `vex`, como plantillas sin firma, con un aviso «MODO DE DESARROLLO» en cada comando. Un binario publicado la ignora y lo dice. |
| _las declaradas por el usuario_ | Las que se referencian con `$NOMBRE` en `runtime.run.envs`. |

## Construir `vex` desde el código

Un binario se comporta como un release salvo que se pida lo contrario al enlazar: `go build ./cmd/vex` o `go install` no abren ningún atajo de desarrollo y, como no llevan los ids de quien firma, **no verifican el catálogo ni las imágenes** (fallan cerrado con un mensaje que lo explica).

Build local de desarrollo:

```bash
go build -ldflags "-X github.com/jairoprogramador/vex/internal/infrastructure/factories.buildChannel=dev" -o vex ./cmd/vex
```

Con `buildChannel=dev`:

- `VEX_DEV_UNSIGNED_TEMPLATES` funciona (con su aviso «MODO DE DESARROLLO»).
- Las firmas del catálogo y de las imágenes se verifican por nombre (cuenta, repositorio y workflow), sin exigir los ids numéricos.

Un release fija además, con `-X`, estas variables (las pone `goreleaser.yaml` desde las variables `SIGSTORE_OWNER_ID`, `SIGSTORE_CATALOG_REPOSITORY_ID` y `SIGSTORE_RUNTIME_REPOSITORY_IDS` del environment `release`):

| Variable de enlace | Valor |
| :--- | :--- |
| `github.com/jairoprogramador/vex/internal/infrastructure/sigstore.pinnedOwnerID` | Id numérico de la cuenta de GitHub que publica (`gh api users/jairoprogramador --jq .id`). Obligatorio |
| `github.com/jairoprogramador/vex/internal/infrastructure/sigstore.pinnedCatalogRepositoryID` | Id numérico de `vex-template-store` (`gh api repos/jairoprogramador/vex-template-store --jq .id`). Obligatorio |
| `github.com/jairoprogramador/vex/internal/infrastructure/sigstore.pinnedRuntimeRepositoryIDs` | Opcional: `vex-runtime-a=<id>,vex-runtime-b=<id>`. El repositorio listado debe tener ese id; el que no está en la lista queda cubierto por el id de la cuenta |

Para probar la verificación completa con un build propio, pasa las tres junto con `buildChannel=release`.
