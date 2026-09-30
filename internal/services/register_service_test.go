package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

func regFixture(t *testing.T) (*RegisterService, *ProjectService, *WPSiteService, string, string, string) {
	t.Helper()
	ps, wpDir, laravelDir, sitesPath, localsPath := registerFixture(t)
	if _, err := ps.Reload(); err != nil {
		t.Fatal(err)
	}
	wp := NewWPSiteService(sitesPath, ps, &wpNepSecrets{m: map[string]string{}})
	return NewRegisterService(ps, wp, localsPath), ps, wp, wpDir, laravelDir, localsPath
}

func TestAddLocalProjectPathEnReload(t *testing.T) {
	rs, ps, _, _, _, _ := regFixture(t)
	nieuw := t.TempDir()
	p, err := rs.AddLocalProjectPath(nieuw, "Pimcore")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != projectID(nieuw) || !p.Local || p.DisplayName != "Pimcore" {
		t.Fatalf("project %+v", p)
	}
	if _, ok := ps.Get(projectID(nieuw)); !ok {
		t.Fatal("niet herladen")
	}
}

func TestAddLocalProjectPathWeigertSitePad(t *testing.T) {
	rs, _, _, wpDir, _, _ := regFixture(t)
	_, err := rs.AddLocalProjectPath(wpDir, "")
	if err == nil || !strings.Contains(err.Error(), "staat al in het register") {
		t.Fatalf("fout %v", err)
	}
}

func TestRemoveLocalProjectWeigertSite(t *testing.T) {
	rs, _, _, wpDir, _, _ := regFixture(t)
	err := rs.RemoveLocalProject(projectID(wpDir))
	if err == nil || err.Error() != "dit is een WordPress-site; ontkoppel of verwijder de site" {
		t.Fatalf("fout %v", err)
	}
	err = rs.RemoveLocalProject("site:depot")
	if err == nil || err.Error() != "dit is een WordPress-site; ontkoppel of verwijder de site" {
		t.Fatalf("fout %v", err)
	}
}

func TestRemoveLocalProject(t *testing.T) {
	rs, ps, _, _, laravelDir, localsPath := regFixture(t)
	if err := rs.RemoveLocalProject(projectID(laravelDir)); err != nil {
		t.Fatal(err)
	}
	locals, _ := config.LoadLocalProjects(localsPath)
	if len(locals) != 0 {
		t.Fatalf("nog %+v", locals)
	}
	if _, ok := ps.Get(projectID(laravelDir)); ok {
		t.Fatal("project staat nog in de lijst")
	}
}

func TestLinkSiteFolderNeemtLokaalProjectOver(t *testing.T) {
	rs, ps, wp, _, laravelDir, localsPath := regFixture(t)
	if err := rs.LinkSiteFolder("depot", laravelDir); err != nil {
		t.Fatal(err)
	}
	site, _ := wp.find("depot")
	if filepath.Clean(site.ProjectPath) != filepath.Clean(laravelDir) {
		t.Fatalf("project_path %q", site.ProjectPath)
	}
	locals, _ := config.LoadLocalProjects(localsPath)
	if len(locals) != 0 {
		t.Fatalf("lokaal project niet overgenomen: %+v", locals)
	}
	p, ok := ps.Get(projectID(laravelDir))
	if !ok || p.SiteID != "depot" {
		t.Fatalf("project %+v ok=%v", p, ok)
	}
	if _, ok := ps.Get("site:depot"); ok {
		t.Fatal("site:depot hoort niet meer te bestaan")
	}
}

func TestLinkSiteFolderWeigertMapVanAndereSite(t *testing.T) {
	rs, _, _, wpDir, _, _ := regFixture(t)
	err := rs.LinkSiteFolder("depot", wpDir)
	if err == nil || !strings.Contains(err.Error(), "staat al in het register") {
		t.Fatalf("fout %v", err)
	}
}

