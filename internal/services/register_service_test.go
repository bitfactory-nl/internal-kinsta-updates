package services

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
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
