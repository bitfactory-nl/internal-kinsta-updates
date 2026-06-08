package services

import (
	"sync"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	"github.com/rdm/sites-tool/internal/domain"
)

type fakeProjectLister struct {
	projects []domain.Project
}

func (f *fakeProjectLister) List() []domain.Project { return f.projects }

type fakeKinstaReader struct {
	configured bool
	details    map[string]*kinsta.SiteDetails        // siteID -> details
	plugins    map[string]*kinsta.EnvironmentDetails // envID -> plugins/themes
}

func (f *fakeKinstaReader) IsConfigured() bool { return f.configured }

func (f *fakeKinstaReader) GetSiteDetails(siteID string) (*kinsta.SiteDetails, error) {
	return f.details[siteID], nil
}

func (f *fakeKinstaReader) GetEnvironmentPluginsAndThemes(envID string) (*kinsta.EnvironmentDetails, error) {
	return f.plugins[envID], nil
}

type fakeNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *fakeNotifier) Send(title, message string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, title+"::"+message)
	return nil
}

func (n *fakeNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.messages)
}

func newVulnFixture(configured bool) (*VulnScanService, *fakeNotifier) {
	projects := &fakeProjectLister{
		projects: []domain.Project{
			{
				ID:          "proj-1",
				DisplayName: "Acme",
				Config:      domain.ProjectConfig{Kinsta: &domain.KinstaProjectCfg{SiteID: "site-1"}},
			},
			{
				ID:     "proj-2",
				Config: domain.ProjectConfig{}, // not Kinsta-linked → skipped
			},
		},
	}
	reader := &fakeKinstaReader{
		configured: configured,
		details: map[string]*kinsta.SiteDetails{
			"site-1": {
				Site:         kinsta.Site{ID: "site-1"},
				Environments: []kinsta.Environment{{ID: "env-1", Name: "live"}},
			},
		},
		plugins: map[string]*kinsta.EnvironmentDetails{
			"env-1": {
				Plugins: []kinsta.Plugin{
					{Name: "safe-plugin", Version: "1.0.0", IsVersionVulnerable: false},
					{Name: "vuln-plugin", Version: "2.1.0", IsVersionVulnerable: true},
				},
			},
		},
	}
	notify := &fakeNotifier{}
	svc := &VulnScanService{
		cfg:      nil,
		projects: projects,
		kinsta:   reader,
		notify:   notify,
		seen:     make(map[string]bool),
	}
	return svc, notify
}

func TestVulnScanFindsVulnerablePlugins(t *testing.T) {
	svc, notify := newVulnFixture(true)

	findings, err := svc.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Slug != "vuln-plugin" || f.Version != "2.1.0" || f.EnvName != "live" || f.ProjectName != "Acme" {
		t.Errorf("unexpected finding: %+v", f)
	}
	if notify.count() != 1 {
		t.Errorf("notifications = %d, want 1", notify.count())
	}
}

func TestVulnScanDedupesNotifications(t *testing.T) {
	svc, notify := newVulnFixture(true)

	if _, err := svc.Scan(); err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	// Second scan finds the same vuln; it must not notify again.
	findings, err := svc.Scan()
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(findings) != 1 {
		t.Errorf("second scan findings = %d, want 1 (still reported)", len(findings))
	}
	if notify.count() != 1 {
		t.Errorf("notifications after two scans = %d, want 1 (deduped)", notify.count())
	}
}

func TestVulnScanSkipsWhenNotConfigured(t *testing.T) {
	svc, notify := newVulnFixture(false)

	findings, err := svc.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %d, want 0 when Kinsta not configured", len(findings))
	}
	if notify.count() != 0 {
		t.Errorf("notifications = %d, want 0", notify.count())
	}
}
