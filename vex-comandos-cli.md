# Comandos principales de Vex

Diseño de los comandos que explotan el registro de despliegue. Cada uno
responde a un momento distinto en el que el desarrollador necesita tomar
una decisión — no a "qué datos hay disponibles".

| Momento | Decisión que hay que tomar | Ventana aceptable |
|---|---|---|
| Antes de desplegar | ¿aprieto el botón? | segundos |
| Al fallar | ¿es mi código o es el entorno? | inmediato |
| Al tener éxito | ¿de verdad salió bien, o solo no truena? | un vistazo |
| Explorando | ¿dónde está el problema recurrente? | minutos, con calma |

Principio transversal: **ningún comando muestra un número aislado. Todos
comparan contra algo** — el último éxito, el promedio histórico, otro
entorno. Nunca predecir, siempre comparar.

---

## 1. Antes de desplegar — `vex plan`

El único comando que puede evitar el fallo en vez de solo explicarlo.

```
$ vex plan --env prod

  api-pagos → prod-lima

  vs. último éxito (7f8a20, hace 4 días):
    ~ artifact              sha256:9b1c → sha256:a44f
                            (ya corrió en staging hace 2 días ✓)
    ~ parameters.DB_POOL_SIZE   cambió
    + steps: `migrate`      (nuevo, irreversible)
    = pipeline, config, target   sin cambios

  Herramientas:
    ✓ terraform 1.9.5
    ✓ kubectl 1.30.2

  Secretos requeridos:
    ✓ REGISTRY_TOKEN
    ✗ DB_PASSWORD           falta en este entorno

  ⚠ No se puede ejecutar: falta DB_PASSWORD
```

Resuelve en una pantalla: qué cambió, si ya se probó en otro entorno, si
hay algo irreversible, y si falta algo para poder correr — sin llegar a
ejecutar nada.

---

## 2. Al fallar — salida de `vex run`

Seis líneas útiles en vez de miles de líneas de log.

```
✗ api-pagos → prod-lima   falló en `deploy` (4 de 5) — 12s

  registry_unavailable

  Error: connection timeout to ghcr.io

  Contexto:
  · Este mismo contenido (8f21ac) ya tuvo éxito hace 3 días.
    → probablemente no es tu configuración, es transitorio.
  · 2 fallos por esta misma causa en los últimos 7 días.

  Reintentar:
    vex run --from deployment/8f21ac
```

La línea más valiosa: cuando el mismo `content_id` ya tuvo éxito antes,
el fallo probablemente no es del código — es transitorio. Le ahorra al
desarrollador media hora de culpar a su propio cambio.

---

## 3. Al tener éxito — salida de `vex run`

Un check verde sin contexto esconde degradaciones silenciosas.

```
✓ api-pagos → prod-lima   (3m42s)

  deployment 8f21ac
  artifact sha256:a44f — mismo que staging (hace 2 días) y dev (hace 6 días)

  vs. despliegues anteriores a prod:
    · duración normal (promedio 3m50s ±40s)
    · sin parámetros nuevos, sin pasos nuevos

  Historial de prod-lima: 47 despliegues, 94% éxito
```

Tres cosas que un simple ✓ nunca dice: confirma la cadena de promoción
(lo que corrió en prod es lo mismo que se probó antes), detecta anomalía
dentro del propio éxito (duración fuera de lo normal sería visible aquí,
como primera señal de degradación), y da marco histórico sin que nadie
lo pida.

---

## 4. Explorando el historial — `vex log` / `vex show`

Para cuando ya no hay una decisión inmediata, solo se quiere entender.

```
$ vex log --env prod

8f21ac  ✓  hace 2h   3m42s   deploy
7f8a20  ✓  hace 4d   3m51s   deploy
d4c1a9  ✗  hace 4d   0m12s   deploy   missing_credential
a3f21c  ✓  hace 9d   4m03s   rollback ← 6b1e9a
6b1e9a  ✗  hace 9d   8m30s   deploy   verification_failure
```

Una línea por nodo, igual que `git log --oneline`.

`vex show 8f21ac` da el detalle completo de ese nodo: el objeto de
despliegue, sus intentos (resultados), y el diff contra su padre.

---

## 5. Preguntar sobre patrones — `vex stats`

Cinco vistas curadas en vez de un lenguaje de consulta genérico —
suficiente para casi todo lo que alguien pregunta, sin el costo de
diseñar y mantener un DSL.

```
$ vex stats

  api-pagos — últimos 90 días

  Por entorno:
    dev     124 despliegues   89% éxito
    test     58 despliegues   72% éxito
    prod     47 despliegues   94% éxito

  Fallos más comunes:
    missing_credential      8   (todos en test)
    verification_failure    5
    build_failure           3

  Paso más lento: `test`  (1m40s promedio, 34% del tiempo total)

  Parámetros nunca usados: SKIP_CACHE, LEGACY_MODE
```

Con drill-down puntual cuando hace falta:

```
vex stats failures --step deploy
vex stats duration --step test
```

---

## Resumen de comandos

| Comando | Momento | Qué responde |
|---|---|---|
| `vex plan` | antes | ¿puedo y debo desplegar ahora? |
| `vex run` (salida en fallo) | durante | ¿es mi código o el entorno? |
| `vex run` (salida en éxito) | durante | ¿de verdad salió bien? |
| `vex log` | explorando | ¿qué pasó, en orden? |
| `vex show <id>` | explorando | ¿qué contiene este despliegue exacto? |
| `vex stats` | explorando | ¿dónde está el problema recurrente? |
