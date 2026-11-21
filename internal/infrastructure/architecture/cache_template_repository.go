package architecture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/domain/architecture/ports"
	"github.com/jairoprogramador/vex/internal/domain/architecture/vos"
	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
)

const (
	cacheTTL = 24 * time.Hour
	// maxCacheAge es cuánto se sigue usando la copia verificada cuando no se puede descargar el catálogo. Pasado
	// ese tiempo se falla cerrado: una revocación publicada no puede quedar oculta por un corte de red indefinido.
	maxCacheAge = 7 * 24 * time.Hour
	httpTimeout = 5 * time.Second
	// maxCatalogBytes acota lo que se lee del catálogo y de su firma.
	maxCatalogBytes = 1 << 20

	// OfficialCatalogURL es el catálogo del último release firmado del template store.
	OfficialCatalogURL = "https://github.com/jairoprogramador/vex-template-store/releases/latest/download/templates.json"
	// officialTemplates es dónde viven las plantillas que publica vex: una plantilla de ahí tiene que poder
	// compararse con el catálogo firmado.
	officialTemplates = "github.com/jairoprogramador/"
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type templateEntry struct {
	Stack    string `json:"stack"`
	Platform string `json:"platform"`
	Level    int    `json:"level"`
	Cost     int    `json:"cost"`
	Template string `json:"template"`
	// Ref es el commit de la plantilla; Runtime, la imagen con su digest.
	Ref     string `json:"ref"`
	Runtime string `json:"runtime"`
	// Contract es el contrato de nube de la plantilla y BootstrapSHA256, la huella de su bootstrap en Ref.
	Contract        string `json:"contract"`
	BootstrapSHA256 string `json:"bootstrap_sha256"`
}

// catalogMeta recuerda cuándo se firmó el último catálogo aceptado.
type catalogMeta struct {
	SignedAt time.Time `json:"signed_at"`
}

// CacheTemplateRepository lee el catálogo de plantillas. Con verificador, el catálogo es el firmado de vex: no
// se usa nada que no pase la verificación, ni recién descargado ni guardado. Sin verificador es un catálogo
// propio de quien usa la CLI, que responde de él.
type CacheTemplateRepository struct {
	cachePath  string
	bundlePath string
	metaPath   string
	remoteURL  string
	verifier   ports.CatalogVerifier
	client     *http.Client
	now        func() time.Time
}

var (
	_ ports.TemplateRepository    = (*CacheTemplateRepository)(nil)
	_ application.TemplateCatalog = (*CacheTemplateRepository)(nil)
)

// NewCacheTemplateRepository lee el catálogo de source (una URL http(s) o, para un catálogo propio, también
// una ruta local) y lo guarda en cachePath. verifier nil significa catálogo propio, sin firma.
func NewCacheTemplateRepository(cachePath, source string, verifier ports.CatalogVerifier) *CacheTemplateRepository {
	return &CacheTemplateRepository{
		cachePath:  cachePath,
		bundlePath: cachePath + ".sigstore.json",
		metaPath:   cachePath + ".meta",
		remoteURL:  source,
		verifier:   verifier,
		client:     &http.Client{Timeout: httpTimeout},
		now:        time.Now,
	}
}

func (r *CacheTemplateRepository) GetExecutionUnit(query vos.QueryTemplate) (vos.ExecutionUnit, error) {
	entries, err := r.entries()
	if err != nil {
		return vos.ExecutionUnit{}, err
	}
	return r.find(entries, query)
}

// Lookup dice si el catálogo firmado publica la plantilla de pipelineURL en commit. Una plantilla que no vive
// donde vex publica las suyas no está en el catálogo, y no hace falta leerlo para saberlo. Una que sí vive ahí
// tiene que poder comprobarse: si el catálogo no se puede verificar, es un error y no un «no está».
func (r *CacheTemplateRepository) Lookup(pipelineURL, commit string) (application.CatalogListing, error) {
	repository := canonicalRepository(pipelineURL)
	if !strings.HasPrefix(repository, officialTemplates) {
		return application.CatalogListing{}, nil
	}
	entries, err := r.entries()
	if err != nil {
		return application.CatalogListing{}, err
	}
	for _, entry := range entries {
		if canonicalRepository(entry.Template) == repository && entry.Ref == commit && entry.BootstrapSHA256 != "" {
			return application.CatalogListing{Listed: true, BootstrapSHA256: entry.BootstrapSHA256}, nil
		}
	}
	return application.CatalogListing{}, nil
}

// canonicalRepository reduce las formas de escribir un repositorio a host/ruta en minúsculas, para compararlas:
// https, ssh o git@host:ruta, con usuario, con el puerto por defecto (:443, :22), con www., con .git o con barras
// de más. Otro puerto es otro servidor y no se quita.
func canonicalRepository(url string) string {
	repository := strings.ToLower(strings.TrimSpace(url))
	hasScheme := false
	for _, scheme := range []string{"https://", "http://", "git+ssh://", "ssh://", "git://"} {
		if rest, ok := strings.CutPrefix(repository, scheme); ok {
			repository, hasScheme = rest, true
			break
		}
	}
	authority, path, _ := strings.Cut(repository, "/")
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	if host, rest, ok := strings.Cut(authority, ":"); ok {
		switch {
		case rest == "443" || rest == "22" || rest == "":
			authority = host
		case !hasScheme && strings.Trim(rest, "0123456789") != "":
			// git@host:ruta
			authority, path = host, rest+"/"+path
		}
	}
	authority = strings.TrimPrefix(strings.TrimSuffix(authority, "."), "www.")
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	path = strings.TrimSuffix(strings.Join(segments, "/"), ".git")
	if path == "" {
		return authority
	}
	return authority + "/" + path
}

func (r *CacheTemplateRepository) entries() ([]templateEntry, error) {
	data, err := r.resolve()
	if err != nil {
		return nil, err
	}
	var entries []templateEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Con firma, un catálogo ilegible es un error de quien lo publicó; sin firma, puede ser una copia rota.
		if r.verifier == nil {
			r.removeCache()
		}
		return nil, fmt.Errorf("el catálogo de plantillas no se entiende: %w", err)
	}
	return entries, nil
}

