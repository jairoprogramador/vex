# Casos de la revisión de seguridad de la CLI

**Estos archivos son SINTÉTICOS.** Son las plantillas de ataque y las vistas previas (what-if) que escribió a mano la
revisión de seguridad independiente para intentar saltarse las barandillas de `vex bootstrap`. No salen de ninguna
suscripción real y no se despliegan nunca: `revision_test.go` solo comprueba que vex las rechaza o las resalta.

| Archivo | Qué intenta | Qué hace vex |
|---|---|---|
| `00-control-fic.json` | Credencial federada en un despliegue anidado | Rechaza (S1) |
| `01-graph-fic-extension.json` | Credencial federada por la extensión de Microsoft Graph | Rechaza (S3: extensiones) |
| `02-pim-owner.json` | Owner mediante una solicitud programada (PIM) | Rechaza (S7) |
| `03-identidad-existente.json` | Devolver como identidad de despliegue una que ya existe | Rechaza (S6) |
| `05-principal-ajeno.json` | Contributor a un principal ajeno sobre un grupo ajeno | Resalta |
| `06-etiquetas-falsas.json` | Expresiones que aparentan otro grupo y otra identidad | Resalta y muestra la expresión entera |
| `07-tipo-concat.json`, `07b`, `07c` | Tipo de recurso con expresión, espacio o barra final | Rechaza (S1/S4) |
| `08-politica-dine.json` | Credencial federada dentro de una política | Rechaza (S1) |
| `09-contenedor-codigo.json` | Contenedor con identidad que ejecuta `az` | Rechaza (S2) |
| `10-rol-a-medida.json` | Rol a medida con el id de Reader | Resalta |
| `w-graph.json` | Vista previa con un recurso de extensión sin identificador | Rechaza (V4) |
| `w-otra-suscripcion.json` | Vista previa que toca otra suscripción | Rechaza (V5) |
| `w-pim.json` | Vista previa con una solicitud programada | Rechaza (V2) |
| `w-roldef-modify.json` | Vista previa que modifica una definición de rol | Resalta |
| `w-unsupported-deploy.json` | Motivo con ANSI, cambios de dirección y homoglifos | Lo escapa y exige confirmación escrita |

En las vistas previas, la suscripción de la sesión es `00000000-0000-0000-0000-000000000001`.
