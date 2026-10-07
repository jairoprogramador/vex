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

## Empezar y configurar

| Comando | Descripción |
| :--- | :--- |
| `vex init` | Inicializa el proyecto y genera `vexconfig.yaml` (`-y` para usar los valores por defecto). |
| `vex version` | Muestra la versión instalada. |

## Flags globales

| Flag | Descripción |
| :--- | :--- |
| `--no-check` | No comprueba antes de ejecutar que el paso está listo. Ahorra ~250 ms; un ambiente o paso mal escrito se explica igualmente, pero al fallar. |

## Códigos de salida

`0` correcto · `1` fallo o error · `130` cancelado.
