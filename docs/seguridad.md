# Seguridad

Este documento dice qué protege `vex` cuando usa las credenciales del portal (`cloud.credentials: portal`), qué no protege y qué riesgo acepta quien lo usa. Va junto a [Configuración](configuracion.md) y al [Contrato para autores de plantillas](contrato-plantillas.md).

## Modelo

Tres piezas, tres dueños:

- **Tu nube** (hoy, una suscripción de Azure). Es tuya. `vex` nunca guarda en ella ni fuera de ella un secreto de larga vida.
- **El pipeline**: un repositorio con los pasos de despliegue y con `bootstrap/<nube>/`, la plantilla que crea la identidad de despliegue y sus permisos. Puede ser de `vex`, de tu empresa o de un tercero.
- **El portal de vex**: sabe qué proyecto, qué ambiente y qué conexión, y firma un token (JWT) de 10 minutos para cada paso de nube. No tiene acceso a tu nube: solo puede emitir un token que **tu** nube acepta porque tú creaste esa confianza.

La confianza es una credencial federada llamada `vex-oidc` sobre la identidad de despliegue: acepta tokens del emisor de `vex`, para un `subject` que nombra el proyecto, el ambiente y la conexión, con audiencia `api://AzureADTokenExchange`. La crea `vex bootstrap` con **tu** sesión de la nube (`az login`), que no sale de tu equipo.

## De qué se defiende

| Amenaza | Qué lo impide |
| :--- | :--- |
| Un secreto de nube filtrado (en el repo, en CI, en un portátil) | No existe: solo hay tokens de 10 minutos, emitidos por paso |
| Una plantilla que se da a sí misma (o a otro) acceso permanente | Reglas S1 y V2: una plantilla no puede crear credenciales federadas (se buscan en todo el documento, también dentro de una política); y `vex` no crea `vex-oidc` sobre una identidad que ya confía en otro emisor |
| Una plantilla que devuelve como identidad de despliegue una que ya existía y ya tiene permisos | La identidad tiene que estar declarada en la plantilla y aparecer en la vista previa con su id exacto (S6, V6). Antes de crear `vex-oidc`, `vex` lee los roles que la identidad tiene de verdad y pide confirmación escrita de cualquiera que la vista previa no enseñó |
| Una plantilla que ejecuta código con tu sesión de administrador, o que crea cómputo con una identidad | Regla S2 (sin scripts de despliegue ni recursos que ejecutan código) y S3 (sin plantillas remotas, `imports` ni extensiones) |
| Una plantilla que actúa fuera de la suscripción de tu sesión | S5 y V5: el despliegue es de una sola suscripción |
| Acceso privilegiado «a plazos» (PIM) o por política | S7: las solicitudes programadas de rol se rechazan. Definiciones de rol, políticas y bloqueos salen resaltados |
| Que el portal cambie el pipeline o la imagen que ejecutas | Si el portal propone otra URL o ref del pipeline, u otra imagen, `vex` enseña la diferencia y pide confirmación antes de escribir `vexconfig.yaml` y de clonar; sin terminal, se detiene |
| Una plantilla que borra | Regla V1 |
| Permisos excesivos que pasan inadvertidos | Vista previa con cada permiso como «identidad → rol → ámbito»; los de alto privilegio, resaltados y con confirmación escrita (V3) |
| Que el pipeline cambie sus permisos después de que los aceptaste | La conexión guarda la huella de `bootstrap/<nube>/`; si cambia, `vex run` se detiene y pide `vex bootstrap` |
| Que se aplique algo distinto de lo que viste | La huella se recalcula justo antes de aplicar |
| Una plantilla de `vex` manipulada, o un catálogo falso o antiguo | El catálogo va firmado y `vex` exige la identidad del workflow que lo publica, rechaza retrocesos y compara el bootstrap con la huella publicada |
| Una imagen de runtime manipulada que se lleva el token | `vex` verifica su firma antes de darle credenciales y la ejecuta por digest. Cada imagen la firma su propio repositorio, y el certificado lleva los ids numéricos de la cuenta y del repositorio |
| Que alguien bloquee la descarga del catálogo para ocultar una plantilla retirada | La copia verificada vale 7 días sin red; después, `vex` se detiene |
| Un binario de `vex` manipulado | Checksums firmados y procedencia SLSA de cada release ([Verificar la descarga](verificar-descarga.md)) |
| Un token de un proyecto usado contra otro | El `subject` lleva proyecto, ambiente y conexión; la nube solo acepta el de esa conexión |
| Texto de la plantilla, de la nube o del portal que falsifica la vista previa | Todo lo que viene de fuera se imprime solo con ASCII visible; cualquier otro carácter sale escapado (`\u{e9}`), también las letras que se parecen a otras |

## Las cinco barandillas

`vex` obedece al bootstrap del pipeline; no decide qué permisos se configuran. Lo que impone, sea cual sea la plantilla:

