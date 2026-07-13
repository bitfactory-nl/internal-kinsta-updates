package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/rdm/sites-tool/internal/domain"
)

const runFile = "run.json"

// RunStore persists test-run history on disk under baseDir/<projectID>/<runID>/.
type RunStore struct {
	baseDir string
}

func NewRunStore(baseDir string) *RunStore {
	return &RunStore{baseDir: baseDir}
}

func (s *RunStore) runDir(projectID, runID string) string {
	return filepath.Join(s.baseDir, projectID, runID)
}

// ScreenshotDir ensures and returns the screenshots directory for a run.
func (s *RunStore) ScreenshotDir(projectID, runID string) (string, error) {
	dir := filepath.Join(s.runDir(projectID, runID), "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir screenshots: %w", err)
	}
	return dir, nil
}

// Save writes the run's metadata as run.json.
func (s *RunStore) Save(run domain.TestRun) error {
	dir := s.runDir(run.ProjectID, run.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir run: %w", err)
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, runFile), data, 0o644)
}

// Get reads a single run.
func (s *RunStore) Get(projectID, runID string) (domain.TestRun, error) {
	data, err := os.ReadFile(filepath.Join(s.runDir(projectID, runID), runFile))
	if err != nil {
		return domain.TestRun{}, fmt.Errorf("read run: %w", err)
	}
	var run domain.TestRun
	if err := json.Unmarshal(data, &run); err != nil {
		return domain.TestRun{}, fmt.Errorf("parse run: %w", err)
	}
	return run, nil
}

// List returns all runs for a project, newest first. Missing project → (nil, nil).
func (s *RunStore) List(projectID string) ([]domain.TestRun, error) {
	dir := filepath.Join(s.baseDir, projectID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read project runs: %w", err)
	}
	var runs []domain.TestRun
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		run, err := s.Get(projectID, e.Name())
		if err != nil {
			continue
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	return runs, nil
}
