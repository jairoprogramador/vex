# Contrato para autores de plantillas

Este documento es para quien escribe o mantiene un pipeline que usa las credenciales del portal (`cloud.credentials: portal`). Dice qué tiene que traer el pipeline, qué le pasa `vex`, qué espera de vuelta y qué reglas no se pueden saltar. El contrato de hoy es `azure/v1`.

El principio: **`vex` no decide qué permisos se configuran; obedece al bootstrap del pipeline, y quien lo acepta responde de lo que concede.** Lo único que es de `vex` es la confianza: la credencial federada `vex-oidc`.

## La carpeta `bootstrap/<nube>/`

```
bootstrap/azure/bootstrap.yaml   # manifiesto
bootstrap/azure/main.bicep       # fuente (opcional)
bootstrap/azure/main.json        # ARM JSON compilado: es lo que se aplica
```

Se aplica el JSON compilado: no exige Bicep en la máquina de quien acepta. El CI del pipeline debe comprobar que `main.json` corresponde a `main.bicep`.

La carpeta no admite enlaces simbólicos, ni más de 64 archivos, ni más de 1 MiB en total. Su **huella** es un sha256 de todos sus archivos (ruta y contenido; los finales de línea de Windows y el BOM no la cambian). Esa huella es lo que acepta quien conecta la nube, lo que guarda la conexión y lo que el catálogo firmado publica como `bootstrap_sha256`: **cualquier cambio en la carpeta obliga a volver a ejecutar `vex bootstrap <ambiente>`** y, si la plantilla está en el catálogo, a publicar una entrada nueva.

## Manifiesto

```yaml
contract: azure/v1                       # obligatorio; la nube es la de la carpeta
template: main.json                      # obligatorio; un archivo de esta carpeta, sin separadores de ruta
providers: [Microsoft.App]               # opcional; espacios de nombres que la suscripción debe tener registrados
cloud_steps: [supply, package, deploy]   # obligatorio, no vacío; los pasos que reciben el token
```

Lectura estricta: cualquier otra clave se rechaza y el archivo no puede pasar de 16 KiB. Declara en `cloud_steps` **solo** los pasos que hablan con la nube: fuera de ellos el token no existe en el contenedor.

## Parámetros que recibe la plantilla

| Parámetro | Qué es |
| :--- | :--- |
| `projectId` | El id del proyecto (uuid) |
| `projectSlug` | Nombre corto del proyecto: `[a-z0-9-]`, 20 caracteres como mucho, terminado en seis caracteres del id |
| `environment` | El ambiente (`sand`, `prod`…) |
| `location` | La región (`--location`, o la de la conexión que ya existe) |

La plantilla debe declarar los cuatro. Cualquier otro parámetro necesita valor por defecto. Es un despliegue **de suscripción** llamado `vex-bootstrap-<slug>-<ambiente>` (acotado a 64 caracteres con un sufijo de hash si el ambiente es muy largo).

## Salidas

| Salida | Obligatoria | Llega al contenedor como |
| :--- | :--- | :--- |
| `deployIdentityId` (id de recurso de la identidad de despliegue) | sí | `VEX_AZURE_DEPLOY_IDENTITY_ID` |
| `deployClientId` | sí | `AZURE_CLIENT_ID`, `ARM_CLIENT_ID` |
| `deployPrincipalId` | sí | `VEX_AZURE_DEPLOY_PRINCIPAL_ID` |
| `resourceGroup` | sí | `VEX_AZURE_RESOURCE_GROUP` |
| `identityResourceGroup` | no | `VEX_AZURE_IDENTITY_RESOURCE_GROUP` |
| `pullIdentityId`, `pullClientId` | no (las dos o ninguna) | `VEX_AZURE_PULL_IDENTITY_ID`, `VEX_AZURE_PULL_CLIENT_ID` |
| `stateAccount`, `stateContainer` | no (las dos o ninguna) | `VEX_AZURE_STATE_ACCOUNT`, `VEX_AZURE_STATE_CONTAINER` |
| cualquier otra salida `string` | — | `VEX_BOOTSTRAP_<NOMBRE>` |

**Extras.** Una salida extra se llama `^[a-z][A-Za-z0-9]{0,40}$` y su variable se forma poniendo un guion bajo antes de cada mayúscula: `registryLoginServer` → `VEX_BOOTSTRAP_REGISTRY_LOGIN_SERVER`. Como mucho 32. Los valores: una línea, imprimibles, 512 bytes como mucho, sin forma de secreto ni de token. Las salidas `securestring` se rechazan: **por las salidas no viajan secretos**; son identificadores y nombres, y las ven todos los pasos del intento.

El directorio y la suscripción salen de la sesión de quien acepta, no de la plantilla.

## Reglas

