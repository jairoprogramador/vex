package portalauth

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

var testToken = Token{AccessToken: "tok-abc"}

func newTestStore(t *testing.T) (*DefaultTokenStore, *FileTokenStore, *bytes.Buffer) {
	t.Helper()
	file := NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	warn := &bytes.Buffer{}
	return newDefaultTokenStore(KeyringTokenStore{}, file, warn), file, warn
}

func fileExists(t *testing.T, f *FileTokenStore) bool {
	t.Helper()
	_, err := os.Stat(f.Path())
	return err == nil
}

func TestDefaultTokenStore_Load(t *testing.T) {
	t.Run("llavero tiene el token", func(t *testing.T) {
		keyring.MockInit()
		s, _, _ := newTestStore(t)
		if err := (KeyringTokenStore{}).Save(testToken); err != nil {
			t.Fatal(err)
		}
		got, err := s.Load()
		if err != nil || got.AccessToken != testToken.AccessToken {
			t.Fatalf("Load = %+v, %v", got, err)
		}
		if s.Backend() != backendKeyring {
			t.Fatalf("backend = %q", s.Backend())
		}
	})

	t.Run("migra el archivo al llavero y lo borra", func(t *testing.T) {
		keyring.MockInit()
		s, file, _ := newTestStore(t)
		if err := file.Save(testToken); err != nil {
			t.Fatal(err)
		}
		got, err := s.Load()
		if err != nil || got.AccessToken != testToken.AccessToken {
			t.Fatalf("Load = %+v, %v", got, err)
		}
		if fileExists(t, file) {
			t.Fatal("credentials.json debía borrarse tras migrar")
		}
		if stored, err := (KeyringTokenStore{}).Load(); err != nil || stored.AccessToken != testToken.AccessToken {
			t.Fatalf("llavero = %+v, %v", stored, err)
		}
		if s.Backend() != backendKeyring {
			t.Fatalf("backend = %q", s.Backend())
		}
	})

	t.Run("llavero vacío y no escribible conserva el archivo", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("dbus"))
		s, file, warn := newTestStore(t)
		if err := file.Save(testToken); err != nil {
			t.Fatal(err)
		}
		got, err := s.Load()
		if err != nil || got.AccessToken != testToken.AccessToken {
			t.Fatalf("Load = %+v, %v", got, err)
		}
		if !fileExists(t, file) {
			t.Fatal("el archivo no debe tocarse si el llavero falla")
		}
		if s.Backend() != backendFile || !strings.Contains(warn.String(), file.Path()) {
			t.Fatalf("backend = %q, aviso = %q", s.Backend(), warn.String())
		}
	})

	t.Run("llavero no disponible lee el archivo y avisa una vez", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("dbus"))
		s, file, warn := newTestStore(t)
		if err := file.Save(testToken); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if _, err := s.Load(); err != nil {
				t.Fatal(err)
			}
		}
		if n := strings.Count(warn.String(), "aviso:"); n != 1 {
			t.Fatalf("avisos = %d, quería 1", n)
		}
	})

	t.Run("nada en ningún lado", func(t *testing.T) {
		keyring.MockInit()
		s, _, _ := newTestStore(t)
		if _, err := s.Load(); !errors.Is(err, ErrTokenNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDefaultTokenStore_Save(t *testing.T) {
	t.Run("llavero OK guarda y borra el archivo residual", func(t *testing.T) {
		keyring.MockInit()
		s, file, _ := newTestStore(t)
		if err := file.Save(Token{AccessToken: "viejo"}); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(testToken); err != nil {
			t.Fatal(err)
		}
		if fileExists(t, file) {
			t.Fatal("el archivo residual debía borrarse")
		}
		if stored, err := (KeyringTokenStore{}).Load(); err != nil || stored.AccessToken != testToken.AccessToken {
			t.Fatalf("llavero = %+v, %v", stored, err)
		}
	})

	t.Run("llavero no disponible guarda en archivo 0600 y avisa", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("dbus"))
		s, file, warn := newTestStore(t)
		if err := s.Save(testToken); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file.Path())
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("permisos = %o", perm)
		}
		if s.Backend() != backendFile || !strings.Contains(warn.String(), file.Path()) {
			t.Fatalf("backend = %q, aviso = %q", s.Backend(), warn.String())
		}
	})
}

func TestDefaultTokenStore_Delete(t *testing.T) {
	keyring.MockInit()
	s, file, _ := newTestStore(t)
	if err := (KeyringTokenStore{}).Save(testToken); err != nil {
		t.Fatal(err)
	}
	if err := file.Save(testToken); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := (KeyringTokenStore{}).Load(); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("llavero: %v", err)
	}
	if fileExists(t, file) {
		t.Fatal("el archivo debía borrarse")
	}
	if err := s.Delete(); err != nil {
		t.Fatalf("segundo Delete no debe fallar: %v", err)
	}
}

func TestNewDefaultTokenStore_EnvFileSkipsKeyring(t *testing.T) {
	t.Setenv(tokenStoreEnv, "file")
	s, err := NewDefaultTokenStore()
	if err != nil {
		t.Fatal(err)
	}
	if s.keyring != nil || s.Backend() != backendFile {
		t.Fatalf("keyring = %v, backend = %q", s.keyring, s.Backend())
	}
}
