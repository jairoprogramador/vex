# Referencia de comandos

## Conceptos

| Palabra | Qué es |
| :--- | :--- |
| **paso** | Una etapa del pipeline (`test`, `package`, `deploy`…). `vex steps` los lista. |
| **ambiente** | Dónde se ejecuta (`sand`, `stag`, `prod`…). `vex envs` los lista. |
| **intento** | Cada vez que se ejecuta un paso. Queda en el historial, salga bien o mal. |
| **despliegue** | Un intento que llegó hasta el final con éxito. Es lo que se puede lanzar o recuperar con `rollback`. |
| **lanzamiento** | Marcar un despliegue como el visible en su ambiente. Ocurre solo con cada despliegue exitoso, salvo en los ambientes **protegidos** (`vex protect`), donde se decide con `vex release`. |

Los ids que aparecen en las tablas son los últimos 7 caracteres; en cualquier comando vale ese id corto (mínimo 6) o el completo. Sin id, los comandos usan el último intento de este proyecto.

## Ejecutar

| Comando | Descripción |
| :--- | :--- |
| `vex <step> <environment>` · `vex run <step> <environment>` | Ejecuta el pipeline hasta ese paso en ese ambiente. `vex run` es la forma explícita: úsala si un paso se llama igual que un comando de vex. |
| `vex check <step> <environment>` | Comprueba que todo está bien (ambiente, paso y variables) **sin ejecutar nada**. |

## Consultar (solo lectura)

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

## Operar ambientes

| Comando | Descripción |
| :--- | :--- |
| `vex abandon [intento]` | Libera el ambiente que un intento muerto dejó ocupado. Normalmente no hace falta: el motor lo recupera solo al lanzar otro intento. Pide confirmación (`-y` para no preguntar) y **no detiene** comandos que el proceso siga ejecutando. |
| `vex rollback <despliegue>` | Vuelve a desplegar los mismos commits de un despliegue anterior (se ven con `vex deployments <ambiente>`). El motor recorre todos los pasos del pipeline, reutilizando los que no cambiaron; el despliegue nuevo queda como hijo del anterior. Clona proyecto y pipeline, así que ambos repositorios deben conservar esos commits. Pide confirmación (`-y` para no preguntar; sin terminal es obligatorio). |
| `vex release <ambiente> <despliegue> [--name v1]` | Hace visible un despliegue en su ambiente (el que `vex deployments` marca con `●`). Cada despliegue exitoso se lanza solo, salvo en los ambientes protegidos. Solo vale un despliegue del mismo ambiente. |
| `vex protect <ambiente>` / `vex unprotect <ambiente>` | `protect` evita que el ambiente lance solo: los lanzamientos se deciden con `vex release`. **No** impide desplegar ni hacer rollback. `vex envs` muestra cuáles están protegidos. |

## Conectar la nube

Solo para proyectos con `cloud: {credentials: portal}`. Detalle en [Configuración](configuracion.md#conectar-la-nube-vex-bootstrap).

| Comando | Descripción |
| :--- | :--- |
| `vex bootstrap <ambiente> [--location <región>]` | Conecta la nube del ambiente con vex usando **tu** sesión de la nube (para Azure, `az login`): comprueba y aplica la plantilla `bootstrap/<nube>/` del pipeline, crea la credencial federada `vex-oidc` y espera a que el portal verifique la conexión. Muestra antes con qué cuenta, sobre qué suscripción, para qué proyecto, qué commit del pipeline y qué cambiaría; no aplica nada sin tu confirmación. Los permisos resaltados se aceptan **tecleando el nombre del ambiente**. Necesita una terminal interactiva: no hay `-y`. Volver a ejecutarlo retoma lo que quedó a medias. `--location` es obligatoria la primera vez. |
| `vex bootstrap <ambiente> --reconnect` | Rehace la conexión aunque el bootstrap del pipeline no haya cambiado (si cambió, `vex bootstrap <ambiente>` ya reconecta solo). **Corta el acceso del ambiente**: al aplicar, la confianza `vex-oidc` pasa de inmediato a la conexión nueva y la anterior deja de obtener tokens hasta que la nueva se verifique (hasta 5 minutos; si la verificación falla, hasta que `vex bootstrap <ambiente>` termine bien). |
| `vex bootstrap <ambiente> --check` | **Solo lee.** Audita la conexión punto por punto: huella del bootstrap, que la identidad de despliegue solo confíe en vex, que `vex-oidc` tenga el emisor, el subject y la audiencia esperados, que la plantilla no traiga cambios pendientes y que el canje de un token funcione. Sale con código `1` si algo no está como vex lo dejó. No necesita terminal interactiva. |
| `vex disconnect <ambiente>` | Revoca la conexión en el portal (si el portal no lo confirma, se detiene sin tocar la nube) y después borra `vex-oidc` con tu sesión de la nube. **No borra recursos ni permisos**: imprime los comandos para hacerlo tú. Si un bloqueo impide borrar la credencial, no lo quita: imprime los comandos exactos. Sale con `1` si la confianza sigue en la nube. Pide confirmación (`-y` para no preguntar). |

## Empezar y configurar

| Comando | Descripción |
| :--- | :--- |
| `vex init` | Inicializa el proyecto y genera `vexconfig.yaml` (`-y` para usar los valores por defecto). Toma la plantilla del [catálogo firmado](configuracion.md#catálogo-firmado-de-plantillas): escribe `pipeline.ref` con un commit, `runtime.image` con su digest y, si la plantilla declara un contrato de nube, el bloque `cloud`. |
| `vex arq` | Vuelve a elegir plantilla con el cuestionario de arquitectura y actualiza `pipeline`, `runtime.image` y, sin pisar lo que hayas escrito, `cloud`. |
| `vex version` | Muestra la versión instalada. |

## Flags globales

| Flag | Descripción |
| :--- | :--- |
| `--no-check` | No comprueba antes de ejecutar que el paso está listo. Ahorra ~250 ms; un ambiente o paso mal escrito se explica igualmente, pero al fallar. |

## Códigos de salida

`0` correcto · `1` fallo o error · `130` cancelado.