`vex` las aplica sea cual sea la plantilla. No hay forma de desactivarlas.

Sobre la plantilla, antes de hablar con la nube (también en las plantillas anidadas):

| Regla | Se rechaza |
| :--- | :--- |
| S1 | Cualquier `…/federatedIdentityCredentials`, en cualquier clave `type` del documento y a cualquier profundidad (también dentro de la `policyRule` de una política). La confianza la crea `vex`, nunca la plantilla |
| S2 | `Microsoft.Resources/deploymentScripts`, buscado igual que S1, y los recursos que ejecutan código: `Microsoft.ContainerInstance/containerGroups`, `Microsoft.App/jobs`, `Microsoft.App/containerApps`, `Microsoft.Compute/virtualMachines` y `virtualMachineScaleSets`, `Microsoft.Web/sites`, `Microsoft.Automation/automationAccounts`, `Microsoft.Logic/workflows`, `Microsoft.Batch/batchAccounts`, tareas de `Microsoft.ContainerRegistry` y `Microsoft.Resources/deploymentStacks`. El bootstrap prepara identidad, permisos y lo mínimo para desplegar; el cómputo lo crean los pasos del pipeline |
| S3 | `templateLink`, `relativePath`, un despliegue anidado sin plantilla en línea, `imports`/`extensions` (p. ej. la extensión de Microsoft Graph) y `properties.template` en algo que no sea `Microsoft.Resources/deployments` |
| S4 | JSON no estricto: claves repetidas (también con otras mayúsculas), comentarios, más de 1 MiB, recursos sin tipo. Y todo `type` que no sea un literal con la forma `Proveedor.Nombre/tipo[/subtipo…]` (`^[A-Za-z0-9.]+(/[A-Za-z0-9]+)+$`): sin expresiones, espacios, `/` final ni `@versión` |
| S5 | Un `$schema` que no sea de despliegue de suscripción; un despliegue anidado con `subscriptionId` o `scope` (todo va a la suscripción de quien acepta) |
| S6 | Parámetros sin valor por defecto fuera de los cuatro de `vex`; salidas obligatorias ausentes; salidas seguras. Un valor por defecto con `newGuid()` o con `utcNow()` (cambian en cada despliegue: se aplicaría algo distinto de lo previsualizado); solo se admite `utcNow` con un formato literal de año y mes, como `utcNow('yyyy-MM-01')`. Una plantilla que no declara ninguna `Microsoft.ManagedIdentity/userAssignedIdentities`, o cuyo `deployIdentityId` o `pullIdentityId` es un id literal o el de una identidad que la plantilla no declara |
| S7 | `Microsoft.Authorization/roleAssignmentScheduleRequests` y `roleEligibilityScheduleRequests` (PIM): un bootstrap no tiene motivo para programar accesos |

Sobre la vista previa (`what-if`):

| Regla | Efecto |
| :--- | :--- |
| V1 | Un borrado (`Delete`) → se rechaza |
| V2 | Aparece una credencial federada, un script de despliegue, un recurso que ejecuta código o una solicitud programada de rol → se rechaza |
| V3 | Cada permiso se lista como «identidad → rol → ámbito» y los resaltados piden teclear el nombre del ambiente (ver abajo) |
| V4 | Una respuesta con una forma que `vex` no conoce → se rechaza (falla cerrado) |
| V5 | Un cambio cuyo id o cuyo ámbito está en otra suscripción que la de la sesión → se rechaza |
| V6 | `deployIdentityId` o `pullIdentityId` no aparece en la vista previa con su id exacto (creada, sin cambios o modificada) → se rechaza antes de crear la confianza |

### Qué sale resaltado

Un permiso resaltado no se rechaza: obliga a quien acepta a teclear el nombre del ambiente. Sale resaltado:

- `Owner`, `User Access Administrator`, `Role Based Access Control Administrator`, y cualquier rol, identidad o ámbito que no se puede determinar sin desplegar.
- Un ámbito de suscripción o de grupo de administración.
- Un rol para un `principalId` que no es el de una identidad creada por esta plantilla.
- Un rol sobre un ámbito que no crea esta plantilla. `Contributor` y `Managed Identity Operator` solo se consideran conocidos sobre un grupo o un recurso que la plantilla crea.
- Todo `Microsoft.Authorization/*` que no sea una asignación de rol: definiciones de rol, asignaciones, definiciones y conjuntos de políticas, exenciones, bloqueos. Una asignación de política con `identity`, siempre.
- Cualquier rol nuevo cuando la vista previa no crea su ámbito: un segundo bootstrap que añade un rol sobre un grupo que ya existía, o un reintento tras un fallo a medias.
- Además, si la nube no puede previsualizar algún recurso, también se pide la confirmación escrita.

