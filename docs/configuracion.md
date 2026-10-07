# Configuración

## `vexconfig.yaml`

`vex init` genera este archivo en la raíz del proyecto.

```yaml
project:
  id: 9d8d4777-5707-43b3-a636-d6f8433b7a34   # lo genera vex init
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
```

- **`runtime.image`**: si contiene `:` es una imagen del registro y se usa tal cual; si no, es la ruta de un Dockerfile y `vex` lo construye (`docker build`) con `build.args` y el uid/gid del usuario. La imagen tiene que incluir `vexd` y arrancarlo como `ENTRYPOINT ["vexd"]`.
- **`runtime.run.envs`**: el valor `"$NOMBRE"` se expande con el entorno del usuario, así que los secretos nunca se escriben en el archivo. Esas variables viajan al motor por la entrada estándar (no por argumentos ni por `-e`), de modo que no aparecen en `docker inspect`. Si una variable no está definida, `vex` avisa y no la envía.
- **`runtime.run.volumes`**: se añaden a los montajes que `vex` hace por su cuenta.

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

La caché del usuario es `~/Library/Caches` en macOS y `~/.cache` en Linux.

## Variables de entorno

| Variable | Descripción |
| :--- | :--- |
| `VEX_STORE_TEMPLATE` | URL del catálogo de plantillas de arquitectura que usan `vex init` y `vex arq`. Por defecto, el de `vex-template-store`. |
| _las declaradas por el usuario_ | Las que se referencian con `$NOMBRE` en `runtime.run.envs`. |
