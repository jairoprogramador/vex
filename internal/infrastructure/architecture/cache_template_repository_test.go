package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jairoprogramador/vex/internal/domain/architecture/ports"
	"github.com/jairoprogramador/vex/internal/domain/architecture/vos"
)

const (
	commitA   = "0123456789abcdef0123456789abcdef01234567"
	officialT = "https://github.com/jairoprogramador/vex-tpl-node-azure-basic.git"
)

var (
	digest     = "sha256:" + strings.Repeat("a", 64)
	bootstrapA = strings.Repeat("b", 64)
)

func entry() templateEntry {
	return templateEntry{
		Stack: "node", Platform: "azure", Level: 1, Cost: 1, Template: officialT, Ref: commitA,
		Runtime: "docker.io/jairoprogramador/node:1.0.0@" + digest, Contract: "azure/v1", BootstrapSHA256: bootstrapA,
	}
}

func catalogJSON(t *testing.T, entries ...templateEntry) []byte {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fakeVerifier acepta un catálogo solo con la «firma» que le corresponde: "firma:" + el contenido firmado. La
// hora de firma viaja en signedAt.
type fakeVerifier struct {
	signedAt time.Time
	calls    int
}

func signature(data []byte) []byte { return append([]byte("firma:"), data...) }

func (v *fakeVerifier) Verify(data, bundle []byte) (time.Time, error) {
	v.calls++
	if !bytes.Equal(bundle, signature(data)) {
		return time.Time{}, ports.ErrCatalogSignature
	}
	return v.signedAt, nil
}

// store es un servidor que publica un catálogo y su firma.
type store struct {
	server  *httptest.Server
	catalog []byte
	bundle  []byte
	hits    int
}

func newStore(t *testing.T, catalog []byte) *store {
	t.Helper()
	s := &store{catalog: catalog, bundle: signature(catalog)}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits++
		switch r.URL.Path {
		case "/templates.json":
			_, _ = w.Write(s.catalog)
		case "/templates.json.sigstore.json":
			if s.bundle == nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(s.bundle)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *store) url() string { return s.server.URL + "/templates.json" }

func query() vos.QueryTemplate {
	return vos.NewQueryTemplate(vos.WithStack("node"), vos.WithPlatform("azure"), vos.WithLevel(1), vos.WithCost(1))
}

func signedRepo(t *testing.T, s *store, verifier *fakeVerifier) (*CacheTemplateRepository, string) {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "pipelines.json")
	return NewCacheTemplateRepository(cache, s.url(), verifier), cache
}

func TestSEC25_ElCatalogoFirmadoFijaRefDigestYContrato(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	repo, cache := signedRepo(t, s, &fakeVerifier{signedAt: time.Unix(1000, 0)})

	unit, err := repo.GetExecutionUnit(query())
	if err != nil {
		t.Fatal(err)
	}
	if unit.Template().Ref() != commitA || unit.Image().Digest() != digest || unit.Contract() != "azure/v1" {
		t.Errorf("unidad = ref %q, digest %q, contrato %q", unit.Template().Ref(), unit.Image().Digest(), unit.Contract())
	}
	for _, path := range []string{cache, cache + ".sigstore.json", cache + ".meta"} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("no se guardó %s: %v", filepath.Base(path), err)
		}
	}
}

func TestSEC33_TemplatesAlteradoUnByte(t *testing.T) {
	good := catalogJSON(t, entry())
	s := newStore(t, good)
	s.catalog = bytes.Replace(good, []byte("0123456789"), []byte("1123456789"), 1) // la firma sigue siendo la del original
	repo, cache := signedRepo(t, s, &fakeVerifier{})

	_, err := repo.GetExecutionUnit(query())
	if !errors.Is(err, ports.ErrCatalogSignature) {
		t.Fatalf("error = %v; se esperaba ErrCatalogSignature", err)
	}
	if _, statErr := os.Stat(cache); !os.IsNotExist(statErr) {
		t.Error("un catálogo sin verificar no se guarda")
	}
}

func TestSEC33_SinFirmaPublicadaNoSeUsa(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	s.bundle = nil
	repo, _ := signedRepo(t, s, &fakeVerifier{})

	if _, err := repo.GetExecutionUnit(query()); err == nil {
		t.Fatal("un catálogo oficial sin firma no se usa")
	}
}

