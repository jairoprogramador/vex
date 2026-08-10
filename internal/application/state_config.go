package application

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// Los literales del contenedor. Hasta la spec 16 eran constantes del MOTOR,
// porque `--mode local` las codificaba: el enum decidía de una vez dónde vivía
// el estado, si el proyecto se clonaba o ya estaba en disco, y dónde caían los
// clones. Al retirarlo, las tres decisiones quedaron en manos de quien invoca —y
// quien invoca es esta CLI, porque es quien monta los volúmenes—, así que ahora
// son literales de aquí (spec 23 §5.2).
const (
	// containerProjectPath es donde se monta el CWD del usuario. Se le pasa al
	// motor con `--project-path`: sin esa bandera el motor CLONA el proyecto
	// desde su remoto y se desplegaría el HEAD publicado en vez del árbol de
	// trabajo que el usuario tiene delante. Silencioso, y es justo lo que el
	// modo local existe para no hacer.
	containerProjectPath = "/appProject"

	// Las DOS raíces del motor, que no son la misma cosa y aquí tampoco son el
	// mismo volumen. El motor las separó a propósito (spec 16) y esta CLI le da
	// a cada una un directorio distinto del host:
	//
	//	area de trabajo  →  <cache del usuario>/vex   clones y workdirs, derivables
	//	destino          →  ~/.vex                    estado y registro, la verdad
	//
	// Colapsarlas en un solo volumen funcionaría —el motor lo tolera— pero
	// juntaría en una carpeta lo desechable y lo permanente, que es justo la
	// distinción que la 16 se molestó en hacer.

	// containerVexRootPath es lo que viaja en `--vex-home`, y es una RAÍZ, no un
	// directorio de trabajo: el motor cuelga de ella el `.vex` donde viven los
	// clones y los workdirs (`<raíz>/.vex/projects`, `<raíz>/.vex/pipelines`).
	// Por eso el volumen se monta en `<raíz>/.vex` y no en la raíz: montarlo en
	// la raíz metería un `.vex` dentro del directorio del host.
	containerVexRootPath = "/vexHome"

	// containerVexWorkPath es donde se monta el área de trabajo del host. Es
	// `<raíz>/.vex` porque es el nombre que el motor va a componer de todos
	// modos.
	containerVexWorkPath = containerVexRootPath + "/.vex"

	// containerStatePath es donde se monta el `~/.vex` del host, y es el destino
	// que viaja en `VEX_STATE_CONFIG`. El motor escribe ahí `state/`, `cache/`,
	// `lineage/` y `keys/`, y empuja ahí `objects/` y `events/`.
	//
	// Que el montaje sea el `~/.vex` del host es lo que hace que no haya
	// migración: son exactamente las rutas que dejaba el viejo symlink de
	// `linkVexHome`, así que los registros escritos antes de la spec 16 se
	// siguen leyendo y los steps al día siguen reviviendo.
	containerStatePath = "/vexState"

	// containerHomeDir es el $HOME del usuario `vex` (uid 1001) DENTRO del
	// contenedor —el que crea el `useradd --create-home` del runtime—. Es contra
	// el que el motor resuelve su área de trabajo cuando no hay XDG_STATE_HOME,
	// y por eso esta CLI necesita conocerlo para la comprobación de §5.4.
	containerHomeDir = "/home/vex"

	// xdgStateHomeEnv es la primera opción de la cadena con la que el motor
	// resuelve el área de trabajo. El usuario puede declararla en los `envs` de
	// vexconfig.yaml, así que no es una constante: es una entrada.
	xdgStateHomeEnv = "XDG_STATE_HOME"

	stagingVendorDir = "vex"
	stagingLeafDir   = "staging"

	// stateConfigTypeLocal es el único `type` del vocabulario que esta CLI
	// emite. `http` existe y está congelado (spec 26).
	stateConfigTypeLocal = "local"
)

// stateConfigJSON es la forma externa de la configuración de destino que `vexd`
// deserializa. El motor la lee con YAML, que acepta JSON tal cual: el contrato
// está escrito en YAML porque lo lee gente, y se emite en JSON porque lo escribe
// código.
//
// Se define aquí y no se importa del motor por la regla de oro: `vex` no importa
// nada de `vex-engine`. La forma se mantiene en sync a mano, igual que la del
// RequestInput.
type stateConfigJSON struct {
	Type  string               `json:"type"`
	Local stateConfigLocalJSON `json:"local"`
}

type stateConfigLocalJSON struct {
	Path string `json:"path"`
}