func TestForProjectViaSiteID(t *testing.T) {
	_, _, wp, _, _, _ := regFixture(t)
	v, err := wp.ForProject("site:depot")
	if err != nil || v == nil || v.Site.ID != "depot" {
		t.Fatalf("v %+v err %v", v, err)
	}
}

func TestSuggestForProjectVraagtLokaleMap(t *testing.T) {
	_, _, wp, _, _, _ := regFixture(t)
	if _, err := wp.SuggestForProject("site:depot"); err == nil {
		t.Fatal("verwacht fout voor project zonder map")
	}
	if b, err := wp.DetectBridge("site:depot"); err != nil || b {
		t.Fatalf("bridge %v err %v", b, err)
	}
}

func TestPadValidatieLegEnRelatief(t *testing.T) {
	rs, _, _, _, _, _ := regFixture(t)
	for _, pad := range []string{"", "  ", "relatief/pad", "."} {
		if _, err := rs.AddLocalProjectPath(pad, ""); err == nil {
			t.Fatalf("AddLocalProjectPath(%q) hoort te falen", pad)
		}
		if err := rs.LinkSiteFolder("depot", pad); err == nil {
			t.Fatalf("LinkSiteFolder(%q) hoort te falen", pad)
		}
	}
	if err := rs.LinkSiteFolder("depot", ""); err == nil || err.Error() != "kies een map" {
		t.Fatalf("fout %v", err)
	}
	if err := rs.LinkSiteFolder("depot", "rel"); err == nil || err.Error() != "pad moet absoluut zijn" {
		t.Fatalf("fout %v", err)
	}
}

func TestAddLocalProjectPathTweeKeerWeigert(t *testing.T) {
	rs, _, _, _, _, _ := regFixture(t)
	nieuw := t.TempDir()
	if _, err := rs.AddLocalProjectPath(nieuw, ""); err != nil {
		t.Fatal(err)
	}
	_, err := rs.AddLocalProjectPath(nieuw, "")
	if err == nil || !strings.Contains(err.Error(), "staat al in het register") {
		t.Fatalf("fout %v", err)
	}
}

func TestLinkSiteFolderBehoudtWachtwoordEnBridge(t *testing.T) {
	ps, _, laravelDir, sitesPath, localsPath := registerFixture(t)
	if _, err := ps.Reload(); err != nil {
		t.Fatal(err)
	}
	sec := &wpNepSecrets{m: map[string]string{config.WPSecretAccount("depot"): "geheim"}}
	wp := NewWPSiteService(sitesPath, ps, sec)
	if _, err := wp.mutate("depot", func(w *domain.WPSite) { w.Bridge = true }); err != nil {
		t.Fatal(err)
	}
	rs := NewRegisterService(ps, wp, localsPath)
	if err := rs.LinkSiteFolder("depot", laravelDir); err != nil {
		t.Fatal(err)
	}
	site, _ := wp.find("depot")
	if !site.Bridge {
		t.Fatal("bridge-vlag kwijt")
	}
	if pw, _ := sec.Get(config.WPSecretAccount("depot")); pw != "geheim" {
		t.Fatal("wachtwoord kwijt")
	}
	// Beide bestanden consistent: pad staat niet meer in local-projects.yml.
	locals, _ := config.LoadLocalProjects(localsPath)
	sites, _ := config.LoadWPSites(sitesPath)
	if err := config.ValidateRegister(sites, locals); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(laravelDir); err != nil {
		t.Fatal("map hoort te blijven")
	}
}

func TestConvertLocalToSiteWeigertSite(t *testing.T) {
	rs, _, _, wpDir, laravelDir, _ := regFixture(t)
	if _, err := rs.ConvertLocalToSite(projectID(wpDir)); err == nil {
		t.Fatal("een site hoort geen voorstel te krijgen")
	}
	if _, err := rs.ConvertLocalToSite(projectID(laravelDir)); err != nil {
		t.Fatal(err)
	}
}