func TestSEC33_FirmaInvalidaNoCaeALaCopiaGuardada(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	verifier := &fakeVerifier{signedAt: time.Unix(1000, 0)}
	repo, _ := signedRepo(t, s, verifier)
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
	// La copia vence y lo que ahora se publica no verifica.
	repo.now = func() time.Time { return time.Now().Add(2 * cacheTTL) }
	s.bundle = []byte("firma de otro")

	if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogSignature) {
		t.Fatalf("error = %v; se esperaba ErrCatalogSignature, no la copia guardada", err)
	}
}

func TestSEC33_RetrocesoRechazado(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	verifier := &fakeVerifier{signedAt: time.Unix(2000, 0)}
	repo, _ := signedRepo(t, s, verifier)
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
	// Vence la copia y el servidor publica un catálogo bien firmado, pero de antes.
	repo.now = func() time.Time { return time.Now().Add(2 * cacheTTL) }
	verifier.signedAt = time.Unix(1000, 0)

	if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogRollback) {
		t.Fatalf("error = %v; se esperaba ErrCatalogRollback", err)
	}

	// Uno posterior sí se acepta.
	verifier.signedAt = time.Unix(3000, 0)
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
}

func TestSEC33_SinRedUsaCacheVerificada(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	verifier := &fakeVerifier{signedAt: time.Unix(1000, 0)}
	repo, cache := signedRepo(t, s, verifier)
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
	s.server.Close()
	repo.now = func() time.Time { return time.Now().Add(2 * cacheTTL) }

	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatalf("sin red se usa la copia verificada: %v", err)
	}

	// Una copia manipulada en disco no pasa: se vuelve a verificar al leerla.
	if err := os.WriteFile(cache, catalogJSON(t, templateEntry{Stack: "node", Platform: "azure", Level: 1, Cost: 1,
		Template: "https://github.com/atacante/x.git", Ref: commitA, Runtime: "x/y:1@" + digest}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogUnavailable) {
		t.Fatalf("error = %v; una copia que no verifica no se usa", err)
	}
}

func TestSEC33_LaCopiaRecienteTambienSeVerifica(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	verifier := &fakeVerifier{signedAt: time.Unix(1000, 0)}
	repo, cache := signedRepo(t, s, verifier)
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
	hits := s.hits
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
	if s.hits != hits {
		t.Error("con copia reciente no se descarga")
	}
	if err := os.WriteFile(cache, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatalf("una copia manipulada se descarta y se descarga de nuevo: %v", err)
	}
	if s.hits == hits {
		t.Error("la copia manipulada se dio por buena")
	}
}

func TestSEC33_SinRedSinCacheFalla(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	repo, _ := signedRepo(t, s, &fakeVerifier{})
	s.server.Close()

	if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogUnavailable) {
		t.Fatalf("error = %v; se esperaba ErrCatalogUnavailable", err)
	}
}

func TestCatalogoFirmado_NoAceptaEntradasSinFijar(t *testing.T) {
	tests := map[string]func(*templateEntry){
		"sin ref":                      func(e *templateEntry) { e.Ref = "" },
		"ref que es una rama":          func(e *templateEntry) { e.Ref = "main" },
		"ref pendiente":                func(e *templateEntry) { e.Ref = "<PENDIENTE: scripts/pin-entry.sh>" },
		"imagen sin digest":            func(e *templateEntry) { e.Runtime = "docker.io/jairoprogramador/node:1.0.0" },
		"contrato sin huella":          func(e *templateEntry) { e.BootstrapSHA256 = "" },
		"contrato con huella a medias": func(e *templateEntry) { e.BootstrapSHA256 = "<PENDIENTE>" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			broken := entry()
			mutate(&broken)
			s := newStore(t, catalogJSON(t, broken))
			repo, _ := signedRepo(t, s, &fakeVerifier{})

			if _, err := repo.GetExecutionUnit(query()); err == nil {
				t.Fatal("el catálogo firmado no entrega nada que pueda cambiar")
			}
		})
	}
}