1. **La confianza la crea `vex`, y solo sobre una identidad del bootstrap.** La plantilla crea la identidad y sus permisos; la credencial federada `vex-oidc` la crea la CLI después, con el emisor fijado en el binario y el `subject` que da el servidor. Una plantilla que traiga credenciales federadas se rechaza. Toda identidad que la plantilla devuelve (`deployIdentityId`, `pullIdentityId`) tiene que estar declarada en ella como `Microsoft.ManagedIdentity/userAssignedIdentities` y salir en la vista previa con ese id exacto (como creación, o sin cambios o modificada si viene de un bootstrap anterior). Después de aplicar y antes de crear la confianza, `vex` lista los roles efectivos de la identidad de despliegue: cada rol que la vista previa no mostró se enseña (los privilegiados, resaltados) y hay que teclear el nombre del ambiente para seguir. Si la identidad ya existía y no tenía un `vex-oidc` de esta conexión, se enseñan todos. Vale igual al reconectar, y `--check` lo marca como fallo.
2. **Vista previa en lenguaje llano y confirmación.** Cuenta, suscripción, dueño del proyecto, repositorio y commit, pasos que recibirán el token y cada cambio. Nunca se acepta un borrado. No hay modo desatendido. Se pide confirmación escrita (teclear el ambiente) cuando hay permisos resaltados y cuando la nube no pudo previsualizar algún recurso.
3. **Huella de lo aceptado.** Se guarda el sha256 de la carpeta y el commit. Si la huella del pipeline que se va a ejecutar no es la de la conexión, no se ejecuta.
4. **Origen fijado y firmado.** El bootstrap se lee de un commit concreto. Si la plantilla es de `vex`, tiene que coincidir con el `bootstrap_sha256` del catálogo firmado; si el catálogo no se puede verificar, `vex` se detiene (falla cerrado). Una plantilla de fuera del catálogo se permite con el aviso «Plantilla sin firma de vex: la revisas tú».
5. **Contrato de salidas.** De la plantilla solo se consume un conjunto cerrado de datos, validados uno a uno; ninguno puede ser un secreto.

## Riesgo residual aceptado

**Quien acepta el bootstrap responde de lo que concede.** `vex` enseña lo que la plantilla va a hacer y se niega a unas pocas cosas, pero no juzga si los permisos son razonables: una plantilla puede dar `Contributor` sobre un grupo de recursos o `Owner` sobre la suscripción, y si la aceptas, queda concedido. La seguridad de tu suscripción depende de quien revisa la vista previa y de quien escribió (y, en el catálogo, firmó) la plantilla. `vex` no lo garantiza.

Esto vale sobre todo para las plantillas sin firma de `vex`: nadie más las revisó.

## La frontera es el intento, no el paso

El archivo del token solo existe durante los pasos de `cloud_steps`, y la sesión de `az` vive en un directorio que el motor borra al terminar cada paso. Eso reduce la exposición, pero **no es una frontera de seguridad entre pasos**:

- Todos los pasos de un intento corren en el mismo contenedor, con el mismo usuario y el mismo sistema de archivos. Un paso puede dejar algo (un archivo en `HOME`, un binario alterado, una herramienta envenenada) que un paso de nube ejecute después.
- Las variables del contrato (identificadores, no secretos) las ven todos los pasos.
- Si el motor pierde a la vez el final de un paso de nube y los eventos del siguiente, ese paso corre con el token aún montado.

Trata el intento entero como la unidad de confianza: **todo el código que corre en un intento, incluidas las pruebas y las dependencias que descargan, puede acabar actuando con la identidad de despliegue de ese ambiente.** Lo que acota el daño son los permisos de esa identidad (por eso se revisan en el bootstrap) y que cada ambiente tenga la suya.

## La revocación no es instantánea

`vex disconnect <ambiente>` revoca en el portal y borra `vex-oidc`. Después de eso:

- Un JWT de `vex` ya emitido sigue siendo válido hasta 10 minutos. Con `vex-oidc` borrada ya no se puede canjear; si el borrado en la nube falló (y `vex` lo dice), sí.
- Un token de acceso de Azure ya canjeado **sigue funcionando hasta que caduca, alrededor de una hora**. Azure no permite revocarlo. `vex-azure-logout` borra la copia local, no el token.
- Los permisos y los recursos que creó la plantilla no se tocan: `vex` imprime los comandos para borrarlos.

Si sospechas de un compromiso, además de desconectar: quita los roles de la identidad de despliegue (o bórrala), que es lo único con efecto inmediato sobre un token ya canjeado.

Reconectar tampoco es instantáneo en el otro sentido: al aplicar, `vex-oidc` pasa a la conexión nueva y **la anterior deja de obtener tokens hasta que la nueva se verifica** (hasta 5 minutos, o hasta que `vex bootstrap` termine bien si la verificación falla).

## La imagen de runtime

Con `cloud.credentials: portal`, antes de crear ningún contenedor y de abrir la sesión de nube:

