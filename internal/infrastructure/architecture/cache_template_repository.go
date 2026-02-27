package architecture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jairoprogramador/vex-client/internal/domain/architecture/ports"
	"github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"
	comVos "github.com/jairoprogramador/vex-client/internal/domain/common/vos"
	defVos "github.com/jairoprogramador/vex-client/internal/domain/common/vos"
)

const cacheTTL = 24 * time.Hour
const httpTimeout = 5 * time.Second

type templateEntry struct {
	Stack    string `json:"stack"`
	Platform string `json:"platform"`
	Level    int    `json:"level"`
	Cost     int    `json:"cost"`
	Template string `json:"template"`
	Runtime  string `json:"runtime"`
}

type CacheTemplateRepository struct {
	cachePath string
	remoteURL string
	client    *http.Client
}

func NewCacheTemplateRepository(cachePath, remoteURL string) ports.TemplateRepository {
	return &CacheTemplateRepository{
		cachePath: cachePath,
		remoteURL: remoteURL,
		client:    &http.Client{Timeout: httpTimeout},
	}
}

func (r *CacheTemplateRepository) GetExecutionUnit(query vos.QueryTemplate) (vos.ExecutionUnit, error) {
	data, err := r.resolve()
	if err != nil {
		return vos.ExecutionUnit{}, fmt.Errorf("failed to resolve templates: %w", err)
	}

	entries, err := r.parse(data)
	if err != nil {
		_ = r.removeCache()
		return vos.ExecutionUnit{}, fmt.Errorf("failed to parse templates: %w", err)
	}

	return r.find(entries, query)
}

func (r *CacheTemplateRepository) GetRuntime(query vos.QueryTemplate) (vos.ExecutionUnit, error) {
	data, err := r.resolve()
	if err != nil {
		return vos.ExecutionUnit{}, fmt.Errorf("failed to resolve templates: %w", err)
	}

	entries, err := r.parse(data)
	if err != nil {
		_ = r.removeCache()
		return vos.ExecutionUnit{}, fmt.Errorf("failed to parse templates: %w", err)
	}

	return r.find(entries, query)
}

func (r *CacheTemplateRepository) resolve() ([]byte, error) {
	if r.isCacheFresh() {
		return r.readCache()
	}

	data, err := r.download()
	if err == nil {
		_ = r.writeCache(data)
		return data, nil
	}

	cached, cacheErr := r.readCache()
	if cacheErr == nil {
		return cached, nil
	}

	return nil, fmt.Errorf("no templates available: remote=%w, cache=%w", err, cacheErr)
}

func (r *CacheTemplateRepository) isCacheFresh() bool {
	info, err := os.Stat(r.cachePath)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < cacheTTL
}

func (r *CacheTemplateRepository) readCache() ([]byte, error) {
	return os.ReadFile(r.cachePath)
}

func (r *CacheTemplateRepository) removeCache() error {
	return os.Remove(r.cachePath)
}

func (r *CacheTemplateRepository) writeCache(data []byte) error {
	dir := filepath.Dir(r.cachePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmp := r.cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, r.cachePath)
}

func (r *CacheTemplateRepository) download() ([]byte, error) {
	resp, err := r.client.Get(r.remoteURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (r *CacheTemplateRepository) parse(data []byte) ([]templateEntry, error) {
	var entries []templateEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *CacheTemplateRepository) find(templates []templateEntry, query vos.QueryTemplate) (vos.ExecutionUnit, error) {
	for _, template := range templates {
		if template.Level == query.Level() &&
			template.Cost == query.Cost() &&
			template.Stack == query.Stack() &&
			template.Platform == query.Platform() {
			templateObj, err := comVos.NewTemplate(template.Template, defVos.DefaultTemplateRef)
			if err != nil {
				return vos.ExecutionUnit{}, err
			}
			imageName := strings.Split(template.Runtime, ":")[0]
			imageTag := strings.Split(template.Runtime, ":")[1]
			imageObj, err := comVos.NewImage(imageName, imageTag)
			if err != nil {
				return vos.ExecutionUnit{}, err
			}
			return vos.NewExecutionUnit(imageObj, templateObj), nil
		}
	}
	return vos.ExecutionUnit{}, errors.New("template not found for the given level and cost")
}
