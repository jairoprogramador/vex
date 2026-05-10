package portalauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// TokenStore is the small interface every credential backend must satisfy.
// The CLI talks only to this contract, which keeps the auth subcommand
// decoupled from the on-disk format.
type TokenStore interface {
	Save(token Token) error
	Load() (Token, error)
	Delete() error
}

// FileTokenStore persists the token as JSON on the local filesystem,
// mode 0600, under CredentialsPath() (or an explicit path for tests).
type FileTokenStore struct {
	path string
}

// NewFileTokenStore returns a store rooted at the platform-default
// credentials path.
func NewFileTokenStore() (*FileTokenStore, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, fmt.Errorf("token store: %w", err)
	}
	return &FileTokenStore{path: path}, nil
}

// NewFileTokenStoreAt returns a store backed by an arbitrary path. Useful
// for tests and for callers that want to override XDG resolution.
func NewFileTokenStoreAt(path string) *FileTokenStore {
	return &FileTokenStore{path: path}
}

// Path returns the absolute filesystem path the store operates on.
func (s *FileTokenStore) Path() string {
	return s.path
}

// Save serializes token to JSON and writes it atomically (write-temp + rename),
// ensuring the parent directory exists with mode 0700 and the final file is
// chmod'd to 0600 on POSIX systems.
func (s *FileTokenStore) Save(token Token) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	tmp := s.path + ".tmp"
	// Best-effort cleanup of a stale temp file from a previous crashed run.
	_ = os.Remove(tmp)

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp credentials: %w", err)
	}

	// os.WriteFile only honors the mode when creating a new file; on POSIX
	// we explicitly chmod to defend against pre-existing files with looser
	// permissions. On Windows the call is a no-op (read-only bit only).
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o600); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("chmod temp credentials: %w", err)
		}
	}

	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename credentials: %w", err)
	}

	if runtime.GOOS != "windows" {
		// Final chmod after rename in case the destination already existed
		// with a different mode.
		if err := os.Chmod(s.path, 0o600); err != nil {
			return fmt.Errorf("chmod credentials: %w", err)
		}
	}
	return nil
}

// Load reads the persisted token. ErrTokenNotFound is returned when the
// credentials file does not exist; any other I/O or decoding error is
// wrapped and returned to the caller.
func (s *FileTokenStore) Load() (Token, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Token{}, ErrTokenNotFound
		}
		return Token{}, fmt.Errorf("read credentials: %w", err)
	}
	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return Token{}, fmt.Errorf("decode credentials: %w", err)
	}
	return token, nil
}

// Delete removes the credentials file. The operation is idempotent: a
// missing file is not an error.
func (s *FileTokenStore) Delete() error {
	if err := os.Remove(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("delete credentials: %w", err)
	}
	return nil
}

// Compile-time contract check: FileTokenStore must satisfy TokenStore.
var _ TokenStore = (*FileTokenStore)(nil)