Para que una plantilla salga sin resaltados: declara en ella las identidades y el grupo de recursos del ambiente, asigna roles solo a esas identidades y solo sobre lo que la plantilla crea, y usa ids de rol y tipos literales. Las etiquetas de la vista previa muestran el literal o el `parameters('x')`; cualquier otra expresión se enseña entera (acotada), no resumida.

Después de aplicar, `vex` lee los roles efectivos de `deployIdentityId` y, si tiene alguno que la vista previa no mostró, lo enseña y pide confirmación escrita. Luego crea `vex-oidc`. Si esa identidad ya confía en otro emisor, se detiene: **la identidad de despliegue debe ser propia del ambiente**, creada por la plantilla, y no llevar otras credenciales federadas ni roles de otro origen.

Los nombres de grupo de recursos y de identidad empiezan por una letra o un número.

## Qué recibe cada paso

- `VEX_CLOUD_CONTRACT=azure/v1`.
- Las variables de la tabla de salidas, más `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID`, `VEX_AZURE_LOCATION` y sus equivalentes para Terraform (`ARM_USE_OIDC=true`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID`, `ARM_OIDC_TOKEN_FILE_PATH`).
- `AZURE_FEDERATED_TOKEN_FILE=/vex/credenciales/oidc.jwt`: el token, en un directorio de solo lectura. **Solo existe durante los pasos de `cloud_steps`.** Dura 10 minutos y `vex` lo renueva en el mismo archivo.
- `AZURE_CONFIG_DIR=/tmp/vex-step-tmp/azure` y `VEX_STEP_TMP=/tmp/vex-step-tmp`: un directorio que el motor crea vacío antes de cada paso y borra al terminar. Todo lo que guarde una sesión (la de `az`, un `kubeconfig`) debe ir ahí, no en `HOME`.

Las variables valen para todo el intento (las ven también `test` o `build`); lo único acotado por paso es el archivo del token. El usuario no puede definir en `runtime.run.envs` nada que empiece por `VEX_CLOUD_`, `VEX_BOOTSTRAP_`, `VEX_AZURE_`, `AZURE_` o `ARM_`.

## `vex-azure-login` y `vex-azure-logout`

Vienen en las imágenes de runtime oficiales (`vex-runtime-kit`).

```yaml
- name: login azure
  cmd: vex-azure-login
```

- **`vex-azure-login`**: llámalo al **inicio de cada paso de nube**, también antes de Terraform. Espera al archivo del token (15 s por defecto, `VEX_LOGIN_WAIT_SECONDS`), inicia sesión de `az` con él y selecciona la suscripción. Sale con `64` (contrato o variables inválidos), `65` (el token no apareció o no se puede leer) o `66` (`az login` falló).
- **`vex-azure-logout`**: `az logout` y `az account clear`. Es idempotente. No hace falta si la sesión vive en `AZURE_CONFIG_DIR`, que desaparece con el paso; úsalo si tu imagen no trae un motor con directorio efímero.
- Un paso que dure más que el token de acceso de Azure (alrededor de una hora) debe volver a llamar a `vex-azure-login`, o usar herramientas que lean el archivo cada vez (Terraform con `ARM_OIDC_TOKEN_FILE_PATH`).
- No copies el token a otro sitio, no lo imprimas y no lo pases por argumentos.

## Despliegue por digest

Lo que se despliega se nombra por su contenido, no por una etiqueta: `package` construye la imagen y anota su digest (`sha256:…`), y `deploy` aplica `<imagen>@sha256:…`. Una etiqueta se puede mover a otra imagen entre `package` y `deploy`; un digest, no. Lo mismo vale para la imagen de runtime del propio pipeline: el catálogo la publica como `repo:versión@sha256:…`.

## Publicar una plantilla en el catálogo de vex

Una entrada del catálogo firmado fija el commit (`ref`), la imagen de runtime con su digest (firmada por el workflow `docker-publish.yml` del repositorio `vex-runtime-*` que se llama como la imagen), el `contract` y `bootstrap_sha256`. Con eso, `vex bootstrap` y `vex run` comprueban que el bootstrap que van a usar es exactamente el publicado. Una plantilla fuera del catálogo funciona igual, con el aviso «Plantilla sin firma de vex: la revisas tú».

## Cambios que rompen

- Cambiar algo en `bootstrap/<nube>/` cambia la huella: cada ambiente conectado pide `vex bootstrap <ambiente>` antes de volver a ejecutar, y **reconectar corta el acceso de ese ambiente hasta que la conexión nueva se verifica**. Agrupa los cambios.
- Quitar una salida que tus pasos usan, o renombrar un extra, rompe los pasos sin que `vex` lo detecte.
- Un contrato nuevo (`azure/v2`) necesita una versión de `vex` que lo conozca: las anteriores se detienen con «actualiza vex».