// encodeStateConfig compone la configuración de destino a partir del montaje que
// la CLI acaba de resolver y la codifica en base64.
//
// Se genera al vuelo en cada invocación y no vive en un archivo (BL-22): la CLI
// resuelve el volumen en tiempo de ejecución, así que un archivo persistente con
// otra ruta se desincronizaría del montaje real y produciría un fallo confuso.
// El dato que decide el montaje y el que compone la configuración son el mismo,
// que es lo único que impide que diverjan.
//
// El base64 es por la misma razón que en el RequestInput: el valor viaja como
// `-e VEX_STATE_CONFIG=<valor>` dentro de una línea de shell, y las comillas y
// los saltos de línea del JSON la romperían.
func encodeStateConfig(containerPath string) (string, error) {
	if strings.TrimSpace(containerPath) == "" {
		return "", errors.New("state config: la ruta del destino es obligatoria")
	}

	raw, err := json.Marshal(stateConfigJSON{
		Type:  stateConfigTypeLocal,
		Local: stateConfigLocalJSON{Path: containerPath},
	})
	if err != nil {
		return "", fmt.Errorf("marshal state config: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// containerStagingDir es la ruta que el motor resolverá para su área de trabajo
// DENTRO del contenedor: `$XDG_STATE_HOME/vex/staging` si alguien declaró la
// variable en los `envs` del proyecto, y si no `$HOME/.local/state/vex/staging`
// con el $HOME del usuario del runtime.
//
// No se reproduce el último eslabón de la cadena del motor (`os.TempDir()`)
// porque sólo se llega a él cuando los anteriores no son escribibles, y eso no
// se puede saber desde fuera del contenedor. Basta con el que se va a usar.
func containerStagingDir(env []proVos.EnvVar) string {
	root := path.Join(containerHomeDir, ".local", "state")
	for _, v := range env {
		if v.Name() == xdgStateHomeEnv {
			if declared := strings.TrimSpace(v.Value()); declared != "" {
				root = declared
			}
		}
	}
	return path.Join(root, stagingVendorDir, stagingLeafDir)
}

// ensureStagingOutsideMounts aborta si el área de trabajo del motor cayera dentro
// de alguno de los volúmenes que la CLI monta (spec 23 §5.4).
//
// Es defensa en profundidad sobre un invariante que vive repartido entre dos
// repositorios, que es exactamente donde los invariantes se rompen. El motor se
// defiende por su lado, pero en SILENCIO: descarta el candidato que caiga bajo su
// volumen, reubica el área y sigue. Quien declaró un XDG_STATE_HOME dentro de un
// montaje quiso otra cosa y no se iba a enterar de que no ocurrió.
//
// Y hay una razón por la que aquí se miran DOS montajes y no uno. La guarda del
// motor compara el staging contra `<--vex-home>/.vex`, o sea contra su ÁREA DE
// TRABAJO; el peligro que §5.4 describe es contra el DESTINO. Mientras los dos
// fueron el mismo volumen la distinción no se veía —y el código del motor la
// escribe como si lo fueran—, pero al separarlos deja de haber nadie más mirando
// el destino. Esta comprobación es, para ese caso, la única.
//
// Lo que estaría en juego no es un directorio vacío: bajo el destino viven
// `objects/`, `events/` y `ack/` —el objeto de despliegue, los hechos de cada
// intento y la última posición confirmada—, y los dos primeros son permanentes.
// Además, un `staging/events` que FUERA `<destino>/events` haría que el `gc` de
// `vexd record` se negara a trabajar, por la guarda que le prohíbe barrer la fila
// «nunca se borra».
func ensureStagingOutsideMounts(env []proVos.EnvVar, mounts ...string) error {
	staging := containerStagingDir(env)
	for _, mount := range mounts {
		if !isUnder(staging, mount) {
			continue
		}
		return fmt.Errorf(
			"el área de trabajo del motor (%s) caería dentro del volumen %s: ahí viven"+
				" objects/, events/ y ack/, y los dos primeros son permanentes."+
				" Saca %s de los 'envs' de vexconfig.yaml o apúntalo fuera de %s",
			staging, mount, xdgStateHomeEnv, mount)
	}
	return nil
}

// isUnder responde si `p` es `root` o cuelga de él. Trabaja con `path` y no con
// `filepath` a propósito: las rutas que compara son las del CONTENEDOR, que es
// Linux, aunque esta CLI corra en Windows.
func isUnder(p, root string) bool {
	p = path.Clean(p)
	root = path.Clean(root)
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+"/")
}
