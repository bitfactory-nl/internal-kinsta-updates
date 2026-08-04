package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rdm/sites-tool/internal/domain"
	"gopkg.in/yaml.v3"
)

// ProjectConfigFile is the per-project config, kept inside .rdm/ so the tool's
// files stay together instead of scattered across the customer's repo root.
const ProjectConfigFile = ".rdm/config.yml"

// LegacyProjectConfigFile is where the config lived before it moved into .rdm/.
// Existing checkouts still have it, so reads fall back to it and the next save
// migrates the project across.
const LegacyProjectConfigFile = ".rdm.yml"

// readProjectConfig returns the contents of whichever config file exists,
// preferring the current location. A missing file yields (nil, nil).
func readProjectConfig(repoPath string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, ProjectConfigFile))
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	data, err = os.ReadFile(filepath.Join(repoPath, LegacyProjectConfigFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func LoadProject(repoPath string) (domain.ProjectConfig, error) {
	data, err := readProjectConfig(repoPath)
	if err != nil {
		return domain.ProjectConfig{}, fmt.Errorf("read %s: %w", ProjectConfigFile, err)
	}
	if data == nil {
		return domain.ProjectConfig{}, nil
	}

	var cfg domain.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return domain.ProjectConfig{}, fmt.Errorf("parse %s: %w", ProjectConfigFile, err)
	}
	return cfg, nil
}

// SaveProject writes to .rdm/config.yml. When a project still carries the old
// root-level .rdm.yml, it is removed only after the new file is safely on disk,
// so a failed write never leaves the project without a config.
func SaveProject(repoPath string, cfg domain.ProjectConfig) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", ProjectConfigFile, err)
	}
	dest := filepath.Join(repoPath, ProjectConfigFile)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("mkdir .rdm: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return err
	}
	legacy := filepath.Join(repoPath, LegacyProjectConfigFile)
	if _, err := os.Stat(legacy); err == nil {
		if err := os.Remove(legacy); err != nil {
			return fmt.Errorf("opruimen oude %s: %w", LegacyProjectConfigFile, err)
		}
	}
	return nil
}

func HasProjectConfig(repoPath string) bool {
	if _, err := os.Stat(filepath.Join(repoPath, ProjectConfigFile)); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(repoPath, LegacyProjectConfigFile))
	return err == nil
}
