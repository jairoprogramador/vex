package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const vexConfigFileName = "vexconfig.yaml"

type Config struct {
	Mode ExecutionMode `yaml:"mode,omitempty"`
}

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

func LoadEffective(projectPath string) (Config, error) {
	global, err := loadFromPath(GlobalConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("global config: %w", err)
	}

	user, err := loadUserConfig()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("user config: %w", err)
	}

	proj, err := loadProjectModeField(projectPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("project config: %w", err)
	}

	merged := Config{}
	if global.Mode != ModeUnset {
		merged.Mode = global.Mode
	}
	if user.Mode != ModeUnset {
		merged.Mode = user.Mode
	}
	if proj.Mode != ModeUnset {
		merged.Mode = proj.Mode
	}

	if merged.Mode == ModeUnset {
		merged.Mode = ModeRemote
	}
	return merged, nil
}

func LoadScope(scope Scope, projectPath string) (Config, error) {
	switch scope {
	case ScopeGlobal:
		cfg, err := loadFromPath(GlobalConfigPath())
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return cfg, err
	case ScopeUser:
		cfg, err := loadUserConfig()
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return cfg, err
	case ScopeProject:
		cfg, err := loadProjectModeField(projectPath)
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return cfg, err
	default:
		return Config{}, fmt.Errorf("scope desconocido: %q", scope)
	}
}

func SaveScope(scope Scope, cfg Config, projectPath string) error {
	switch scope {
	case ScopeProject:
		return updateProjectModeField(projectPath, cfg.Mode)
	case ScopeUser:
		p, err := UserConfigPath()
		if err != nil {
			return err
		}
		return writeConfigAtomic(p, cfg)
	case ScopeGlobal:
		return writeConfigAtomic(GlobalConfigPath(), cfg)
	default:
		return fmt.Errorf("scope desconocido: %q", scope)
	}
}

func loadFromPath(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsear %s: %w", path, err)
	}
	return cfg, nil
}

func loadUserConfig() (Config, error) {
	p, err := UserConfigPath()
	if err != nil {
		return Config{}, err
	}
	return loadFromPath(p)
}

type projectModeOnly struct {
	Mode ExecutionMode `yaml:"mode,omitempty"`
}

func loadProjectModeField(projectPath string) (Config, error) {
	path := filepath.Join(projectPath, vexConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var partial projectModeOnly
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return Config{}, fmt.Errorf("parsear %s: %w", path, err)
	}
	return Config{Mode: partial.Mode}, nil
}

func updateProjectModeField(projectPath string, mode ExecutionMode) error {
	path := filepath.Join(projectPath, vexConfigFileName)

	// Leer el archivo existente en un mapa genérico para preservar todos los campos.
	var raw map[string]any
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("leer %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsear %s: %w", path, err)
		}
	}
	if raw == nil {
		raw = make(map[string]any)
	}

	if mode == ModeUnset {
		delete(raw, "mode")
	} else {
		raw["mode"] = string(mode)
	}

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("serializar config: %w", err)
	}
	return writeAtomic(path, out, 0o644)
}

func writeConfigAtomic(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serializar config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("crear directorio %s: %w", filepath.Dir(path), err)
	}
	return writeAtomic(path, data, 0o644)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("escribir temp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renombrar %s → %s: %w", tmp, path, err)
	}
	return nil
}
