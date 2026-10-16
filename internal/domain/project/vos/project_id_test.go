package vos

import (
	"regexp"
	"testing"
)

func TestNewRandomProjectID(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id, err := NewRandomProjectID()
		if err != nil {
			t.Fatalf("NewRandomProjectID: %v", err)
		}
		if !pattern.MatchString(id.String()) {
			t.Fatalf("formato inválido: %q", id.String())
		}
		if _, dup := seen[id.String()]; dup {
			t.Fatalf("id repetido: %q", id.String())
		}
		seen[id.String()] = struct{}{}
	}
}

func TestParseProjectUUID(t *testing.T) {
	t.Parallel()

	if _, err := ParseProjectUUID("9d8d4777-5707-43b3-a636-d6f8433b7a34"); err != nil {
		t.Fatalf("uuid válido rechazado: %v", err)
	}
	for _, bad := range []string{"", "seed-id", "9d8d4777-5707-13b3-a636-d6f8433b7a34"} {
		if _, err := ParseProjectUUID(bad); err == nil {
			t.Fatalf("%q debería rechazarse", bad)
		}
	}
}