func TestCatalogoPropio_SinFirmaYConRefPorDefecto(t *testing.T) {
	own := entry()
	own.Ref, own.Runtime, own.Contract, own.BootstrapSHA256 = "", "acme/runtime:1.0", "", ""
	path := filepath.Join(t.TempDir(), "propio.json")
	if err := os.WriteFile(path, catalogJSON(t, own), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := NewCacheTemplateRepository(filepath.Join(t.TempDir(), "cache.json"), path, nil)

	unit, err := repo.GetExecutionUnit(query())
	if err != nil {
		t.Fatal(err)
	}
	if unit.Template().Ref() != "main" || unit.Image().Pinned() {
		t.Errorf("catálogo propio: ref %q, imagen %q", unit.Template().Ref(), unit.Image().Spec())
	}
}

func TestCatalogoFirmado_NoSeLeeDeUnArchivoLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.json")
	data := catalogJSON(t, entry())
	_ = os.WriteFile(path, data, 0o600)
	_ = os.WriteFile(path+".sigstore.json", signature(data), 0o600)
	repo := NewCacheTemplateRepository(filepath.Join(t.TempDir(), "cache.json"), path, &fakeVerifier{})

	if _, err := repo.GetExecutionUnit(query()); err == nil {
		t.Fatal("el catálogo firmado solo viene de su URL")
	}
}

func TestCatalogo_RechazaRespuestasEnormes(t *testing.T) {
	s := newStore(t, bytes.Repeat([]byte(" "), maxCatalogBytes+2))
	repo, _ := signedRepo(t, s, &fakeVerifier{})

	if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestLookup(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	repo, _ := signedRepo(t, s, &fakeVerifier{signedAt: time.Unix(1000, 0)})

	tests := []struct {
		name, url, commit string
		listed            bool
	}{
		{"la plantilla del catálogo en su commit", officialT, commitA, true},
		{"sin .git y con mayúsculas", "https://github.com/JairoProgramador/vex-tpl-node-azure-basic", commitA, true},
		{"por ssh", "git@github.com:jairoprogramador/vex-tpl-node-azure-basic.git", commitA, true},
		{"en otro commit", officialT, strings.Repeat("f", 40), false},
		{"otra plantilla de vex que el catálogo no publica", "https://github.com/jairoprogramador/otra.git", commitA, false},
		{"plantilla propia", "https://github.com/acme/pipe.git", commitA, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listing, err := repo.Lookup(tt.url, tt.commit)
			if err != nil {
				t.Fatal(err)
			}
			if listing.Listed != tt.listed || (tt.listed && listing.BootstrapSHA256 != bootstrapA) {
				t.Errorf("Lookup = %+v", listing)
			}
		})
	}
}

// Barandilla 4, fallo cerrado: una plantilla de vex no se usa si su catálogo no se puede comprobar; una propia
// no depende del catálogo para nada.
func TestLookup_SinCatalogoVerificable(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	s.bundle = []byte("firma de otro")
	repo, _ := signedRepo(t, s, &fakeVerifier{})

	if _, err := repo.Lookup(officialT, commitA); !errors.Is(err, ports.ErrCatalogSignature) {
		t.Fatalf("plantilla de vex: error = %v; se esperaba ErrCatalogSignature", err)
	}
	hits := s.hits
	listing, err := repo.Lookup("https://github.com/acme/pipe.git", commitA)
	if err != nil || listing.Listed {
		t.Fatalf("plantilla propia: %+v, %v", listing, err)
	}
	if s.hits != hits {
		t.Error("una plantilla propia no necesita el catálogo")
	}

	s.server.Close()
	if _, err := repo.Lookup(officialT, commitA); !errors.Is(err, ports.ErrCatalogUnavailable) {
		t.Fatalf("sin red ni copia: error = %v", err)
	}
}

