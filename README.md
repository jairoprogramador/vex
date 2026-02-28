<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo-light.png">
    <img alt="vex" src="assets/logo-dark.png" width="200">
  </picture>
  <p><strong>De código a producción en dos comandos.</strong></p>

  <p>
    <a href="https://github.com/jairoprogramador/vex-client/releases">
      <img src="https://img.shields.io/github/v/release/jairoprogramador/vex-client?style=for-the-badge" alt="Latest Release">
    </a>
    <a href="https://github.com/jairoprogramador/vex-client/blob/main/LICENSE">
      <img src="https://img.shields.io/github/license/jairoprogramador/vex-client?style=for-the-badge" alt="License">
    </a>
  </p>
</div>

---

**`vex`** es una CLI open source para equipos que quieren desplegar en producción sin necesitar un equipo de DevOps ni arquitectos de nube. Sin Terraform. Sin Dockerfiles. Sin configuraciones interminables. Solo dos comandos:

```sh
vex init
vex deploy prod
```

Eso es todo. `vex` selecciona automáticamente una arquitectura cloud probada, empaqueta tu aplicación y la despliega. Lo que normalmente toma días de configuración manual, con `vex` toma 5 minutos.

## ¿Para quién es?

Para equipos pequeños de desarrollo que:

- Necesitan llevar un microservicio a producción **rápido**.
- No tienen un equipo de DevOps dedicado.
- No quieren perder días configurando infraestructura cloud.
- Prefieren enfocarse en escribir código.

## ¿Cómo funciona?

`vex` es la interfaz con la que interactúas. Por debajo, se apoya en [vex-core](https://github.com/jairoprogramador/vex) (el motor de ejecución) y [template store](https://github.com/jairoprogramador/vex-template-store) (plantillas de despliegue probadas para distintos escenarios). Tú solo necesitas conocer dos comandos.

### Flujo en 3 pasos

**1. Inicializa tu proyecto**

Navega al directorio de tu proyecto y ejecuta:

```sh
vex init
```

Responde unas preguntas breves (nombre, equipo, organización) y `vex` genera un archivo `vexconfig.yaml` con la configuración lista para desplegar. Se asigna automáticamente una arquitectura cloud básica.

**2. Ajusta la arquitectura (Opcional)**

Si necesitas una arquitectura más robusta, ejecuta:

```sh
vex arq
```

`vex` te hace 3 preguntas sobre escalabilidad, recuperación y presupuesto, y selecciona la arquitectura cloud que mejor se adapta a tu caso.

**3. Despliega**

```sh
vex deploy sand    # sandbox
vex deploy stag    # staging
vex deploy prod    # producción
```

Listo. Tu microservicio está en producción.

## Instalación

### macOS (Homebrew)

```sh
brew install --cask jairoprogramador/vex-client/vex
```

> Si macOS indica que no puede verificar el desarrollador, permite la ejecución en **Ajustes del sistema > Privacidad y seguridad > Abrir de todos modos**, o ejecuta: `xattr -cr $(which vex)`.

### Linux

Descarga el paquete desde [Releases](https://github.com/jairoprogramador/vex-client/releases):

```sh
# Debian / Ubuntu
sudo dpkg -i vex-client_*.deb

# Red Hat / Fedora
sudo rpm -i vex-client_*.rpm
```

O directamente el binario:

```sh
curl -sL https://github.com/jairoprogramador/vex-client/releases/latest/download/vex-client_linux_amd64.tar.gz | tar xz
sudo mv vex /usr/local/bin/
```

### Windows

1. Descarga `vex-client_*_windows_amd64.zip` desde [Releases](https://github.com/jairoprogramador/vex-client/releases).
2. Descomprime el archivo.
3. Añade `vex.exe` a tu variable de entorno `PATH`.

## Referencia de comandos

| Comando | Descripción |
| :--- | :--- |
| `vex init` | Inicializa el proyecto y genera `vexconfig.yaml`. |
| `vex arq` | Ajusta la arquitectura cloud según tus necesidades (opcional). |
| `vex deploy [env]` | Despliega en el entorno indicado (`sand`, `stag`, `prod`). |
| `vex version` | Muestra la versión instalada. |

> `vex init` acepta el flag `--yes` / `-y` para usar valores por defecto sin preguntas interactivas.

## Contribuciones

Las contribuciones son bienvenidas. Si tienes ideas, encuentras un error o quieres mejorar algo, abre un [issue](https://github.com/jairoprogramador/vex-client/issues) o enviá un [pull request](https://github.com/jairoprogramador/vex-client/pulls).

## Licencia

`vex` está distribuido bajo la [Apache License 2.0](https://github.com/jairoprogramador/vex-client/blob/main/LICENSE).
