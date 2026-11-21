# Solución de problemas

| Mensaje | Qué hacer |
| :--- | :--- |
| `Falta el ambiente. Uso: vex <step> <environment>` | Indica el ambiente: `vex test sand`. |
| `El ambiente "x" ya tiene un intento en curso` | Otro intento usa ese ambiente. Espera a que termine. Si su proceso murió (por ejemplo, mataron el contenedor), un motor reciente lo recupera solo pasados unos 15 segundos; con un motor anterior, o si no quieres esperar, usa `vex abandon`. |
| `El ambiente «x» no existe en este pipeline` · `El paso «x» no existe…` | Error de tecleo: el mensaje sugiere el más parecido y lista los que hay (`vex envs`, `vex steps`). No se crea ningún intento. |
| `No encuentro ese id entre los que conozco` | `release` y `rollback` piden el id de un **despliegue** (`vex deployments <ambiente>`), no de un intento (`vex ls <ambiente>`). |
| `El pipeline no pasa la comprobación` | El pipeline tiene un error; el mensaje lista cada fallo con su archivo y su regla. |
| `La variable "x" no está disponible… (ambiente "y")` | Revisa que el ambiente exista en el pipeline y que la variable esté declarada (compartida o de ese ambiente). |
| `El nombre del proyecto no es válido … → Corrige project.name en vexconfig.yaml` (o `project.id`, `project.team`, `project.organization`, o el nombre del ambiente) | El motor pone esos datos en los comandos del pipeline: solo letras sin acentos, números, `.`, `_` y `-`, sin espacios. Cambia el valor que indica el mensaje. |
| `No se pudo ejecutar el motor en el contenedor` | Comprueba que Docker esté en ejecución y que la imagen exista. |
| `La imagen trae un vexd que no habla esta versión del protocolo` | Actualiza la imagen runtime: su `vexd` es anterior al protocolo JSON-RPC. |

## Credenciales de nube (`cloud.credentials: portal`)

| Mensaje | Qué hacer |
| :--- | :--- |
| ``No hay conexión de nube activa para '<amb>'. Ejecuta `vex bootstrap <amb>` `` | El ambiente todavía no tiene conexión (o está a medio crear). Ejecuta el comando que indica. |
| ``El bootstrap del pipeline cambió: ejecuta `vex bootstrap <amb>` `` | La carpeta `bootstrap/<nube>/` del pipeline no es la que se aceptó al crear la conexión (o el pipeline pide otro contrato). Revisa el cambio y vuelve a aceptarlo con ese comando. |
| `el pipeline no declara bootstrap/<nube>/bootstrap.yaml` | El pipeline no está preparado para las credenciales del portal. Usa `credentials: env` o un pipeline que traiga su bootstrap. |
| `cloud.provider es "x" pero el pipeline solo trae bootstrap/y/` | Corrige `cloud.provider` en `vexconfig.yaml` o quítalo. |
| `este pipeline requiere el contrato de nube "…"` | Tu versión de `vex` no conoce ese contrato: actualiza `vex`. |
| `La ejecución se detuvo en el paso «…»: tu sesión de vex venció o fue revocada` | `vex login` y vuelve a ejecutar. |
| `La ejecución se detuvo en el paso «…»: la conexión de nube de '<amb>' ya no está activa` | Alguien desconectó el ambiente. ``vex bootstrap <amb>`` la vuelve a crear. |
| `La imagen de runtime trae un motor sin soporte para credenciales de nube del portal` | Actualiza `runtime.image` a una imagen con un `vexd` más reciente. |
| `no se pudo descargar la imagen de runtime` | Revisa la red y el nombre de `runtime.image`. Si la imagen ya está en tu equipo, `vex` usa la local. |

### Al conectar o desconectar la nube