// resolve devuelve el catálogo a usar: la copia guardada si es reciente, o uno recién descargado. Con
// verificador, todo lo que devuelve pasó la verificación; una firma inválida o un retroceso no caen a la copia
// guardada. Solo un fallo de red lo hace.
func (r *CacheTemplateRepository) resolve() ([]byte, error) {
	if err := r.checkMeta(); err != nil {
		return nil, err
	}
	if r.isCacheFresh() {
		if data, err := r.readCache(); err == nil {
			return data, nil
		}
	}

	data, bundle, err := r.download()
	if err != nil {
		if cached, cacheErr := r.readCache(); cacheErr == nil {
			if r.verifier != nil && r.cacheAge() > maxCacheAge {
				return nil, fmt.Errorf("%w: no se pudo descargar uno nuevo (%v)", ports.ErrCatalogStale, err)
			}
			return cached, nil
		}
		return nil, fmt.Errorf("%w: %v", ports.ErrCatalogUnavailable, err)
	}
	signedAt, err := r.accept(data, bundle)
	if err != nil {
		return nil, err
	}
	// Guardar es de mejor esfuerzo: sin copia, la próxima vez se descarga otra vez.
	_ = r.writeCache(data, bundle, signedAt)
	return data, nil
}

// accept verifica un catálogo recién descargado y rechaza uno más antiguo que el último aceptado: también
// estaría bien firmado.
func (r *CacheTemplateRepository) accept(data, bundle []byte) (time.Time, error) {
	if r.verifier == nil {
		return time.Time{}, nil
	}
	signedAt, err := r.verifier.Verify(data, bundle)
	if err != nil {
		return time.Time{}, err
	}
	if last, ok := r.lastSignedAt(); ok && signedAt.Before(last) {
		return time.Time{}, ports.ErrCatalogRollback
	}
	return signedAt, nil
}

// checkMeta exige que una copia guardada del catálogo firmado tenga su marca de fecha. Sin copia no hay nada que
// exigir (es la primera vez); con copia y sin marca, alguien la quitó o se dañó, y no se sigue como si nada: sin
// ella un catálogo antiguo y bien firmado pasaría por nuevo.
func (r *CacheTemplateRepository) checkMeta() error {
	if r.verifier == nil {
		return nil
	}
	if _, ok := r.lastSignedAt(); ok {
		return nil
	}
	for _, path := range []string{r.cachePath, r.bundlePath} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%w: falta o no se entiende %s", ports.ErrCatalogCacheBroken, r.metaPath)
		}
	}
	return nil
}

func (r *CacheTemplateRepository) cacheAge() time.Duration {
	info, err := os.Stat(r.cachePath)
	if err != nil {
		return maxCacheAge + time.Hour
	}
	return r.now().Sub(info.ModTime())
}

func (r *CacheTemplateRepository) lastSignedAt() (time.Time, bool) {
	raw, err := os.ReadFile(r.metaPath)
	if err != nil {
		return time.Time{}, false
	}
	var meta catalogMeta
	if err := json.Unmarshal(raw, &meta); err != nil || meta.SignedAt.IsZero() {
		return time.Time{}, false
	}
	return meta.SignedAt, true
}

func (r *CacheTemplateRepository) isCacheFresh() bool {
	info, err := os.Stat(r.cachePath)
	if err != nil {
		return false
	}
	return r.now().Sub(info.ModTime()) < cacheTTL
}

