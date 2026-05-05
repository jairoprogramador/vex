package portalauth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFileTokenStore_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "vex", CredentialsFileName)
	store := NewFileTokenStoreAt(path)

	now := time.Now().UTC().Truncate(time.Second)
	want := Token{
		AccessToken: "tok-abc",
		TokenType:   "Bearer",
		ExpiresAt:   now.Add(90 * 24 * time.Hour),
		ObtainedAt:  now,
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.TokenType != want.TokenType {
		t.Fatalf("token mismatch: got %+v want %+v", got, want)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("ExpiresAt mismatch: got %v want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

func TestFileTokenStore_LoadMissingReturnsSentinel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileTokenStoreAt(filepath.Join(dir, "missing.json"))

	_, err := store.Load()
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestFileTokenStore_DeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	store := NewFileTokenStoreAt(path)

	// Delete on a missing file must not error.
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete on missing: %v", err)
	}

	if err := store.Save(Token{AccessToken: "x", TokenType: "Bearer"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
}

func TestFileTokenStore_PermissionsArePrivate(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics not applicable on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	// Pre-create the file with overly-permissive mode to make sure Save
	// tightens it back to 0600.
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	store := NewFileTokenStoreAt(path)
	if err := store.Save(Token{AccessToken: "tok", TokenType: "Bearer"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected mode 0600, got %v", perm)
	}
}

func TestFileTokenStore_CreatesParentDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	nested := filepath.Join(dir, "deep", "vex", CredentialsFileName)
	store := NewFileTokenStoreAt(nested)

	if err := store.Save(Token{AccessToken: "x", TokenType: "Bearer"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
