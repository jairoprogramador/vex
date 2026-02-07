<div align="center">
  <!-- <img src="doc/img/Vex.jpg" alt="Vex Logo" width="150"/> -->
  <h1>Vex</h1>
  <p><strong>Despliega cualquier tecnología en cualquier plataforma con simples comandos.</strong></p>
  <p><i>La infraestructura se convierte en una plantilla.</i></p>

  <p>
    <a href="https://github.com/jairoprogramador/vex/releases">
      <img src="https://img.shields.io/github/v/release/jairoprogramador/vex?style=for-the-badge" alt="Latest Release">
    </a>
    <a href="https://github.com/jairoprogramador/vex/blob/main/LICENSE">
      <img src="https://img.shields.io/github/license/jairoprogramador/vex?style=for-the-badge" alt="License">
    </a>
  </p>
</div>

---

**`vex`** es una herramienta CLI diseñada para eliminar la complejidad de los procesos de despliegue. Olvídate de los scripts frágiles, los largos `READMEs` y la pregunta "¿cómo se desplegaba esto?". Con `vex`, estandarizas tus despliegues usando plantillas reutilizables, permitiendo que cualquier desarrollador, en cualquier equipo, pueda desplegar cualquier aplicación de forma segura y predecible.

**Define tu proceso de despliegue una vez, y ejecútalo miles de veces con simples comandos.**

## ✨ Características Principales

