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
	mu         sync.RWMutex
	projects   []domain.Project
	sitesPath  string
	localsPath string
	app        *application.App
}

func NewProjectService(sitesPath, localsPath string) *ProjectService {
	return &ProjectService{sitesPath: sitesPath, localsPath: localsPath}
}

// SetApp injects the Wails app reference (called after app creation).
func (s *ProjectService) SetApp(app *application.App) {
	s.app = app
}

// SiteProjectID is het project-ID van een WordPress-site zonder lokale map.
func SiteProjectID(siteID string) string { return "site:" + siteID }

// Reload bouwt de projectlijst uit het register: sites (met of zonder map) en
// lokale projecten. Items met een map houden het ID op basis van het pad, zodat
// opgeslagen historie (testruns, rapporten, mediascans) blijft aansluiten.
func (s *ProjectService) Reload() ([]domain.Project, error) {
	sites, err := config.LoadWPSites(s.sitesPath)
	if err != nil {
		return nil, err
	}
	locals, err := config.LoadLocalProjects(s.localsPath)
	if err != nil {
		return nil, err
	}
	found := make([]domain.Project, 0, len(sites)+len(locals))
	for _, site := range sites {
		found = append(found, s.projectForSite(site))
	}
	for _, lp := range locals {
		p, _ := s.loadProject(lp.Path)
		if lp.Name != "" {
			p.DisplayName = lp.Name
		}
		p.Local = true
		p.Missing = !dirExists(lp.Path)
		found = append(found, p)
	}
	s.mu.Lock()
	s.projects = found
	s.mu.Unlock()
	return found, nil
}

func (s *ProjectService) projectForSite(site domain.WPSite) domain.Project {
	var p domain.Project
	if site.ProjectPath != "" {
		p, _ = s.loadProject(site.ProjectPath)
		p.Local = true
		p.Missing = !dirExists(site.ProjectPath)
	} else {
		p = domain.Project{
			ID:         SiteProjectID(site.ID),
			Provider:   domain.ProviderNone,
			Deploy:     domain.DeployConf{Type: "wordpress", Link: domain.DeployLinks{Prod: site.URL}},
			LastScanAt: time.Now(),
		}
	}
	p.SiteID = site.ID
	p.DisplayName = site.Name
	if p.Deploy.Link.Prod == "" {
		p.Deploy.Link.Prod = site.URL
	}
	if site.KinstaSiteID != "" {
		// Eigen kopie: de config uit loadProject mag niet gemuteerd worden.
		k := domain.KinstaProjectCfg{}
		if p.Config.Kinsta != nil {
			k = *p.Config.Kinsta
		}
		k.SiteID = site.KinstaSiteID
		p.Config.Kinsta = &k
		p.Provider = domain.ProviderKinsta
	}
	return p
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// List returns the last scanned project list without re-scanning.
func (s *ProjectService) List() []domain.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.projects
}

// Get returns a project by ID from the last scan.
func (s *ProjectService) Get(id string) (domain.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.projects {
		if p.ID == id {
			return p, true
		}
	}
	return domain.Project{}, false
}

// UpdateProjectConfig replaces the Config of a project in-memory (used after saving .rdm/config.yml).
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
			if !p.Local || p.Missing {
				return p, nil
			}
			status, _ := gitread.Status(p.Path)
			s.projects[i].Git = status
			if deploy, err := config.LoadDeployConf(p.Path); err == nil {
				s.projects[i].Deploy = domain.DeployConf{
					Type: deploy.Type,
					Link: domain.DeployLinks{Test: deploy.Link.Test, Acc: deploy.Link.Acc, Prod: deploy.Link.Prod, Local: deploy.Link.Local},
					Vars: deploy.Vars,
				}
			}
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
			var status domain.GitStatus
			if p.Local && !p.Missing {
				status, _ = gitread.Status(p.Path)
			}
			dirty := len(status.Staged)+len(status.Unstaged)+len(status.Untracked) > 0
			summary := domain.ProjectStatusSummary{
				ProjectID:   p.ID,
				DisplayName: p.DisplayName,
				Branch:      status.Branch,
				Ahead:       status.Ahead,
				Behind:      status.Behind,
				Dirty:       dirty,
				IsRepo:      status.IsRepo,
				DeployType:  p.Deploy.Type,
				SiteID:      p.SiteID,
				Local:       p.Local,
				Missing:     p.Missing,
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
		Deploy:      domain.DeployConf{Type: deploy.Type, Link: domain.DeployLinks{Test: deploy.Link.Test, Acc: deploy.Link.Acc, Prod: deploy.Link.Prod, Local: deploy.Link.Local}, Vars: deploy.Vars},
		Git:         status,
		LastScanAt:  time.Now(),
	}, nil
}

// inferProvider maps a deploy_conf type to a Provider when .rdm/config.yml is absent.
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
