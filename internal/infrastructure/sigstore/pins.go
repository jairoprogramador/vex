package sigstore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
)

// Los nombres de GitHub se pueden volver a registrar: si la cuenta o un repositorio se renombran o se borran,
// otro puede quedarse el nombre y firmar con una identidad que cumple las expresiones de policy.go. Los ids
// numéricos no se reutilizan, y Fulcio los escribe en el certificado. Se fijan al enlazar el binario:
//
//	-X github.com/jairoprogramador/vex/internal/infrastructure/sigstore.pinnedOwnerID=<id de la cuenta>
//	-X github.com/jairoprogramador/vex/internal/infrastructure/sigstore.pinnedCatalogRepositoryID=<id de vex-template-store>
//	-X github.com/jairoprogramador/vex/internal/infrastructure/sigstore.pinnedRuntimeRepositoryIDs=vex-runtime-a=<id>,vex-runtime-b=<id>
//
// Un release sin pinnedOwnerID o sin pinnedCatalogRepositoryID no verifica nada (falla cerrado). Los repositorios
// de runtime son varios y aparecen nuevos sin publicar otra CLI: el que está en la lista debe tener ese id; el que
// no, queda cubierto por el id de la cuenta, que es quien puede crear repositorios con ese nombre.
var (
	pinnedOwnerID              = ""
	pinnedCatalogRepositoryID  = ""
	pinnedRuntimeRepositoryIDs = ""
)

const catalogRepository = "vex-template-store"

// errPinsMissing: el binario es un release y no lleva los ids que debe exigir.
var errPinsMissing = errors.New("este binario se publicó sin los ids de la cuenta y del repositorio que firman " +
	"(pinnedOwnerID, pinnedCatalogRepositoryID): no puede comprobar quién firmó y no verifica nada. Es un fallo de quien lo construyó")

var (
	numericID       = regexp.MustCompile(`^[0-9]{1,20}$`)
	signerRepoShape = regexp.MustCompile(`^https://github\.com/[^/]+/([^/]+)/`)
)

// identityPins son los ids que debe llevar el certificado de quien firma.
type identityPins struct {
	owner string
	// repositories: nombre del repositorio → id.
	repositories map[string]string
}

// loadPins lee los ids enlazados en el binario. Con required (un release) faltar es un error; sin él (un build
// de desarrollo) se verifica solo por nombre.
func loadPins(required bool) (identityPins, error) {
	return parsePins(required, pinnedOwnerID, pinnedCatalogRepositoryID, pinnedRuntimeRepositoryIDs)
}

func parsePins(required bool, owner, catalog, runtimes string) (identityPins, error) {
	if owner == "" || catalog == "" {
		if required || owner != "" || catalog != "" || runtimes != "" {
			return identityPins{}, errPinsMissing
		}
		return identityPins{}, nil
	}
	pins := identityPins{owner: owner, repositories: map[string]string{catalogRepository: catalog}}
	if runtimes != "" {
		for _, pair := range strings.Split(runtimes, ",") {
			name, id, ok := strings.Cut(pair, "=")
			if !ok || !runtimeName.MatchString(name) {
				return identityPins{}, fmt.Errorf("pinnedRuntimeRepositoryIDs no se entiende cerca de %q: se espera vex-runtime-x=<id>,…", pair)
			}
			pins.repositories[name] = id
		}
	}
	for name, id := range pins.repositories {
		if !numericID.MatchString(id) {
			return identityPins{}, fmt.Errorf("el id fijado para %s no es un número", name)
		}
	}
	if !numericID.MatchString(owner) {
		return identityPins{}, errors.New("pinnedOwnerID no es un número")
	}
	return pins, nil
}

// check dice si el certificado es de la cuenta fijada y, cuando su repositorio tiene id fijado, de ese repositorio.
func (p identityPins) check(summary certificate.Summary) bool {
	if p.owner == "" {
		return true
	}
	if summary.SourceRepositoryOwnerIdentifier != p.owner {
		return false
	}
	id, pinned := p.repositories[signerRepository(summary.SubjectAlternativeName)]
	return !pinned || summary.SourceRepositoryIdentifier == id
}

// signerRepository es el nombre del repositorio del workflow que firmó.
func signerRepository(signer string) string {
	if match := signerRepoShape.FindStringSubmatch(signer); match != nil {
		return match[1]
	}
	return ""
}