// readCache lee la copia guardada. Con verificador se vuelve a verificar con su firma: lo que hay en disco no
// se da por bueno.
func (r *CacheTemplateRepository) readCache() ([]byte, error) {
	data, err := os.ReadFile(r.cachePath)
	if err != nil || r.verifier == nil {
		return data, err
	}
	bundle, err := os.ReadFile(r.bundlePath)
	if err != nil {
		return nil, err
	}
	signedAt, err := r.verifier.Verify(data, bundle)
	if err != nil {
		return nil, err
	}
	// La copia tampoco puede ser anterior al último catálogo aceptado: cambiarla en disco por una vieja y bien
	// firmada no la hace buena.
	if last, ok := r.lastSignedAt(); !ok || signedAt.Before(last) {
		return nil, ports.ErrCatalogRollback
	}
	return data, nil
}

func (r *CacheTemplateRepository) removeCache() {
	_ = os.Remove(r.cachePath)
}

func (r *CacheTemplateRepository) writeCache(data, bundle []byte, signedAt time.Time) error {
	if err := os.MkdirAll(filepath.Dir(r.cachePath), 0o700); err != nil {
		return err
	}
	if r.verifier != nil {
		meta, err := json.Marshal(catalogMeta{SignedAt: signedAt})
		if err != nil {
			return err
		}
		// Primero la marca de fecha y la firma, después el catálogo: si algo se corta a medias, nunca queda una
		// copia sin su marca o sin su firma.
		if err := writeFileAtomic(r.metaPath, meta); err != nil {
			return err
		}
		if err := writeFileAtomic(r.bundlePath, bundle); err != nil {
			return err
		}
	}
	return writeFileAtomic(r.cachePath, data)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// download trae el catálogo y, si se va a verificar, su firma, que se publica junto a él.
func (r *CacheTemplateRepository) download() (data, bundle []byte, err error) {
	if data, err = r.fetch(r.remoteURL); err != nil {
		return nil, nil, err
	}
	if r.verifier == nil {
		return data, nil, nil
	}
	if bundle, err = r.fetch(r.remoteURL + ".sigstore.json"); err != nil {
		return nil, nil, fmt.Errorf("firma del catálogo: %w", err)
	}
	return data, bundle, nil
}

func (r *CacheTemplateRepository) fetch(source string) ([]byte, error) {
	if !strings.Contains(source, "://") {
		if r.verifier != nil {
			return nil, errors.New("el catálogo firmado no se lee de un archivo local")
		}
		return readLimited(source)
	}
	resp, err := r.client.Get(source)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("el servidor respondió %d", resp.StatusCode)
	}
	return readAllLimited(resp.Body)
}

func readLimited(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readAllLimited(file)
}

func readAllLimited(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxCatalogBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCatalogBytes {
		return nil, fmt.Errorf("pesa más de %d bytes", maxCatalogBytes)
	}
	return data, nil
}

func (r *CacheTemplateRepository) find(templates []templateEntry, query vos.QueryTemplate) (vos.ExecutionUnit, error) {
	for _, template := range templates {
		if template.Level == query.Level() &&
			template.Cost == query.Cost() &&
			template.Stack == query.Stack() &&
			template.Platform == query.Platform() {
			return r.executionUnit(template)
		}
	}
	return vos.ExecutionUnit{}, errors.New("el catálogo no tiene una plantilla para ese nivel y costo")
}

func (r *CacheTemplateRepository) executionUnit(template templateEntry) (vos.ExecutionUnit, error) {
	ref := template.Ref
	if r.verifier != nil {
		// El catálogo firmado fija todo lo que decide qué se ejecuta: nada apunta a algo que pueda cambiar.
		if err := validateSignedEntry(template); err != nil {
			return vos.ExecutionUnit{}, fmt.Errorf("la plantilla %s del catálogo firmado %w", template.Template, err)
		}
	} else if ref == "" {
		ref = comVos.DefaultPipelineRef
	}
	pipeline, err := comVos.NewPipeline(template.Template, ref)
	if err != nil {
		return vos.ExecutionUnit{}, err
	}
	image, err := comVos.NewImage(template.Runtime)
	if err != nil {
		return vos.ExecutionUnit{}, err
	}
	return vos.NewExecutionUnit(image, pipeline).WithContract(template.Contract), nil
}

func validateSignedEntry(template templateEntry) error {
	image, err := comVos.NewImage(template.Runtime)
	switch {
	case !commitPattern.MatchString(template.Ref):
		return errors.New("no fija ref a un commit")
	case err != nil || !image.Pinned():
		return errors.New("no fija el digest de su imagen de runtime")
	case template.Contract != "" && !sha256Pattern.MatchString(template.BootstrapSHA256):
		return errors.New("declara un contrato de nube sin la huella de su bootstrap")
	}
	return nil
}