- Una imagen de registro (`repo:tag`, `repo:tag@sha256:…`, `repo@sha256:…`) tiene que estar firmada con Sigstore por el workflow `docker-publish.yml` de **su** repositorio —la imagen `…/vex-runtime-x` la firma `jairoprogramador/vex-runtime-x`, no otro runtime—, ejecutado desde un tag de versión. Sin firma, con otra identidad o si no se puede consultar el registro: no se ejecuta. No hay opción para saltarlo.
- Los nombres de GitHub se pueden volver a registrar; los ids numéricos, no. El binario publicado lleva el id de la cuenta y el del repositorio del catálogo (y, opcionalmente, los de los repositorios de runtime) y exige que el certificado de la firma los traiga. Un binario de release sin esos ids no verifica nada: falla cerrado y lo dice.
- Se ejecuta `repo@sha256:…`, el digest verificado. Si solo escribiste un tag, `vex` lo resuelve, avisa y te dice qué digest fijar.
- Si fijaste `tag@digest` y el tag ya apunta a otro digest, se rechaza: un tag de versión que cambia es señal de ataque.
- Una imagen que se construye desde un Dockerfile tuyo no se puede verificar: se acepta con un aviso. Es tu código.

Con `credentials: env` o sin bloque `cloud` no se verifica nada: los secretos son tuyos y `vex` no entrega ninguno.

## Qué se guarda en tu equipo

| Qué | Dónde | Sensible |
| :--- | :--- | :--- |
| Sesión de `vex` (token del portal) | Llavero del sistema; si no hay, un archivo solo legible por ti | Sí |
| JWT del paso en curso | `<caché del usuario>/vex/credenciales/<id>/oidc.jwt` (`0600` en un directorio `0700`); se borra al terminar el paso y el intento. Junto al directorio hay un `<id>.lock` vacío que la ejecución mantiene bloqueado: otra ejecución de `vex` no limpia el directorio de una que sigue viva. Si `credenciales` ya existe y es un enlace, no es tuyo o no es `0700`, `vex` se detiene | Sí, 10 minutos |
| Constancia de lo que aceptaste y desconectaste | `~/.vex/nube/aceptaciones.jsonl` (`0600`): fecha, cuenta, proyecto, ambiente, commit, huella, permisos resaltados | No lleva secretos |
| Catálogo firmado, su firma y la fecha de firma | `~/.vex/pipelines.json`, `.sigstore.json`, `.meta` | No |
| Raíz de confianza de Sigstore | `~/.vex/sigstore` | No |
| Historial del motor | `~/.vex/almacen` | Salidas de tus comandos: lo que tu pipeline imprima |
| Clones, espacio de trabajo, tiempos | `<caché del usuario>/vex/…` | Tu código |

La sesión de `az` es de la CLI de Azure, no de `vex`: `vex` la usa (sin shell, con argumentos validados) y no la copia. Al portal solo viajan identificadores: directorio, suscripción y las salidas de la plantilla.

## Límites conocidos

- **`--check` ve los roles directos de la identidad, no los heredados por grupo.** Comprueba la huella, que la identidad solo confíe en `vex`, que `vex-oidc` tenga el emisor, el `subject` y la audiencia esperados, que la plantilla no traiga cambios pendientes, que el canje funcione y que la identidad de despliegue no tenga roles que la plantilla no declara (`az role assignment list --assignee-object-id … --all`, con tu sesión). No ve lo que la identidad herede por pertenecer a un grupo de Entra ID, ni permisos de fuera de Azure RBAC (Graph, políticas de acceso de Key Vault).
- **Un segundo bootstrap sobre recursos que ya existen resalta más.** Un rol solo se considera «conocido» si su ámbito lo crea esta misma vista previa y su identidad es del bootstrap. Si reintentas tras un fallo a medias, o la plantilla añade un rol sobre un grupo que ya existía, sale resaltado y pide confirmación escrita.
- **La copia del catálogo caduca.** Sin red, la copia verificada se usa hasta 7 días. Si a la copia le falta su marca de fecha (`pipelines.json.meta`), `vex` se detiene en vez de tratarla como la primera descarga.
- **Un motor antiguo ignora el directorio efímero.** Una imagen con un `vexd` anterior no crea ni borra `/tmp/vex-step-tmp`, no avisa, y `vex` no lo detecta: la sesión de `az` dura entonces lo que dure el contenedor del intento. Usa una imagen de runtime actual.
- **La verificación de firmas necesita red** la primera vez (raíz de confianza de Sigstore) y cada vez que consulta el registro de la imagen. Sin red, un proyecto con `credentials: portal` y una imagen de registro no se ejecuta.
- **Las plantillas de `vex` dependen del catálogo vigente.** Una plantilla de `vex` en un commit que el catálogo ya no publica se trata como plantilla sin firma (con aviso), no se bloquea.
- **El portal ve** qué proyecto, ambiente y commit ejecutas y cuándo pides un token. No ve tu código ni la salida de tus comandos en la ejecución local.
- **`VEX_STORE_TEMPLATE`** apunta `vex init` a un catálogo sin firma. Avisa siempre, pero quien controle tu entorno puede definirla; revisa el `vexconfig.yaml` que resulte.
- **La ejecución remota** (en la infraestructura del portal) no verifica todavía la firma de la imagen ni conserva el digest.

## Reportar un problema de seguridad

Escribe a quien mantiene `vex` por un canal privado (aviso de seguridad en el repositorio de GitHub) antes de abrir un issue público.
