# Fixtures de la vista previa (what-if)

**Estos archivos son SINTÉTICOS.** Se escribieron a mano siguiendo la forma documentada de
`az deployment sub what-if --no-pretty-print --output json` (campos `changes`, `potentialChanges`, `diagnostics`,
`error` y `status`; cada cambio con `resourceId`, `changeType`, `before`, `after`, `delta` y `unsupportedReason`).
No salen de una suscripción real.

| Archivo | Qué representa |
|---|---|
| `whatif-create.json` | Primera vez: todo se crea; los permisos aún no conocen la identidad; un recurso no se pudo previsualizar |
| `whatif-nochange.json` | Segunda vez: nada cambia; Azure ignora lo que la plantilla no declara, incluida la credencial `vex-oidc` que creó vex |

Las variantes que rompen cada regla se construyen en `whatif_test.go` a partir de estos dos.

Pendiente: sustituirlos por capturas reales (sin identificadores de la suscripción) cuando se ejecute `vex bootstrap`
contra una suscripción de prueba. Si la forma real difiere, la regla V4 hará que `vex bootstrap` se detenga sin aplicar
nada: eso es lo esperado, y se corrige ajustando el lector con su caso de prueba, nunca desactivando la regla.
