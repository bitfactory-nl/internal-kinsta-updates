package services

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
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

// SiteLinkConflict reports a Kinsta site that is linked to more than one project.
type SiteLinkConflict struct {
	SiteID   string   `json:"siteId"`
	Projects []string `json:"projects"`
}

// SiteLinkConflicts lists Kinsta sites linked to more than one project. Such a
// duplicate is never harmless: both projects then read the same site's PHP
// version, plugin list and SSH details, so at least one shows another customer's
// data — including in generated reports.
func (s *KinstaService) SiteLinkConflicts() []SiteLinkConflict {
	perSite := map[string][]string{}
	for _, p := range s.project.List() {
		if p.Config.Kinsta == nil || p.Config.Kinsta.SiteID == "" {
			continue
		}
		id := p.Config.Kinsta.SiteID
		perSite[id] = append(perSite[id], p.DisplayName)
	}

	out := make([]SiteLinkConflict, 0)
	for id, names := range perSite {
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		out = append(out, SiteLinkConflict{SiteID: id, Projects: names})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SiteID < out[j].SiteID })
	return out
}

// projectWithSite returns the display name of another project already linked to
// siteID, or "" when the site is free.
func (s *KinstaService) projectWithSite(exceptProjectID, siteID string) string {
	for _, p := range s.project.List() {
		if p.ID == exceptProjectID || p.Config.Kinsta == nil {
			continue
		}
		if p.Config.Kinsta.SiteID == siteID {
			return p.DisplayName
		}
	}
	return ""
}

// LinkSite saves a Kinsta site_id to the project's .rdm.yml so it persists.
// Linking a site that already belongs to another project is refused: that is how
// projects end up reading each other's data.
func (s *KinstaService) LinkSite(projectID, siteID string) error {
	p, err := s.projectFor(projectID)
	if err != nil {
		return err
	}
	if other := s.projectWithSite(projectID, siteID); other != "" {
		return fmt.Errorf("deze Kinsta-site is al gekoppeld aan %s; maak die koppeling eerst los", other)
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

// EnvSSHEndpoint is one environment's SSH endpoint as Kinsta reports it. The API
// has no username field, so that has to come from the project's own config.
type EnvSSHEndpoint struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	EnvName string `json:"envName"`
}

// EnvironmentSSH resolves the SSH endpoint of one environment of the project's
// linked Kinsta site. An empty envID picks the live environment, or the only one
// when the site has just a single environment.
func (s *KinstaService) EnvironmentSSH(projectID, envID string) (EnvSSHEndpoint, error) {
	siteID, err := s.GetLinkedSiteID(projectID)
	if err != nil {
		return EnvSSHEndpoint{}, err
	}
	if siteID == "" {
		return EnvSSHEndpoint{}, fmt.Errorf("dit project is nog niet aan een Kinsta-site gekoppeld")
	}
	details, err := s.GetSiteDetails(siteID)
	if err != nil {
		return EnvSSHEndpoint{}, err
	}
	if len(details.Environments) == 0 {
		return EnvSSHEndpoint{}, fmt.Errorf("geen omgevingen gevonden voor deze Kinsta-site")
	}

	env := details.Environments[0]
	if envID != "" {
		gevonden := false
		for _, e := range details.Environments {
			if e.ID == envID {
				env, gevonden = e, true
				break
			}
		}
		if !gevonden {
			return EnvSSHEndpoint{}, fmt.Errorf("omgeving %q niet gevonden", envID)
		}
	} else {
		for _, e := range details.Environments {
			if e.Name == "live" {
				env = e
				break
			}
		}
	}

	port, err := strconv.Atoi(strings.TrimSpace(env.SSHConnection.SSHPort))
	if err != nil || port == 0 {
		return EnvSSHEndpoint{}, fmt.Errorf("Kinsta gaf geen SSH-poort voor omgeving %q", env.Name)
	}
	return EnvSSHEndpoint{
		Host:    strings.TrimSpace(env.SSHConnection.SSHIP.ExternalIP),
		Port:    port,
		EnvName: env.Name,
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
