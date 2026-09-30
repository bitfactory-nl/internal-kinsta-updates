package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/rdm/sites-tool/internal/domain"
)

type localProjectsFile struct {
	Projects []domain.LocalProject `yaml:"projects"`
}

// LocalProjectsPath is het per-gebruiker bestand met lokale projecten die geen
// WordPress-site zijn.
func LocalProjectsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "local-projects.yml")
}

func LoadLocalProjects(path string) ([]domain.LocalProject, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.LocalProject{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lees %s: %w", path, err)
	}
	var f localProjectsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range f.Projects {
		f.Projects[i].Path = ExpandHome(f.Projects[i].Path)
	}
	if err := validateLocalProjects(f.Projects); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Projects == nil {
		f.Projects = []domain.LocalProject{}
	}
	return f.Projects, nil
}

func SaveLocalProjects(path string, ps []domain.LocalProject) error {
	if err := validateLocalProjects(ps); err != nil {
		return fmt.Errorf("local-projects: %w", err)
	}
	out := make([]domain.LocalProject, len(ps))
	for i, p := range ps {
		p.Path = CompressHome(p.Path)
		out[i] = p
	}
	data, err := yaml.Marshal(localProjectsFile{Projects: out})
	if err != nil {
		return fmt.Errorf("local-projects: %w", err)
	}
	return atomischSchrijven(path, data)
}

func validateLocalProjects(ps []domain.LocalProject) error {
	gezien := map[string]bool{}
	for i, p := range ps {
		if p.Path == "" {
			return fmt.Errorf("project %d: pad is verplicht", i+1)
		}
		key := filepath.Clean(p.Path)
		if gezien[key] {
			return fmt.Errorf("map %s staat al in het register", p.Path)
		}
		gezien[key] = true
	}
	return nil
}

// ValidateRegister controleert dat geen map tegelijk aan een site en aan een
// lokaal project hangt.
func ValidateRegister(sites []domain.WPSite, locals []domain.LocalProject) error {
	paden := map[string]bool{}
	for _, s := range sites {
		if s.ProjectPath != "" {
			paden[filepath.Clean(s.ProjectPath)] = true
		}
	}
	for _, p := range locals {
		if paden[filepath.Clean(p.Path)] {
			return fmt.Errorf("map %s staat al in het register", p.Path)
		}
	}
	return validateLocalProjects(locals)
}
