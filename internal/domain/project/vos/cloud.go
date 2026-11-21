package vos

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	CredentialsPortal = "portal"
	CredentialsEnv    = "env"
)

// NeutralReservedEnvPrefixes son los prefijos reservados comunes a todas las nubes; los de cada nube los
// aporta su adaptador.
var NeutralReservedEnvPrefixes = []string{"VEX_CLOUD_", "VEX_BOOTSTRAP_"}

var cloudProviderPattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)

// Cloud es el bloque cloud de vexconfig.yaml: de dónde salen las credenciales de nube y, opcionalmente, en qué
// nube espera desplegar el proyecto. El valor cero significa que el bloque no existe.
type Cloud struct {
	provider    string
	credentials string
}

// NewCloud valida el bloque. No comprueba que la nube exista: eso lo decide el contrato del pipeline.
func NewCloud(provider, credentials string) (Cloud, error) {
	switch credentials {
	case CredentialsPortal, CredentialsEnv:
	case "":
		return Cloud{}, errors.New("cloud.credentials es obligatorio cuando existe el bloque cloud")
	default:
		return Cloud{}, fmt.Errorf("cloud.credentials %q no existe: usa %q o %q", credentials, CredentialsPortal, CredentialsEnv)
	}
	if provider != "" && !cloudProviderPattern.MatchString(provider) {
		return Cloud{}, fmt.Errorf("cloud.provider %q no es un nombre de nube válido (minúsculas, p. ej. \"azure\")", provider)
	}
	return Cloud{provider: provider, credentials: credentials}, nil
}

// CloudFromContract traduce el contrato que declara una plantilla a la configuración de nube del proyecto.
// supported lo aporta quien conoce los contratos que esta versión de vex sabe usar: la comparación es exacta.
func CloudFromContract(contract string, supported func(string) bool) (Cloud, error) {
	switch {
	case contract == "":
		return Cloud{}, nil
	case supported(contract):
		provider, _, _ := strings.Cut(contract, "/")
		return NewCloud(provider, CredentialsPortal)
	default:
		return Cloud{}, fmt.Errorf("esta plantilla requiere el contrato de nube %q, que esta versión de vex no conoce: actualiza vex", contract)
	}
}

// ReconcileCloud decide la configuración de nube tras elegir (o cambiar) de plantilla, respetando lo explícito:
// una plantilla con contrato escribe el bloque si no lo hay; una sin contrato quita el que apuntaba al portal.
func ReconcileCloud(actual Cloud, contract string, supported func(string) bool) (Cloud, error) {
	fromContract, err := CloudFromContract(contract, supported)
	if err != nil {
		return Cloud{}, err
	}
	switch {
	case fromContract.IsZero() && actual.IsPortal():
		return Cloud{}, nil
	case fromContract.IsZero(), !actual.IsZero():
		return actual, nil
	default:
		return fromContract, nil
	}
}

func (c Cloud) IsZero() bool        { return c == Cloud{} }
func (c Cloud) Provider() string    { return c.provider }
func (c Cloud) Credentials() string { return c.credentials }
func (c Cloud) IsPortal() bool      { return c.credentials == CredentialsPortal }

// ConflictingEnvNames devuelve, en su orden, los nombres de envs que empiezan por alguno de los prefijos
// reservados. Solo hay conflicto cuando las credenciales salen del portal.
func (c Cloud) ConflictingEnvNames(envs []EnvVar, reservedPrefixes []string) []string {
	if !c.IsPortal() {
		return nil
	}
	var conflicts []string
	for _, env := range envs {
		if hasAnyPrefix(env.Name(), reservedPrefixes) {
			conflicts = append(conflicts, env.Name())
		}
	}
	return conflicts
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
