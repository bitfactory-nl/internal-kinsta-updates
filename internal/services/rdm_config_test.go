package services

import (
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// siteMetKinstaFixture: een site met een eigen Kinsta-koppeling (k-1) en een
// map zonder kinsta-blok in .rdm/config.yml.
func siteMetKinstaFixture(t *testing.T) (*ProjectService, string) {
	t.Helper()
	dir := t.TempDir()
	web := filepath.Join(dir, "web-k")
	if err := config.SaveProject(web, domain.ProjectConfig{DisplayName: "Web K"}); err != nil {
		t.Fatal(err)
	}
	sites := filepath.Join(dir, "wp-sites.yml")
	if err := config.SaveWPSites(sites, []domain.WPSite{
		{ID: "web-k", Name: "Web K", URL: "https://k.nl", Username: "u", ProjectPath: web, KinstaSiteID: "k-1", KinstaEnvID: "e-2"},
	}); err != nil {
		t.Fatal(err)
	}
	ps := NewProjectService(sites, filepath.Join(dir, "local-projects.yml"))
	if _, err := ps.Reload(); err != nil {
		t.Fatal(err)
	}
	return ps, web
}

// De Kinsta-koppeling van de site is een override in het geheugen; opslaan van
// iets anders in .rdm/config.yml mag die niet naar schijf schrijven.
func TestSaveSSHAccessSchrijftSiteKinstaNietInRdm(t *testing.T) {
	ps, web := siteMetKinstaFixture(t)
	svc := NewMediaService(ps, nil, NewMediaScanStore(t.TempDir()))
	svc.secrets = &fakeSecrets{}
	if p, _ := ps.Get(projectID(web)); p.Config.Kinsta == nil || p.Config.Kinsta.SiteID != "k-1" {
		t.Fatalf("override ontbreekt in het geheugen: %+v", p.Config.Kinsta)
	}
	if err := svc.SaveSSHAccess(projectID(web), "steinweg", "/www/k/public", ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadProject(web)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kinsta != nil && cfg.Kinsta.SiteID != "" {
		t.Fatalf("kinsta site_id van de site in .rdm: %+v", cfg.Kinsta)
	}
	if cfg.SSH == nil || cfg.SSH.User != "steinweg" {
		t.Fatalf("ssh niet opgeslagen: %+v", cfg.SSH)
	}
	if p, _ := ps.Get(projectID(web)); p.Config.Kinsta == nil || p.Config.Kinsta.SiteID != "k-1" {
		t.Fatalf("override kwijt in het geheugen: %+v", p.Config.Kinsta)
	}
}