func TestCanonicalRepository(t *testing.T) {
	want := "github.com/jairoprogramador/x"
	for _, url := range []string{
		"https://github.com/jairoprogramador/x.git", "https://github.com/jairoprogramador/x/", "HTTPS://GitHub.com/JairoProgramador/X",
		"git@github.com:jairoprogramador/x.git", "ssh://git@github.com/jairoprogramador/x", "https://user@github.com/jairoprogramador/x.git",
		// Variantes que apuntan al mismo repositorio y antes no se reconocían como de vex.
		"https://github.com:443/jairoprogramador/x.git", "ssh://git@github.com:22/jairoprogramador/x.git", "github.com:443/jairoprogramador/x",
		"https://www.github.com/jairoprogramador/x", "https://github.com//jairoprogramador//x.git/", "https://GITHUB.COM./jairoprogramador/x",
		"git+ssh://git@github.com/jairoprogramador/x.git", "https://user:clave@github.com:443/jairoprogramador/x",
	} {
		if got := canonicalRepository(url); got != want {
			t.Errorf("canonicalRepository(%q) = %q", url, got)
		}
	}
	// Lo que solo se parece no es de vex.
	for _, url := range []string{
		"https://github.com/jairoprogramador-evil/x.git", "https://github.com.evil.example/jairoprogramador/x.git",
		"https://evil.example/github.com/jairoprogramador/x.git", "https://github.com/atacante/jairoprogramador",
		"https://github.com:8443/jairoprogramador/x", "https://evil.example/x@github.com/jairoprogramador/x",
		"https://github.com@evil.example/jairoprogramador/x",
	} {
		if strings.HasPrefix(canonicalRepository(url), officialTemplates) {
			t.Errorf("%q se tomaría por una plantilla de vex", url)
		}
	}
}

// B6 de la revisión de seguridad: la copia guardada tiene una edad máxima y no vale sin su marca de fecha.

func TestB6_UnaCopiaDeMasDeSieteDiasNoSeUsaSinRed(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	repo, _ := signedRepo(t, s, &fakeVerifier{signedAt: time.Unix(1000, 0)})
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
	s.server.Close()

	repo.now = func() time.Time { return time.Now().Add(maxCacheAge - time.Hour) }
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatalf("dentro del plazo se usa la copia verificada: %v", err)
	}

	repo.now = func() time.Time { return time.Now().Add(maxCacheAge + time.Hour) }
	if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogStale) {
		t.Fatalf("error = %v; se esperaba ErrCatalogStale", err)
	}
	if _, err := repo.Lookup("https://github.com/jairoprogramador/x.git", commitA); !errors.Is(err, ports.ErrCatalogStale) {
		t.Fatalf("Lookup: error = %v; una plantilla de vex no se da por «no listada» con una copia vencida", err)
	}
}

func TestB6_UnaCopiaSinSuMarcaDeFechaEsUnError(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, meta string){
		"marca borrada": func(t *testing.T, meta string) {
			if err := os.Remove(meta); err != nil {
				t.Fatal(err)
			}
		},
		"marca corrupta": func(t *testing.T, meta string) {
			if err := os.WriteFile(meta, []byte(`{"signed_at": `), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"marca vacía": func(t *testing.T, meta string) {
			if err := os.WriteFile(meta, []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t, catalogJSON(t, entry()))
			verifier := &fakeVerifier{signedAt: time.Unix(2000, 0)}
			repo, cache := signedRepo(t, s, verifier)
			if _, err := repo.GetExecutionUnit(query()); err != nil {
				t.Fatal(err)
			}
			damage(t, cache+".meta")
			// Sin la marca, este catálogo anterior y bien firmado pasaría por nuevo.
			repo.now = func() time.Time { return time.Now().Add(2 * cacheTTL) }
			verifier.signedAt = time.Unix(1000, 0)

			if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogCacheBroken) {
				t.Fatalf("error = %v; se esperaba ErrCatalogCacheBroken", err)
			}
		})
	}
	t.Run("sin copia es la primera vez", func(t *testing.T) {
		s := newStore(t, catalogJSON(t, entry()))
		repo, _ := signedRepo(t, s, &fakeVerifier{signedAt: time.Unix(1000, 0)})

		if _, err := repo.GetExecutionUnit(query()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestB6_LaCopiaNoPuedeSerAnteriorAlUltimoCatalogoAceptado(t *testing.T) {
	s := newStore(t, catalogJSON(t, entry()))
	verifier := &fakeVerifier{signedAt: time.Unix(2000, 0)}
	repo, _ := signedRepo(t, s, verifier)
	if _, err := repo.GetExecutionUnit(query()); err != nil {
		t.Fatal(err)
	}
	// Alguien cambia la copia en disco por un catálogo antiguo con su firma válida, y no hay red.
	s.server.Close()
	verifier.signedAt = time.Unix(1000, 0)

	if _, err := repo.GetExecutionUnit(query()); !errors.Is(err, ports.ErrCatalogUnavailable) {
		t.Fatalf("error = %v; una copia anterior a la marca no se usa", err)
	}
}