*   **⚙️ Agnostico a la Tecnología:** ¿Java, Node.js, Python, Go? ¿Terraform, Docker, Kubernetes? `vex` orquesta cualquier herramienta que puedas ejecutar en un shell.
*   **📄 Infraestructura como plantilla:** Centraliza la lógica de tus despliegues (steps, variables, entornos) en un repositorio de plantilla. Estandariza las buenas prácticas y evoluciona tu infraestructura sin tocar tus projectos.
*   **🚀 Despliega facil:** Clona o crea tu projecto y ejecuta: `vex init` y `vex deploy`, eso es todo. Recuerda se necesita instalar el [Cliente de Vex](https://github.com/jairoprogramador/vex-client)
*   **✅ Verificación continua:** El estado de cada despliegue se guarda, permitiendo validaciones y evitando ejecuciones accidentales en entornos incorrectos.
*   **💻 Mejor experiencia de desarrollador:** Comandos intuitivos, feedback claro y la abstracción perfecta para que los desarrolladores se centren en lo que importa: el código.

## 🚀 Instalación

Instala `vex` en segundos.

### macOS (Homebrew)

```sh
brew install --cask jairoprogramador/vex/vexc
```

Si macOS indica que no puede verificar el desarrollador, puedes permitir la ejecución en  
**Ajustes del sistema → Privacidad y seguridad → "Abrir de todos modos"**,  
o en Terminal: `xattr -cr $(which vexc)`.

### Linux

Puedes descargar el paquete `.deb` o `.rpm` desde la [página de Releases](https://github.com/jairoprogramador/vex/releases) y usar tu gestor de paquetes.

```sh
# Para sistemas basados en Debian/Ubuntu
sudo dpkg -i vex_*.deb

# Para sistemas basados en Red Hat/Fedora
sudo rpm -i vex_*.rpm
```

Alternativamente, puedes descargar el binario directamente:
```sh
curl -sL https://github.com/jairoprogramador/vex/releases/latest/download/vex_linux_amd64.tar.gz | tar xz

sudo mv fd /usr/local/bin/
```

### Windows

1.  Descarga el archivo `vex_windows_***64.zip` desde la [página de Releases](https://github.com/jairoprogramador/vex/releases).
2.  Descomprime el archivo.
3.  Añade el ejecutable `vex.exe` a tu variable de entorno `PATH`.


## 🏁 Guía de Inicio Rápido: Desplegando un microservicio Java

Vamos a desplegar un microservicio Java que utiliza **Terraform** para provisionar la infraestructura en **Azure** y se empaqueta con **Docker**.

Toda la lógica de este despliegue está definida en nuestra plantilla de ejemplo:
➡️ **[jairoprogramador/mydeploy](https://github.com/jairoprogramador/mydeploy)**

Este repositorio de plantillas contiene los `steps`, `variables` y la definición de los `environments` (ej: `sandbox`, `stagin`, `produccion`).

### Paso 1: Inicializa tu Proyecto

Clona o crear el proyecto de microservicio que quieres desplegar. Una vez dentro del directorio del proyecto, ejecuta:

*Nota: Debes tener instalado el [Cliente de Vex](https://github.com/jairoprogramador/vex-client)*

```sh
vex init
```

`vex` detectará que no está inicializado y te hará un par de preguntas para crear el archivo de configuración local `vexconfig.yaml`. Este archivo vincula tu proyecto con la plantilla de despliegue.

```yaml
# .vexconfig.yaml (Ejemplo generado)
project:
  id: 9238fa29be....
  name: "test"
  version: "1.0.0"
  team: "shikigami"
  description: "Mi proyecto de ejemplo"
  organization: "vex"

template:
  url: "https://github.com/jairoprogramador/mydeploy.git"
  ref: "main"
runtime:
    image: Dockerfile
    tag: latest
    build:
        args:
            - name: "VEX_VERSION"
              value: "1.0.10"
            - name: "MAVEN_VERSION"
              value: "3.9.12"
    run:
        volumes:
            - host: /home/user/.m2/
              container: /home/ubuntu/.m2
            - host: /home/user/myproject
              container: /home/vex/app
            - host: /home/user/.vex
              container: /home/ubuntu/.vex
        envs:
            - name: "ARM_CLIENT_ID"
              value: "$ARM_CLIENT_ID"
            - name: "ARM_CLIENT_SECRET"
              value: "$ARM_CLIENT_SECRET"
            - name: "ARM_TENANT_ID"
              value: "$ARM_TENANT_ID"
            - name: "ARM_SUBSCRIPTION_ID"
              value: "$ARM_SUBSCRIPTION_ID"
```

### Paso 2: Prueba el despliegue en un entorno

Antes de desplegar, puedes validar que todo está bien. El comando `vexc test [environment]` ejecuta los comandos definidos en la plantilla referentes a las pruebas.

```sh
# Ejecuta los pasos de prueba para el entorno 'sand'
vexc test sand
```

Esto podría, por ejemplo, compilar el proyecto, ejecutar los test unitarios, las pruebas de seguridad, validar versiones, verificar pull request, etc, sin desplegarlo.

### Paso 3: Despliega

Una vez que las pruebas pasan, estás listo para desplegar. El comando `vexc deploy [environment]` ejecuta la secuencia completa de pasos definidos en la plantilla, por ejemplo para el entorno de sandbox.

```sh
# Despliega en el entorno 'sand'
vexc deploy sand
```
`vex` orquestará todo el proceso:
1.  Clonará la plantilla de despliegue.
2.  Ejecutará los pasos para aprovisionar recursos.
3.  Empaquetará y subirá la imagen del proyecto.
4.  Desplegará la aplicación en el ambiente elegido.

¡Y listo! Tu microservicio está desplegado.

## 📚 Comandos Básicos

| Comando | Descripción |
| :--- | :--- |
| `vexc init` | Inicializa un proyecto creando el archivo `vexconfig.yaml`. |
| `vexc [step] [env]` | Ejecuta hasta el `step` indicado en el entorno `env`. |
| `vexc test [env]` | Ejecuta hasta el paso `test` en el entorno `env`. Verificamos la calidad del proyecto. |
| `vexc supply [env]` | Ejecuta hasta el paso `supply` en el entorno `env`. Aprovisionamos la infraestructura necesaria. |
| `vexc package [env]` | Ejecuta hasta el paso `package` en el entorno `env`. Empaquetamos el proyecto para su despliegue. |
| `vexc deploy [env]` | Ejecuta hasta el paso `deploy` en el entorno `env`. Es el ultimo paso, desplegamos el projecto en el entorno indicado. |

**Flags comunes:**
*   `--yes` o `-y`: Salta las confirmaciones interactivas, para `fdc init`


## 🤝 Contribuciones

¡Las contribuciones son bienvenidas! Si tienes ideas, sugerencias o encuentras un error, por favor abre un [issue](https://github.com/jairoprogramador/vex/issues) o envía un [pull request](https://github.com/jairoprogramador/vex/pulls).

## 📄 Licencia

`vex` está distribuido bajo la [Apache License 2.0](https://github.com/jairoprogramador/vex/blob/main/LICENSE).
