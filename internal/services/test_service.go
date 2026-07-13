package services

import (
	"context"
	"fmt"

	"github.com/rdm/sites-tool/internal/adapters/browser"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// browserRunner is the subset of *browser.Runner TestService needs (test seam).
type browserRunner interface {
	Run(ctx context.Context, req browser.RunRequest) (browser.RunResponse, error)
}

// visionClient is the subset of *claude.Client TestService needs (test seam).
type visionClient interface {
	Author(ctx context.Context, description string) ([]domain.Step, error)
	Compare(ctx context.Context, baselinePNG, updatePNG []byte, stepDesc string, highImpact bool) ([]domain.Finding, error)
	Heal(ctx context.Context, ariaSnapshot, target string) (string, error)
}

// TestService orchestrates AI visual test runs and flow management.
type TestService struct {
	projects  *ProjectService
	cfg       *config.Global
	store     *RunStore
	runner    browserRunner
	newVision func() (visionClient, error)
}

// NewTestService wires the service. newVision defaults to a real claude.Client
// built from the resolved Anthropic key; it is overridden in tests.
func NewTestService(projects *ProjectService, cfg *config.Global, store *RunStore, runner browserRunner) *TestService {
	s := &TestService{projects: projects, cfg: cfg, store: store, runner: runner}
	s.newVision = s.defaultVision
	return s
}

// defaultVision is a temporary stub replaced in Task 5 (Wails wiring).
func (s *TestService) defaultVision() (visionClient, error) {
	return nil, fmt.Errorf("vision client not wired")
}

func (s *TestService) project(id string) (domain.Project, error) {
	p, ok := s.projects.Get(id)
	if !ok {
		return domain.Project{}, fmt.Errorf("project %q niet gevonden", id)
	}
	return p, nil
}

// ListFlows returns the committed flows for a project.
func (s *TestService) ListFlows(projectID string) ([]domain.Flow, error) {
	p, err := s.project(projectID)
	if err != nil {
		return nil, err
	}
	return config.LoadFlows(p.Path)
}

// SaveFlows validates and writes the flows for a project.
func (s *TestService) SaveFlows(projectID string, flows []domain.Flow) error {
	p, err := s.project(projectID)
	if err != nil {
		return err
	}
	return config.SaveFlows(p.Path, flows)
}

// AuthorFlow turns a natural-language description into flow steps via Claude.
func (s *TestService) AuthorFlow(projectID, description string) ([]domain.Step, error) {
	if _, err := s.project(projectID); err != nil {
		return nil, err
	}
	v, err := s.newVision()
	if err != nil {
		return nil, err
	}
	return v.Author(context.Background(), description)
}

// ListRuns returns the run history for a project, newest first.
func (s *TestService) ListRuns(projectID string) ([]domain.TestRun, error) {
	return s.store.List(projectID)
}

// GetRun returns a single stored run.
func (s *TestService) GetRun(projectID, runID string) (domain.TestRun, error) {
	return s.store.Get(projectID, runID)
}
