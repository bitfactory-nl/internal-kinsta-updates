package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rdm/sites-tool/internal/domain"
	"gopkg.in/yaml.v3"
)

const ProjectConfigFile = ".rdm.yml"

func LoadProject(repoPath string) (domain.ProjectConfig, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, ProjectConfigFile))
	if os.IsNotExist(err) {
		return domain.ProjectConfig{}, nil
	}
	if err != nil {
		return domain.ProjectConfig{}, fmt.Errorf("read .rdm.yml: %w", err)
	}

	var cfg domain.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return domain.ProjectConfig{}, fmt.Errorf("parse .rdm.yml: %w", err)
	}
	return cfg, nil
}

func SaveProject(repoPath string, cfg domain.ProjectConfig) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal .rdm.yml: %w", err)
	}
	return os.WriteFile(filepath.Join(repoPath, ProjectConfigFile), data, 0o644)
}

func HasProjectConfig(repoPath string) bool {
	_, err := os.Stat(filepath.Join(repoPath, ProjectConfigFile))
	return err == nil
}
