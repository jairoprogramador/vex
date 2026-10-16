package vos

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type ProjectID struct {
	value string
}

func NewProjectID(id string) (ProjectID, error) {
	if id == "" {
		return ProjectID{}, errors.New("id is required")
	}
	return ProjectID{value: id}, nil
}

// ParseProjectUUID valida que id sea un UUID v4. Se usa con los IDs que
// asigna el portal; los IDs ya existentes en vexconfig.yaml pasan por NewProjectID.
func ParseProjectUUID(id string) (ProjectID, error) {
	if !uuidV4Pattern.MatchString(id) {
		return ProjectID{}, fmt.Errorf("%q no es un UUID v4", id)
	}
	return ProjectID{value: id}, nil
}

// NewRandomProjectID devuelve un UUID v4 de crypto/rand. Es provisional: si el
// portal asigna otro, vexconfig.yaml lo reemplaza.
func NewRandomProjectID() (ProjectID, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ProjectID{}, fmt.Errorf("generar id de proyecto: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return ProjectID{value: fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])}, nil
}

func (p ProjectID) String() string {
	return p.value
}

func (p ProjectID) Equals(other ProjectID) bool {
	return p.value == other.value
}
