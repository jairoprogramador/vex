package vos

import (
	"strings"
	"testing"
)

func TestNewImage(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name, spec                         string
		image, tag, digestWant, spec2, ref string
		registry                           bool
	}{
		{"solo digest", "a/b@" + digest, "a/b", "", digest, "a/b@" + digest, "a/b@" + digest, true},
		{"tag y digest", "a/b:1.2.3@" + digest, "a/b", "1.2.3", digest, "a/b:1.2.3@" + digest, "a/b@" + digest, true},
		{"registro con puerto y digest", "reg:5000/a/b@" + digest, "reg:5000/a/b", "", digest, "reg:5000/a/b@" + digest, "reg:5000/a/b@" + digest, true},
		{"solo tag", "a/b:1.2.3", "a/b", "1.2.3", "", "a/b:1.2.3", "a/b:1.2.3", true},
		{"dockerfile", "Dockerfile", "Dockerfile", "latest", "", "Dockerfile", "Dockerfile:latest", false},
		{"vacío", "", "Dockerfile", "latest", "", "Dockerfile", "Dockerfile:latest", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewImage(tt.spec)
			if err != nil {
				t.Fatalf("NewImage(%q): %v", tt.spec, err)
			}
			if got.Image() != tt.image || got.Tag() != tt.tag || got.Digest() != tt.digestWant {
				t.Errorf("partes = %q %q %q", got.Image(), got.Tag(), got.Digest())
			}
			if got.Spec() != tt.spec2 || got.Reference() != tt.ref || got.TagExplicit() != tt.registry {
				t.Errorf("Spec=%q Reference=%q TagExplicit=%v", got.Spec(), got.Reference(), got.TagExplicit())
			}
			if got.Pinned() != (tt.digestWant != "") {
				t.Errorf("Pinned = %v", got.Pinned())
			}
		})
	}
}

func TestNewImageRechazaDigestMalEscrito(t *testing.T) {
	for _, spec := range []string{
		"a/b:1.2.3@sha256:" + strings.Repeat("a", 63),
		"a/b@sha256:" + strings.Repeat("A", 64),
		"a/b@sha512:" + strings.Repeat("a", 64),
		"a/b@",
	} {
		if _, err := NewImage(spec); err == nil {
			t.Errorf("NewImage(%q) debería fallar", spec)
		}
	}
}
