package portalauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "vex"
	keyringUser    = "vexPortalToken"

	// tokenStoreEnv = "file" salta el llavero por completo (salida de emergencia).
	tokenStoreEnv = "VEX_TOKEN_STORE"

	backendKeyring = "llavero del sistema"
	backendFile    = "archivo (sin llavero)"
)

var errKeyringUnavailable = errors.New("portalauth: keyring unavailable")

// KeyringTokenStore guarda el token en el llavero del sistema (Keychain,
// Credential Manager o Secret Service).
type KeyringTokenStore struct{}

func (KeyringTokenStore) Save(t Token) error {
	b, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	if err := keyring.Set(keyringService, keyringUser, string(b)); err != nil {
		return fmt.Errorf("%w: %v", errKeyringUnavailable, err)
	}
	return nil
}

func (KeyringTokenStore) Load() (Token, error) {
	s, err := keyring.Get(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return Token{}, ErrTokenNotFound
	}
	if err != nil {
		return Token{}, fmt.Errorf("%w: %v", errKeyringUnavailable, err)
	}
	var t Token
	if err := json.Unmarshal([]byte(s), &t); err != nil {
		return Token{}, fmt.Errorf("decode credentials: %w", err)
	}
	return t, nil
}

func (KeyringTokenStore) Delete() error {
	if err := keyring.Delete(keyringService, keyringUser); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("%w: %v", errKeyringUnavailable, err)
	}
	return nil
}

// DefaultTokenStore usa el llavero y cae al archivo 0600 cuando no hay
// llavero; migra de forma transparente un credentials.json previo.
type DefaultTokenStore struct {
	keyring TokenStore // nil = solo archivo (VEX_TOKEN_STORE=file)
	file    *FileTokenStore
	warn    io.Writer
	warnOne sync.Once

	mu      sync.Mutex
	backend string
}

// NewDefaultTokenStore construye el almacén de producción.
func NewDefaultTokenStore() (*DefaultTokenStore, error) {
	file, err := NewFileTokenStore()
	if err != nil {
		return nil, err
	}
	if os.Getenv(tokenStoreEnv) == "file" {
		return newDefaultTokenStore(nil, file, os.Stderr), nil
	}
	return newDefaultTokenStore(KeyringTokenStore{}, file, os.Stderr), nil
}

func newDefaultTokenStore(kr TokenStore, file *FileTokenStore, warn io.Writer) *DefaultTokenStore {
	s := &DefaultTokenStore{keyring: kr, file: file, warn: warn}
	if kr == nil {
		s.backend = backendFile
	} else {
		s.backend = backendKeyring
	}
	return s
}

// Backend indica dónde quedó el token tras la última operación.
func (s *DefaultTokenStore) Backend() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backend
}

func (s *DefaultTokenStore) setBackend(b string) {
	s.mu.Lock()
	s.backend = b
	s.mu.Unlock()
}

func (s *DefaultTokenStore) warnFallback() {
	s.warnOne.Do(func() {
		fmt.Fprintf(s.warn, "aviso: no hay llavero del sistema disponible; el token se guarda en %s (permisos 0600)\n", s.file.Path())
	})
}

func (s *DefaultTokenStore) Load() (Token, error) {
	if s.keyring == nil {
		return s.file.Load()
	}
	token, err := s.keyring.Load()
	switch {
	case err == nil:
		s.setBackend(backendKeyring)
		return token, nil
	case errors.Is(err, ErrTokenNotFound):
		return s.migrateFromFile()
	case errors.Is(err, errKeyringUnavailable):
		return s.loadFromFileWithoutKeyring()
	default:
		return Token{}, err
	}
}

// migrateFromFile copia credentials.json al llavero y solo entonces lo borra.
func (s *DefaultTokenStore) migrateFromFile() (Token, error) {
	token, err := s.file.Load()
	if err != nil {
		return Token{}, err
	}
	if err := s.keyring.Save(token); err != nil {
		s.setBackend(backendFile)
		s.warnFallback()
		return token, nil
	}
	s.setBackend(backendKeyring)
	if err := s.file.Delete(); err != nil {
		return Token{}, fmt.Errorf("remove migrated credentials file: %w", err)
	}
	return token, nil
}

func (s *DefaultTokenStore) loadFromFileWithoutKeyring() (Token, error) {
	token, err := s.file.Load()
	if err != nil {
		return Token{}, err
	}
	s.setBackend(backendFile)
	s.warnFallback()
	return token, nil
}

func (s *DefaultTokenStore) Save(t Token) error {
	if s.keyring != nil {
		err := s.keyring.Save(t)
		if err == nil {
			s.setBackend(backendKeyring)
			return s.file.Delete()
		}
		if !errors.Is(err, errKeyringUnavailable) {
			return err
		}
		s.warnFallback()
	}
	if err := s.file.Save(t); err != nil {
		return err
	}
	s.setBackend(backendFile)
	return nil
}

func (s *DefaultTokenStore) Delete() error {
	var errs []error
	if s.keyring != nil {
		errs = append(errs, s.keyring.Delete())
	}
	errs = append(errs, s.file.Delete())
	return errors.Join(errs...)
}

var _ TokenStore = (*DefaultTokenStore)(nil)
