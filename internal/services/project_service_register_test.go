package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

func registerFixture(t *testing.T) (*ProjectService, string, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	wpDir := filepath.Join(dir, "web-a")
	laravelDir := filepath.Join(dir, "web-laravel")
	os.MkdirAll(wpDir, 0o755)
	os.MkdirAll(laravelDir, 0o755)
	os.WriteFile(filepath.Join(wpDir, "deploy_conf.json"), []byte(`{"type":"wordpress_kinsta","link":{"prod":"https://a.nl"}}`), 0o644)
	sites := filepath.Join(dir, "wp-sites.yml")
	locals := filepath.Join(dir, "local-projects.yml")
	if err := config.SaveWPSites(sites, []domain.WPSite{
		{ID: "site-a", Name: "Site A", URL: "https://a.nl", Username: "u", ProjectPath: wpDir, KinstaSiteID: "k-123"},
		{ID: "depot", Name: "Depot", URL: "https://depot.nl", Username: "u", KinstaSiteID: "k-9"},
		{ID: "weg", Name: "Weg", URL: "https://weg.nl", Username: "u", ProjectPath: filepath.Join(dir, "bestaat-niet")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveLocalProjects(locals, []domain.LocalProject{{Path: laravelDir, Name: "Laravel"}}); err != nil {
		t.Fatal(err)
	}
	return NewProjectService(sites, locals), wpDir, laravelDir, sites, locals
}

func TestReloadBouwtDrieSoorten(t *testing.T) {
	svc, wpDir, laravelDir, _, _ := registerFixture(t)
	ps, err := svc.Reload()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.Project{}
	for _, p := range ps {
		byID[p.ID] = p
	}
	a := byID[projectID(wpDir)]
	if !a.Local || a.Missing || a.SiteID != "site-a" || a.Path != wpDir || a.DisplayName != "Site A" {
		t.Fatalf("site met map: %+v", a)
	}
	if a.Config.Kinsta == nil || a.Config.Kinsta.SiteID != "k-123" {
		t.Fatalf("kinsta-koppeling van de site ontbreekt: %+v", a.Config.Kinsta)
	}
	d := byID["site:depot"]
	if d.Local || d.Path != "" || d.SiteID != "depot" || d.Deploy.Link.Prod != "https://depot.nl" || d.Config.Kinsta == nil || d.Config.Kinsta.SiteID != "k-9" {
		t.Fatalf("site zonder map: %+v", d)
	}
	if d.Provider != domain.ProviderKinsta {
		t.Fatalf("provider %q", d.Provider)
	}
	l := byID[projectID(laravelDir)]
	if !l.Local || l.SiteID != "" || l.DisplayName != "Laravel" {
		t.Fatalf("lokaal project: %+v", l)
	}
	var weg domain.Project
	for _, p := range ps {
		if p.SiteID == "weg" {
			weg = p
		}
	}
	if !weg.Local || !weg.Missing {
		t.Fatalf("verdwenen map: %+v", weg)
	}
	if len(ps) != 4 {
		t.Fatalf("aantal %d", len(ps))
	}
}

func TestReloadKinstaSiteGaatVoorRdm(t *testing.T) {
	svc, wpDir, _, _, _ := registerFixture(t)
	os.MkdirAll(filepath.Join(wpDir, ".rdm"), 0o755)
	os.WriteFile(filepath.Join(wpDir, ".rdm", "config.yml"), []byte("kinsta:\n  site_id: oud\n"), 0o644)
	ps, _ := svc.Reload()
	for _, p := range ps {
		if p.SiteID == "site-a" && p.Config.Kinsta.SiteID != "k-123" {
			t.Fatalf("site-veld moet voorgaan: %q", p.Config.Kinsta.SiteID)
		}
	}
}

func TestBatchStatusMarkeertLocal(t *testing.T) {
	svc, _, _, _, _ := registerFixture(t)
	svc.Reload()
	for _, s := range svc.BatchStatus() {
		if s.ProjectID == "site:depot" && (s.Local || s.SiteID != "depot") {
			t.Fatalf("depot-summary %+v", s)
		}
	}
}

func TestSiteProjectID(t *testing.T) {
	if SiteProjectID("x") != "site:x" {
		t.Fatal()
	}
}

// newProjectServiceWith geeft een ProjectService zonder register met een vaste
// projectlijst, voor tests die projecten direct injecteren.
func newProjectServiceWith(ps []domain.Project) *ProjectService {
	s := NewProjectService("", "")
	s.projects = ps
	return s
}

// newProjectServiceForRoot registreert elke submap van root als lokaal project
// en laadt het register, zodat tests die vroeger een root scanden dezelfde
// projectlijst krijgen.
func newProjectServiceForRoot(t *testing.T, root string) *ProjectService {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var locals []domain.LocalProject
	for _, e := range entries {
		if e.IsDir() {
			locals = append(locals, domain.LocalProject{Path: filepath.Join(root, e.Name())})
		}
	}
	dir := t.TempDir()
	localsPath := filepath.Join(dir, "local-projects.yml")
	if err := config.SaveLocalProjects(localsPath, locals); err != nil {
		t.Fatal(err)
	}
	ps := NewProjectService(filepath.Join(dir, "wp-sites.yml"), localsPath)
	if _, err := ps.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	return ps
}
