package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
	"gopkg.in/yaml.v3"
)

// FlowsFile is the committed, secret-free flow definition file per project.
const FlowsFile = ".rdm/flows.yml"

type flowsDoc struct {
	Flows []domain.Flow `yaml:"flows"`
}

// LoadFlows reads and validates .rdm/flows.yml. A missing file yields (nil, nil).
func LoadFlows(repoPath string) ([]domain.Flow, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, FlowsFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read flows.yml: %w", err)
	}
	var doc flowsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse flows.yml: %w", err)
	}
	if err := ValidateFlows(doc.Flows); err != nil {
		return nil, err
	}
	return doc.Flows, nil
}

// SaveFlows validates then writes .rdm/flows.yml, creating .rdm if needed.
func SaveFlows(repoPath string, flows []domain.Flow) error {
	if err := ValidateFlows(flows); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(repoPath, ".rdm"), 0o755); err != nil {
		return fmt.Errorf("mkdir .rdm: %w", err)
	}
	data, err := yaml.Marshal(flowsDoc{Flows: flows})
	if err != nil {
		return fmt.Errorf("marshal flows.yml: %w", err)
	}
	return os.WriteFile(filepath.Join(repoPath, FlowsFile), data, 0o644)
}

// ValidateFlows enforces flow invariants; returns the first violation found.
func ValidateFlows(flows []domain.Flow) error {
	seen := map[string]bool{}
	for i, f := range flows {
		if strings.TrimSpace(f.Name) == "" {
			return fmt.Errorf("flow %d: naam ontbreekt", i)
		}
		if seen[f.Name] {
			return fmt.Errorf("dubbele flow-naam %q", f.Name)
		}
		seen[f.Name] = true
		if len(f.Steps) == 0 {
			return fmt.Errorf("flow %q: geen stappen", f.Name)
		}
		for j, s := range f.Steps {
			if !s.Action.Valid() {
				return fmt.Errorf("flow %q stap %d: onbekende actie %q", f.Name, j, s.Action)
			}
			if s.Action == domain.StepInput && strings.TrimSpace(s.Value) == "" {
				return fmt.Errorf("flow %q stap %d: type-stap zonder waarde", f.Name, j)
			}
			if s.Action != domain.StepWait && strings.TrimSpace(s.Target) == "" {
				return fmt.Errorf("flow %q stap %d: target ontbreekt", f.Name, j)
			}
		}
	}
	return nil
}
