# Solución de problemas

| Mensaje | Qué hacer |
| :--- | :--- |
| `Falta el ambiente. Uso: vex <step> <environment>` | Indica el ambiente: `vex test sand`. |
| `El ambiente "x" ya tiene un intento en curso` | Otro intento usa ese ambiente. Espera a que termine. Si su proceso murió (por ejemplo, mataron el contenedor), un motor reciente lo recupera solo pasados unos 15 segundos; con un motor anterior, o si no quieres esperar, usa `vex abandon`. |
| `El ambiente «x» no existe en este pipeline` · `El paso «x» no existe…` | Error de tecleo: el mensaje sugiere el más parecido y lista los que hay (`vex envs`, `vex steps`). No se crea ningún intento. |
| `No encuentro ese id entre los que conozco` | `release` y `rollback` piden el id de un **despliegue** (`vex deployments <ambiente>`), no de un intento (`vex ls <ambiente>`). |
| `El pipeline no pasa la comprobación` | El pipeline tiene un error; el mensaje lista cada fallo con su archivo y su regla. |
| `La variable "x" no está disponible… (ambiente "y")` | Revisa que el ambiente exista en el pipeline y que la variable esté declarada (compartida o de ese ambiente). |
| `No se pudo ejecutar el motor en el contenedor` | Comprueba que Docker esté en ejecución y que la imagen exista. |
| `La imagen trae un vexd que no habla esta versión del protocolo` | Actualiza la imagen runtime: su `vexd` es anterior al protocolo JSON-RPC. |

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
