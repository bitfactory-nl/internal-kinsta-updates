package services

import (
	"context"
	"fmt"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

type KinstaService struct {
	cfg     *config.Global
	project *ProjectService
}

func NewKinstaService(cfg *config.Global, project *ProjectService) *KinstaService {
	return &KinstaService{cfg: cfg, project: project}
}

func (s *KinstaService) client() (*kinsta.Client, error) {
	apiKey, err := config.ResolveSecret(s.cfg.Kinsta.APIKey)
	if err != nil {
		return nil, fmt.Errorf("kinsta api key: %w", err)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("kinsta api key niet geconfigureerd")
	}
	return kinsta.NewClient(apiKey, s.cfg.Kinsta.CompanyID), nil
}

func (s *KinstaService) projectFor(id string) (*domain.Project, error) {
	for _, p := range s.project.List() {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("project not found: %s", id)
}

// IsConfigured returns true if the global Kinsta API key is set.
func (s *KinstaService) IsConfigured() bool {
	return s.cfg.Kinsta.APIKey != ""
}

// GetLinkedSiteID returns the Kinsta site_id stored in .rdm.yml for a project, or "" if not set.
func (s *KinstaService) GetLinkedSiteID(projectID string) (string, error) {
	p, err := s.projectFor(projectID)
	if err != nil {
		return "", err
	}
	if p.Config.Kinsta == nil {
		return "", nil
	}
	return p.Config.Kinsta.SiteID, nil
}

// ListSites returns all Kinsta sites for the configured company.
func (s *KinstaService) ListSites() ([]kinsta.Site, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return c.ListSites(ctx)
}

// LinkSite saves a Kinsta site_id to the project's .rdm.yml so it persists.
func (s *KinstaService) LinkSite(projectID, siteID string) error {
	p, err := s.projectFor(projectID)
	if err != nil {
		return err
	}
	cfg := p.Config
	if cfg.Kinsta == nil {
		cfg.Kinsta = &domain.KinstaProjectCfg{}
	}
	cfg.Kinsta.SiteID = siteID
	if err := config.SaveProject(p.Path, cfg); err != nil {
		return fmt.Errorf("opslaan .rdm.yml: %w", err)
	}
	s.project.UpdateProjectConfig(projectID, cfg)
	return nil
}

// GetSiteDetails fetches site info + full environment details (PHP, WP version, etc.).
func (s *KinstaService) GetSiteDetails(siteID string) (*kinsta.SiteDetails, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	site, err := c.GetSite(ctx, siteID)
	if err != nil {
		return nil, err
	}

	envs, err := c.GetEnvironments(ctx, siteID)
	if err != nil {
		envs = make([]kinsta.Environment, 0)
	}

	return &kinsta.SiteDetails{
		Site:         *site,
		Environments: envs,
	}, nil
}

// GetEnvironmentPluginsAndThemes returns plugins + themes for an environment.
// The environment details themselves are already included in the SiteDetails response.
func (s *KinstaService) GetEnvironmentPluginsAndThemes(envID string) (*kinsta.EnvironmentDetails, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	plugins, err := c.GetEnvironmentPlugins(ctx, envID)
	if err != nil {
		plugins = make([]kinsta.Plugin, 0)
	}

	themes, err := c.GetEnvironmentThemes(ctx, envID)
	if err != nil {
		themes = make([]kinsta.Theme, 0)
	}

	return &kinsta.EnvironmentDetails{
		Plugins: plugins,
		Themes:  themes,
	}, nil
}