| Mensaje | Qué hacer |
| :--- | :--- |
| `No encontré la CLI de Azure (az)` / `No hay una sesión de Azure en este equipo` | Instala `az` o ejecuta `az login` (y `az account set --subscription <id>` si tienes varias). |
| `vex bootstrap aplica permisos en tu nube … ejecútalo en una terminal interactiva` | No hay modo desatendido. En CI solo tiene sentido `vex bootstrap <amb> --check`. |
| `indica la región de Azure con --location` | La primera vez no hay conexión de la que tomarla. |
| `El bootstrap del pipeline no se puede aplicar (regla S…/V…)` | La plantilla del pipeline rompe una regla de `vex` (ver [Configuración](configuracion.md#conectar-la-nube-vex-bootstrap)). Lo arregla quien mantiene el pipeline; `vex` no tiene forma de saltársela. |
| `regla V4: … una forma que vex no conoce` | Tu versión de `az` devuelve la vista previa de otra manera. Actualiza `vex`; si persiste, repórtalo con la versión de `az`. |
| `la identidad de despliegue … ya confía en otros emisores` | La identidad tiene credenciales federadas que no puso `vex`. Quítalas o haz que la plantilla cree una identidad propia. |
| `ya tiene una credencial vex-oidc que no es la de esta conexión` | Es de otro proyecto, otro ambiente u otro emisor. Si ya no la usa nadie, bórrala con el comando que se imprime y repite. |
| `el portal propone un emisor de tokens distinto del de este binario de vex` | Tu `vex` y el portal no son del mismo entorno (desarrollo/producción). Instala la versión correcta. |
| `Este binario de vex no trae configurado el emisor de tokens` | Es una compilación local sin emisor: instala una versión oficial. |
| `solo quien administra el proyecto en el portal puede conectar o desconectar su nube` | Pídeselo a quien administra el proyecto. |
| `otra persona (u otra terminal) está conectando este ambiente` | Espera a que termine (como mucho 30 minutos). |
| `la conexión aún no se pudo verificar` / `quedó a medias` | Entra tarda en propagar la credencial. Vuelve a ejecutar `vex bootstrap <amb>`: retoma la verificación sin volver a desplegar. |
| `quedó revocado en el portal, pero la confianza de vex sigue en la nube` | La conexión ya no emite tokens. Ejecuta los comandos que se imprimen para borrar `vex-oidc` (si hay un bloqueo, primero el que lo quita). |
| `el portal no confirmó la revocación; no se tocó nada en la nube` | Reintenta `vex disconnect <amb>`. Mientras tanto la conexión sigue activa. |

Cuando un comando falla con un error conocido de la nube, `vex` añade debajo de su salida una línea `→` con lo que conviene hacer (también en `vex log --failed`). Para `azure/v1`:

| En la salida del comando | Causa | Qué hacer |
| :--- | :--- | :--- |
| `AADSTS70021` | La credencial federada que confía en vex se modificó o se borró en Azure. | `vex bootstrap <amb> --check` dice qué cambió; `vex bootstrap <amb> --reconnect` la rehace. |
| `AADSTS700024` | El token venció (la máquina se suspendió a mitad de un paso). | Vuelve a ejecutar. |
| `AuthorizationFailed`, `does not have authorization to perform action` | A la identidad de despliegue le falta un permiso, o se acaba de crear y Azure aún lo propaga. | Reintenta en 1 minuto; si persiste, revisa los roles que declara el bootstrap del pipeline. |
| `AADSTS700211`, `AADSTS50166`, `AADSTS7000215` | Azure no pudo validar la identidad de vex. | `vex bootstrap <amb> --check`; si hace falta, `vex bootstrap <amb> --reconnect`. |
| `vex-azure-login: … no apareció` / `timeout` | El token no llegó a tiempo al contenedor. | Revisa tu conexión a Internet y vuelve a ejecutar. |
| `vex-azure-login: … permiso` / `permission` | El contenedor no puede leer el token. | Ver «Linux: permisos del token». |

### Linux: permisos del token

Síntoma: `Permission denied` al leer `/vex/credenciales/oidc.jwt` o al escribir en `/vex/almacen`.

`vex` ejecuta el contenedor con tu usuario (`--user <uid>:<gid>`), así que el token (`0600`, en un directorio `0700`) solo lo lee quien debe. Si aun así falla:

- **Docker *rootless***: `docker info | grep -i rootless`. `vex` lo detecta y usa `--user 0:0`, que en ese modo es tu usuario.
- **La imagen no admite un UID arbitrario**: usa una imagen de runtime cuyo `/home/vex` sea escribible por cualquier usuario, o constrúyela con `DEV_UID`/`DEV_GID` iguales a los tuyos.
- **SELinux** (Fedora, RHEL): etiqueta los directorios que se montan: `chcon -R -t container_file_t ~/.cache/vex ~/.vex`.

Si una ejecución murió sin limpiar (un `kill -9`, un corte de luz), puede quedar un directorio en `<caché del usuario>/vex/credenciales/`: la siguiente ejecución borra los de más de una hora, y se pueden borrar a mano.

## Catálogo de errores

Cada error de nube, portal, bootstrap y cadena de suministro tiene un código que no cambia entre versiones. `vex` muestra siempre «qué pasó» y, en una línea `→`, «qué hacer»; el código es para buscarlo aquí. Ningún mensaje incluye tokens ni respuestas crudas del portal, del registro o de la nube.

| Código | Qué pasó | Qué hacer |
| :--- | :--- | :--- |
| `catalogo_firma_invalida` | No se pudo verificar la firma del catálogo de plantillas. | No se usó. Reintenta en unos minutos; si persiste, avisa en el repositorio `vex-template-store`. |
| `catalogo_retroceso` | El catálogo descargado es más antiguo que el último verificado. | No se usó. Lo arregla quien mantiene el catálogo publicando un release nuevo. |
| `catalogo_no_disponible` | No hay red ni copia verificada del catálogo. | Revisa la conexión. Una plantilla de `vex` no se usa sin compararla con su catálogo. |
| `catalogo_copia_antigua` | No se pudo descargar el catálogo y la copia verificada de este equipo tiene más de 7 días. | No se usó: una copia tan antigua puede ocultar una plantilla retirada. Revisa la conexión y repite. |
| `catalogo_copia_incompleta` | Hay una copia guardada del catálogo pero falta, o está dañada, su marca de fecha (`pipelines.json.meta`): no se puede comprobar que el catálogo no retrocede. | No se usó. Si no la tocaste tú, revisa quién escribe en `~/.vex`. Para empezar de cero borra `~/.vex/pipelines.json`, `~/.vex/pipelines.json.sigstore.json` y `~/.vex/pipelines.json.meta`. |
| `plantilla_no_coincide` | El bootstrap del pipeline no es el que publica el catálogo firmado para ese commit. | No se aplicó ni ejecutó nada. `vex arq` vuelve a la plantilla del catálogo. Si no cambiaste nada, avisa: puede ser una manipulación. |
| `imagen_sin_firma` | `runtime.image` no tiene firma de `vex`. | No se entregaron credenciales. Usa una imagen oficial (`vex arq`) o un Dockerfile propio. |
| `imagen_otra_identidad` | La imagen está firmada, pero no por el workflow oficial. | No se entregaron credenciales. Si es tuya, constrúyela con un Dockerfile en `runtime.image`. |
| `imagen_digest_movido` | El tag ya no apunta al digest fijado en `vexconfig.yaml`. | No se entregaron credenciales. Un tag de versión no cambia: revisa el release o toma la del catálogo con `vex arq`. |
| `imagen_registro` | No se pudo consultar el registro para verificar la firma. | Revisa la red y `docker login` si el registro es privado. Sin verificar no se ejecuta. |
| `contrato_no_soportado` | El pipeline pide un contrato de nube que tu `vex` no conoce. | Actualiza `vex` (`vex version`). |
| `config_nube_invalida` | `runtime.run.envs` define variables reservadas con `credentials: portal`. | Quítalas, o usa `credentials: env`. |
| `credenciales_portal_no_disponibles` | Tu compilación de `vex` no puede obtener credenciales del portal. | Instala una versión oficial. |
| `nombre_invalido` | Un dato del proyecto o el ambiente tiene caracteres que el motor no admite. | Corrige el campo que indica el mensaje, o usa un ambiente de `vex envs`. |
| `sin_credenciales_portal` | `vex bootstrap` en un proyecto sin `cloud.credentials: portal`. | Añade el bloque `cloud` a `vexconfig.yaml`. |
| `emisor_no_configurado` | El binario no trae el emisor de tokens (compilación local). | Instala una versión oficial. |
| `bootstrap_rechazado` | La plantilla rompe una regla S o V. | No se aplicó nada. Ver [Contrato para autores de plantillas](contrato-plantillas.md#reglas). |
| `bootstrap_no_aceptado` | No se confirmó la vista previa (o no se tecleó el ambiente). | Vuelve a ejecutar `vex bootstrap <ambiente>`. |
| `bootstrap_cambio_tras_vista_previa` | La carpeta `bootstrap/` cambió entre la vista previa y la aplicación. | No se aplicó nada. Vuelve a ejecutar `vex bootstrap <ambiente>`. |
| `bootstrap_desactualizado` | La conexión se aceptó con otro bootstrap que el del pipeline actual. | `vex bootstrap <ambiente>` muestra los cambios antes de aplicarlos. |
| `verificacion_pendiente` | La conexión no se activó en 5 minutos. | `vex bootstrap <ambiente>` retoma la verificación sin volver a desplegar. |
| `verificacion_fallida` | El portal probó la conexión y no funciona. | Ejecuta el comando que indica el mensaje. |
| `conexion_rechazada` | El portal no acepta la operación (otra conexión en curso, no administras el proyecto…). | Lo dice el mensaje; casi siempre, repetir `vex bootstrap <ambiente>`. |
| `auditoria_con_problemas` | `vex bootstrap <ambiente> --check` encontró diferencias. | `vex bootstrap <ambiente>` vuelve a aplicar lo que el pipeline declara. |
| `conexion_no_lista` | El ambiente no tiene una conexión activa. | `vex bootstrap <ambiente>`. |
| `acceso_nube_perdido` | La sesión de `vex` o la conexión dejaron de valer a mitad de un intento. | `vex login` o `vex bootstrap <ambiente>`, según diga el mensaje. |
| `rollback_sin_ambiente` | `vex` no sabe de qué ambiente es el despliegue. | `vex deployments <ambiente>` y repite el rollback. |
| `sesion_invalida` | La sesión de `vex` venció o fue cerrada. | `vex login`. |
| `sin_conexion` | `vex disconnect` en un ambiente sin conexión. | Nada que hacer; `vex bootstrap <ambiente> --check` muestra el estado. |
| `confianza_bloqueada` | Un bloqueo de la nube impide borrar `vex-oidc`. | Ejecuta, en orden, los comandos que `vex` imprime. |
| `az_no_instalado` | No está la CLI de Azure. | Instálala: <https://aka.ms/installazurecli>. |
| `az_sin_sesion` | No hay sesión de Azure. | `az login`. |
| `az_fallo` | Un comando de `az` terminó con error (se muestra su mensaje, acotado y sin tokens). | Revisa el detalle; `vex bootstrap <ambiente> --check` dice cómo quedó la conexión. |

Las pistas que `vex` añade bajo la salida de un comando fallido (tabla de arriba) también tienen código: `AADSTS70021`, `AADSTS700024`, `arm_403`, `identidad_no_validada`, `token_no_llego` y `token_sin_permiso`.

## El llavero del sistema no funciona

La CLI guarda el token del portal en el llavero del sistema (Keychain, Credential Manager o Secret Service). Si no hay llavero (Linux sin escritorio, contenedores), usa un archivo con permisos 0600 y lo avisa una vez por proceso. `vex whoami` muestra el almacén en uso (`Store:`).

Para saltar el llavero por completo, define `VEX_TOKEN_STORE=file` y vuelve a ejecutar `vex login`.

Ubicación del archivo:

| Sistema | Ruta |
| :--- | :--- |
| Linux | `~/.config/vex/credentials.json` |
| macOS | `~/Library/Application Support/vex/credentials.json` |
| Windows | `%AppData%\vex\credentials.json` |

Para borrar a mano la entrada del llavero en macOS: `security delete-generic-password -s vex -a vexPortalToken`.

## La CLI no abrió el navegador

`vex login` solo abre sola una URL que pertenezca al portal con que se compiló la CLI (mismo esquema, host y puerto). Si no coincide, imprime la URL y un aviso `no pertenece al portal configurado`, y no abre nada.

Causas posibles:

- El backend devolvió una URL de otro host que el `PORTAL_URL` de la compilación (por ejemplo, `www.` de más o un ambiente distinto).
- Build de desarrollo sin `portalauth/env.go` configurado: sin portal, la CLI no abre nada.

Qué hacer: copia la URL solo si coincide con el sitio oficial (`vexja.com`). Si no la reconoces, no entres ni escribas el código.
