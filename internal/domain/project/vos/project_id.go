package vos

import (
	"crypto/sha256"
	"fmt"
	"errors"
)

type ProjectID struct {
	value string
}

func NewProjectID(id string) (ProjectID, error) {
	if id == "" {
		return ProjectID{}, errors.New("id is required")
	}
	return ProjectID{value: id}, nil
}

func GenerateProjectID(name, organization, team string) ProjectID {
	data := fmt.Sprintf("%s-%s-%s", name, organization, team)
	hash := sha256.Sum256([]byte(data))
	b := make([]byte, 16)
	copy(b, hash[:16])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return ProjectID{value: fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])}
}

func (p ProjectID) String() string {
	return p.value
}

func (p ProjectID) Equals(other ProjectID) bool {
	return p.value == other.value
}