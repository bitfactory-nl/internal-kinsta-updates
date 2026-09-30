package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

type wpNepSecrets struct{ m map[string]string }

func (n *wpNepSecrets) Set(a, v string) error { n.m[a] = v; return nil }
func (n *wpNepSecrets) Get(a string) (string, error) {
	if v, ok := n.m[a]; ok {
		return v, nil
	}
	return "", errors.New("niet gevonden")
}
func (n *wpNepSecrets) Delete(a string) error { delete(n.m, a); return nil }

type wpNepProjecten map[string]domain.Project

func (n wpNepProjecten) Get(id string) (domain.Project, bool) { p, ok := n[id]; return p, ok }

func (n wpNepProjecten) List() []domain.Project {
	out := make([]domain.Project, 0, len(n))
	for _, p := range n {
		out = append(out, p)
	}
	return out
}

func (n wpNepProjecten) Reload() ([]domain.Project, error) { return n.List(), nil }

func nieuweWPSiteService(t *testing.T) (*WPSiteService, *wpNepSecrets, string) {
	t.Helper()
	dir := t.TempDir()
	projDir := filepath.Join(dir, "web-ekcoatingsnl")
	if err := os.MkdirAll(filepath.Join(projDir, "public", "wp-content", "mu-plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := domain.Project{ID: "p1", Path: projDir, Local: true, DisplayName: "web-ekcoatingsnl"}
	p.Deploy.Link.Prod = "https://www.ekcoating.nl"
	sec := &wpNepSecrets{m: map[string]string{}}
	svc := NewWPSiteService(filepath.Join(dir, "wp-sites.yml"), filepath.Join(dir, "local-projects.yml"), wpNepProjecten{"p1": p}, sec)
	return svc, sec, projDir
}

func testWPSite(id, projectPath string) domain.WPSite {
	return domain.WPSite{ID: id, Name: id, URL: "https://" + id + ".nl", Username: "mcp-bot", ProjectPath: projectPath}
}

func TestWPSiteCreateBewaartSecretApart(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	v, err := svc.Create(testWPSite("depot", ""), "pw-1")
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasPassword || sec.m["wp:depot"] != "pw-1" {
		t.Fatalf("secret niet in Keychain: %+v %v", v, sec.m)
	}
	list, _ := svc.List()
	if len(list) != 1 || list[0].Site.ID != "depot" {
		t.Fatalf("list: %+v", list)
	}
}

func TestWPSiteCreateWeigertDubbeleID(t *testing.T) {
	svc, _, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("depot", ""), "pw")
	if _, err := svc.Create(testWPSite("depot", ""), "pw"); err == nil {
		t.Fatal("verwacht fout bij dubbele id")
	}
}

func TestWPSiteCreateOngeldigSchrijftGeenSecret(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	s := testWPSite("depot", "")
	s.URL = "http://depot.nl"
	if _, err := svc.Create(s, "pw"); err == nil {
		t.Fatal("verwacht validatiefout")
	}
	if len(sec.m) != 0 {
		t.Fatalf("secret achtergebleven: %v", sec.m)
	}
}

func TestWPSiteUpdateLeegWachtwoordHoudtBestaand(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("depot", ""), "pw-1")
	s := testWPSite("depot", "")
	s.Name = "Depot nieuw"
	if _, err := svc.Update(s, ""); err != nil {
		t.Fatal(err)
	}
	if sec.m["wp:depot"] != "pw-1" {
		t.Fatalf("wachtwoord overschreven: %v", sec.m)
	}
	if _, err := svc.Update(testWPSite("bestaat-niet", ""), ""); err == nil {
		t.Fatal("verwacht fout voor onbekende id")
	}
}

func TestWPSiteForProjectEnUnlinkEnDelete(t *testing.T) {
	svc, sec, projDir := nieuweWPSiteService(t)
	if v, _ := svc.ForProject("p1"); v != nil {
		t.Fatal("verwacht nil zonder koppeling")
	}
	svc.Create(testWPSite("ekcoating", projDir), "pw")
	v, err := svc.ForProject("p1")
	if err != nil || v == nil || v.Site.ID != "ekcoating" {
		t.Fatalf("ForProject: %+v %v", v, err)
	}
	if err := svc.Unlink("ekcoating"); err != nil {
		t.Fatal(err)
	}
	if v, _ := svc.ForProject("p1"); v != nil {
		t.Fatal("koppeling niet verwijderd")
	}
	list, _ := svc.List()
	if len(list) != 1 || list[0].Site.ProjectPath != "" {
		t.Fatalf("ontkoppelen moet een losse site overhouden: %+v", list)
	}
	if err := svc.Delete("ekcoating"); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.m["wp:ekcoating"]; ok {
		t.Fatal("Keychain-item niet gewist")
	}
}

func TestWPSiteProjectMissing(t *testing.T) {
	svc, _, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("weg", "/bestaat/niet"), "pw")
	list, _ := svc.List()
	if !list[0].ProjectMissing {
		t.Fatal("ProjectMissing moet true zijn")
	}
}

