package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// initialScanDelay gives the app time to finish its first project scan before
// the first vulnerability sweep runs.
const initialScanDelay = 30 * time.Second

// defaultScanInterval is used when the configured interval is unset or invalid.
const defaultScanInterval = 60 * time.Minute

// VulnFinding is a single vulnerable plugin discovered on a Kinsta environment.
type VulnFinding struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	SiteID      string `json:"siteId"`
	EnvID       string `json:"envId"`
	EnvName     string `json:"envName"`
	Slug        string `json:"slug"`
	Version     string `json:"version"`
}

func (f VulnFinding) key() string {
	return f.ProjectID + "|" + f.EnvID + "|" + f.Slug + "|" + f.Version
}

// projectLister yields the currently known projects.
type projectLister interface {
	List() []domain.Project
}

// kinstaReader is the subset of KinstaService used to enumerate plugins.
type kinstaReader interface {
	IsConfigured() bool
	GetSiteDetails(siteID string) (*kinsta.SiteDetails, error)
	GetEnvironmentPluginsAndThemes(envID string) (*kinsta.EnvironmentDetails, error)
}

// notifier sends a desktop notification.
type notifier interface {
	Send(title, message string) error
}

// VulnScanService periodically checks Kinsta-linked projects for vulnerable
// plugins and notifies the user about newly discovered ones.
type VulnScanService struct {
	cfg      *config.Global
	projects projectLister
	kinsta   kinstaReader
	notify   notifier

	mu      sync.Mutex
	seen    map[string]bool
	stop    chan struct{}
	running bool
}

func NewVulnScanService(cfg *config.Global, projects *ProjectService, kinsta *KinstaService, notify *NotifyService) *VulnScanService {
	return &VulnScanService{
		cfg:      cfg,
		projects: projects,
		kinsta:   kinsta,
		notify:   notify,
		seen:     make(map[string]bool),
	}
}

// Start begins the background scan loop if vulnerability alerts are enabled.
// It is a no-op when already running or when alerts are disabled.
func (s *VulnScanService) Start() {
	if s.cfg == nil || !s.cfg.Notifications.EnableVulnerabilityAlerts {
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	stop := s.stop
	s.mu.Unlock()

	interval := time.Duration(s.cfg.Notifications.ScanIntervalMinutes) * time.Minute
	if interval < time.Minute {
		interval = defaultScanInterval
	}

	go func() {
		timer := time.NewTimer(initialScanDelay)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
				_, _ = s.Scan()
				timer.Reset(interval)
			}
		}
	}()
}

// Stop halts the background scan loop.
func (s *VulnScanService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		close(s.stop)
		s.running = false
	}
}

// Scan inspects every Kinsta-linked project's environments for vulnerable
// plugins, sends a notification for each newly seen one, and returns all
// findings from this sweep. Safe to call manually (e.g. a "scan now" button).
func (s *VulnScanService) Scan() ([]VulnFinding, error) {
	if s.kinsta == nil || !s.kinsta.IsConfigured() {
		return nil, nil
	}

	findings := make([]VulnFinding, 0)
	for _, p := range s.projects.List() {
		if p.Config.Kinsta == nil || p.Config.Kinsta.SiteID == "" {
			continue
		}
		siteID := p.Config.Kinsta.SiteID

		details, err := s.kinsta.GetSiteDetails(siteID)
		if err != nil || details == nil {
			continue
		}

		for _, env := range details.Environments {
			ed, err := s.kinsta.GetEnvironmentPluginsAndThemes(env.ID)
			if err != nil || ed == nil {
				continue
			}
			for _, pl := range ed.Plugins {
				if !pl.IsVersionVulnerable {
					continue
				}
				findings = append(findings, VulnFinding{
					ProjectID:   p.ID,
					ProjectName: displayName(p),
					SiteID:      siteID,
					EnvID:       env.ID,
					EnvName:     env.Name,
					Slug:        pl.Name,
					Version:     pl.Version,
				})
			}
		}
	}

	s.notifyNew(findings)
	return findings, nil
}

// notifyNew sends a notification for each finding not seen before, recording
// it so repeated scans don't re-notify. Notifications are sent outside the lock.
func (s *VulnScanService) notifyNew(findings []VulnFinding) {
	s.mu.Lock()
	fresh := make([]VulnFinding, 0)
	for _, f := range findings {
		k := f.key()
		if s.seen[k] {
			continue
		}
		s.seen[k] = true
		fresh = append(fresh, f)
	}
	s.mu.Unlock()

	for _, f := range fresh {
		title := "Kwetsbare plugin: " + f.Slug
		msg := fmt.Sprintf("%s (%s) — %s %s", f.ProjectName, f.EnvName, f.Slug, f.Version)
		_ = s.notify.Send(title, msg)
	}
}

func displayName(p domain.Project) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.ID
}
