package services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitread"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type ProjectService struct {
	mu       sync.RWMutex
	projects []domain.Project
	roots    []string
	app      *application.App
}

func NewProjectService(roots []string) *ProjectService {
	return &ProjectService{roots: roots}
}

// SetApp injects the Wails app reference (called after app creation).
func (s *ProjectService) SetApp(app *application.App) {
	s.app = app
}

// GetRoots returns the currently configured project roots.
func (s *ProjectService) GetRoots() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.roots))
	copy(out, s.roots)
	return out
}

// AddRoot opens a native folder picker, adds the chosen folder, persists it, and returns updated roots.
func (s *ProjectService) AddRoot() ([]string, error) {
	if s.app == nil {
		return nil, fmt.Errorf("app not initialized")
	}

	chosen, err := s.app.Dialog.OpenFile().
		SetTitle("Selecteer project folder").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(false).
		PromptForSingleSelection()
	if err != nil {
		return nil, fmt.Errorf("dialog: %w", err)
	}
	if chosen == "" {
		return s.GetRoots(), nil // user cancelled
	}

	s.mu.Lock()
	// Avoid duplicates
	for _, r := range s.roots {
		if r == chosen {
			s.mu.Unlock()
			return s.GetRoots(), nil
		}
	}
	s.roots = append(s.roots, chosen)
	roots := make([]string, len(s.roots))
	copy(roots, s.roots)
	s.mu.Unlock()

	if err := s.persistRoots(roots); err != nil {
		return roots, fmt.Errorf("save config: %w", err)
	}
	return roots, nil
}

// RemoveRoot removes a folder from the configured roots and persists.
func (s *ProjectService) RemoveRoot(path string) ([]string, error) {
	s.mu.Lock()
	filtered := s.roots[:0]
	for _, r := range s.roots {
		if r != path {
			filtered = append(filtered, r)
		}
	}
	s.roots = filtered
	roots := make([]string, len(s.roots))
	copy(roots, s.roots)
	s.mu.Unlock()

	if err := s.persistRoots(roots); err != nil {
		return roots, fmt.Errorf("save config: %w", err)
	}
	return roots, nil
}

func (s *ProjectService) persistRoots(roots []string) error {
	g, err := config.LoadGlobal()
	if err != nil {
		g = config.Global{}
	}
	g.ProjectsRoots = roots
	return config.SaveGlobal(g)
}

// Scan discovers all projects under configured roots and refreshes git status.
func (s *ProjectService) Scan() ([]domain.Project, error) {
	s.mu.RLock()
	roots := make([]string, len(s.roots))
	copy(roots, s.roots)
	s.mu.RUnlock()

	var found []domain.Project
	for _, root := range roots {
		entries, err := os.ReadDir(expandHome(root))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(expandHome(root), e.Name())
			p, err := s.loadProject(path)
			if err != nil {
				continue
			}
			found = append(found, p)
		}
	}

	s.mu.Lock()
	s.projects = found
	s.mu.Unlock()

	return found, nil
}

// List returns the last scanned project list without re-scanning.
func (s *ProjectService) List() []domain.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.projects
}

// UpdateProjectConfig replaces the Config of a project in-memory (used after saving .rdm.yml).
func (s *ProjectService) UpdateProjectConfig(id string, cfg domain.ProjectConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.projects {
		if p.ID == id {
			s.projects[i].Config = cfg
			return
		}
	}
}

// RefreshOne updates git status for a single project by ID.
func (s *ProjectService) RefreshOne(id string) (domain.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.projects {
		if p.ID == id {
			status, _ := gitread.Status(p.Path)
			s.projects[i].Git = status
			s.projects[i].LastScanAt = time.Now()
			return s.projects[i], nil
		}
	}
	return domain.Project{}, fmt.Errorf("project %q not found", id)
}

// BatchStatus returns a lightweight status summary for all projects (sidebar use).
func (s *ProjectService) BatchStatus() []domain.ProjectStatusSummary {
	s.mu.RLock()
	projects := make([]domain.Project, len(s.projects))
	copy(projects, s.projects)
	s.mu.RUnlock()

	results := make([]domain.ProjectStatusSummary, 0, len(projects))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range projects {
		wg.Add(1)
		go func(p domain.Project) {
			defer wg.Done()
			status, _ := gitread.Status(p.Path)
			dirty := len(status.Staged)+len(status.Unstaged)+len(status.Untracked) > 0
			summary := domain.ProjectStatusSummary{
				ProjectID:   p.ID,
				DisplayName: p.DisplayName,
				Branch:      status.Branch,
				Ahead:       status.Ahead,
				Behind:      status.Behind,
				Dirty:       dirty,
				IsRepo:      status.IsRepo,
			}
			mu.Lock()
			results = append(results, summary)
			mu.Unlock()
		}(p)
	}

	wg.Wait()
	return results
}

func (s *ProjectService) loadProject(path string) (domain.Project, error) {
	cfg, _ := config.LoadProject(path)
	deploy, _ := config.LoadDeployConf(path)

	name := cfg.DisplayName
	if name == "" {
		name = filepath.Base(path)
	}

	provider := cfg.Provider
	if provider == "" {
		provider = inferProvider(deploy.Type)
	}

	status, _ := gitread.Status(path)

	return domain.Project{
		ID:          projectID(path),
		Path:        path,
		DisplayName: name,
		Provider:    provider,
		Config:      cfg,
		Deploy:      domain.DeployConf{Type: deploy.Type, Link: domain.DeployLinks{Test: deploy.Link.Test, Acc: deploy.Link.Acc, Prod: deploy.Link.Prod}, Vars: deploy.Vars},
		Git:         status,
		LastScanAt:  time.Now(),
	}, nil
}

// inferProvider maps a deploy_conf type to a Provider when .rdm.yml is absent.
func inferProvider(deployType string) domain.Provider {
	switch deployType {
	case "wordpress_kinsta":
		return domain.ProviderKinsta
	case "wordpress_transip":
		return domain.ProviderVPS
	default:
		return domain.ProviderNone
	}
}

func projectID(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8])
}

func expandHome(path string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