func TestWPSiteSuggestForProject(t *testing.T) {
	svc, _, projDir := nieuweWPSiteService(t)
	s, err := svc.SuggestForProject("p1")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.WPSite{ID: "web-ekcoatingsnl", Name: "web-ekcoatingsnl", URL: "https://www.ekcoating.nl",
		Username: "mcp-bot", ProjectPath: projDir}
	if s.ID != want.ID || s.Name != want.Name || s.URL != want.URL || s.Username != want.Username || s.ProjectPath != want.ProjectPath {
		t.Fatalf("got %+v, want %+v", s, want)
	}
}

func TestWPSiteDetectBridge(t *testing.T) {
	svc, _, projDir := nieuweWPSiteService(t)
	mu := filepath.Join(projDir, "public", "wp-content", "mu-plugins")
	if ok, _ := svc.DetectBridge("p1"); ok {
		t.Fatal("geen bridge verwacht")
	}
	os.WriteFile(filepath.Join(mu, "nb-mcp-bridge.php"), []byte("<?php"), 0o644)
	if ok, _ := svc.DetectBridge("p1"); !ok {
		t.Fatal("bridge-bestand niet gevonden")
	}
	os.Remove(filepath.Join(mu, "nb-mcp-bridge.php"))
	os.MkdirAll(filepath.Join(mu, "nb-mcp-bridge"), 0o755)
	if ok, _ := svc.DetectBridge("p1"); !ok {
		t.Fatal("bridge-map niet gevonden")
	}
	if _, err := svc.DetectBridge("onbekend"); err == nil {
		t.Fatal("verwacht fout voor onbekend project")
	}
}

func TestWPSiteStats(t *testing.T) {
	svc, sec, _ := nieuweWPSiteService(t)
	svc.Create(testWPSite("aa", ""), "pw")
	svc.Create(testWPSite("bb", ""), "pw")
	delete(sec.m, "wp:bb")
	total, zonder, err := svc.Stats()
	if err != nil || total != 2 || zonder != 1 {
		t.Fatalf("Stats = %d, %d, %v", total, zonder, err)
	}
}

func TestWPSiteCreateWeigertTweedeSiteOpZelfdeProject(t *testing.T) {
	svc, sec, projDir := nieuweWPSiteService(t)
	if _, err := svc.Create(testWPSite("een", projDir), "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(testWPSite("twee", projDir), "pw"); err == nil {
		t.Fatal("verwacht fout bij tweede site op zelfde project")
	}
	if _, ok := sec.m["wp:twee"]; ok {
		t.Fatal("secret van geweigerde site achtergebleven")
	}
}

func TestWPSiteUpdateOngeldigeURLHoudtOudeSite(t *testing.T) {
	svc, _, _ := nieuweWPSiteService(t)
	if _, err := svc.Create(testWPSite("depot", ""), "pw"); err != nil {
		t.Fatal(err)
	}
	s := testWPSite("depot", "")
	s.URL = "http://extern.nl"
	if _, err := svc.Update(s, ""); err == nil {
		t.Fatal("verwacht validatiefout")
	}
	list, _ := svc.List()
	if len(list) != 1 || list[0].Site.URL != "https://depot.nl" {
		t.Fatalf("oude site niet behouden: %+v", list)
	}
}

func TestWPSiteUpdateWeigertProjectBotsing(t *testing.T) {
	svc, _, projDir := nieuweWPSiteService(t)
	if _, err := svc.Create(testWPSite("een", projDir), "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(testWPSite("twee", ""), "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(testWPSite("twee", projDir), ""); err == nil {
		t.Fatal("verwacht fout bij botsend project")
	}
}

func TestWPSiteUnlinkOnbekendeID(t *testing.T) {
	svc, _, _ := nieuweWPSiteService(t)
	if err := svc.Unlink("bestaat-niet"); err == nil {
		t.Fatal("verwacht fout voor onbekende id")
	}
}

func TestWPSiteUpdateBehoudtBridge(t *testing.T) {
	svc, _, _ := nieuweWPSiteService(t)
	if _, err := svc.Create(testWPSite("depot", ""), "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.mutate("depot", func(w *domain.WPSite) { w.Bridge = true }); err != nil {
		t.Fatal(err)
	}
	s := testWPSite("depot", "")
	s.Name = "Depot nieuw"
	s.Bridge = false
	v, err := svc.Update(s, "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := svc.find("depot")
	if !got.Bridge || !v.Site.Bridge {
		t.Fatalf("bridge-vlag verloren: opgeslagen %+v, view %+v", got, v.Site)
	}
	if got.Name != "Depot nieuw" {
		t.Fatalf("naam niet gewijzigd: %+v", got)
	}
}

func TestWPSiteSuggestForProjectBridge(t *testing.T) {
	svc, _, projDir := nieuweWPSiteService(t)
	if s, _ := svc.SuggestForProject("p1"); s.Bridge {
		t.Fatal("zonder bridge-bestand hoort Bridge false te zijn")
	}
	mu := filepath.Join(projDir, "public", "wp-content", "mu-plugins")
	if err := os.WriteFile(filepath.Join(mu, "nb-mcp-bridge.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := svc.SuggestForProject("p1"); !s.Bridge {
		t.Fatal("met bridge-bestand hoort Bridge true te zijn")
	}
}
