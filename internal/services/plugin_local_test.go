package services

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

// bouwPluginZip maakt in-memory een wp.org-vormige zip: één topmap <slug>/ met
// daarin een hoofdbestand met de WordPress-pluginheader.
func bouwPluginZip(t *testing.T, slug, version string, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	schrijf := func(naam, inhoud string) {
		w, err := zw.Create(naam)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(inhoud)); err != nil {
			t.Fatal(err)
		}
	}
	schrijf(slug+"/"+slug+".php",
		"<?php\n/**\n * Plugin Name: "+slug+"\n * Version: "+version+"\n */\n")
	schrijf(slug+"/readme.txt", "Stable tag: "+version+"\n")
	for naam, inhoud := range extra {
		schrijf(naam, inhoud)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// lokaleMapMet legt zips neer in een tijdelijke map en geeft het pad terug.
func lokaleMapMet(t *testing.T, zips map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for naam, data := range zips {
		if err := os.WriteFile(filepath.Join(dir, naam), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLeesPluginZipInfo(t *testing.T) {
	slug, versie, err := leesPluginZipInfo(bouwPluginZip(t, "acf-pro", "6.4.1", nil))
	if err != nil {
		t.Fatalf("leesPluginZipInfo: %v", err)
	}
	if slug != "acf-pro" || versie != "6.4.1" {
		t.Errorf("slug=%q versie=%q, wil acf-pro 6.4.1", slug, versie)
	}
}

func TestLeesPluginZipInfoZonderHeader(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("map/bestand.txt")
	w.Write([]byte("geen plugin"))
	zw.Close()

	if _, _, err := leesPluginZipInfo(buf.Bytes()); err == nil {
		t.Error("wil een fout voor een zip zonder pluginheader")
	}
}

func TestListLocalPaidPlugins(t *testing.T) {
	dir := lokaleMapMet(t, map[string][]byte{
		"acf-pro.zip":      bouwPluginZip(t, "acf-pro", "6.4.1", nil),
		"gravityforms.zip": bouwPluginZip(t, "gravityforms", "2.9.3", nil),
		"kapot.zip":        []byte("dit is geen zip"),
		"notitie.txt":      []byte("negeer mij"),
	})
	svc := &PluginService{cfg: &config.Global{}}
	svc.cfg.PluginRepo.LocalDir = dir

	lijst, err := svc.ListLocalPaidPlugins()
	if err != nil {
		t.Fatalf("ListLocalPaidPlugins: %v", err)
	}
	if len(lijst) != 3 {
		t.Fatalf("lijst = %+v, wil 3 regels (2 goed + 1 kapot, txt genegeerd)", lijst)
	}
	perSlug := map[string]LocalPaidPlugin{}
	var kapot *LocalPaidPlugin
	for i, r := range lijst {
		if r.Error != "" {
			kapot = &lijst[i]
			continue
		}
		perSlug[r.Slug] = r
	}
	if perSlug["acf-pro"].Version != "6.4.1" || perSlug["gravityforms"].Version != "2.9.3" {
		t.Errorf("versies = %+v", perSlug)
	}
	if kapot == nil || kapot.FileName != "kapot.zip" {
		t.Errorf("de onleesbare zip hoort als fout in de lijst, kreeg %+v", lijst)
	}
}

func TestListLocalPaidPluginsZonderMap(t *testing.T) {
	svc := &PluginService{cfg: &config.Global{}}
	if lijst, err := svc.ListLocalPaidPlugins(); err != nil || lijst != nil {
		t.Errorf("zonder ingestelde map wil je (nil, nil), kreeg %v, %v", lijst, err)
	}
}

// lokalePluginService bouwt een PluginService met echte project- en git-services
// rond het testproject uit setupApplyTestProject.
func lokalePluginService(t *testing.T, ps *ProjectService, localDir string) *PluginService {
	t.Helper()
	cfg := &config.Global{}
	cfg.PluginRepo.LocalDir = localDir
	return &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps)}
}

func TestLocalPluginDiff(t *testing.T) {
	ps, projectID, _ := setupApplyTestProject(t, "acf-pro", "6.3.0")
	dir := lokaleMapMet(t, map[string][]byte{
		"acf-pro.zip": bouwPluginZip(t, "acf-pro", "6.4.1", nil),
		"andere.zip":  bouwPluginZip(t, "niet-geinstalleerd", "1.0.0", nil),
	})
	svc := lokalePluginService(t, ps, dir)

	overzicht, err := svc.LocalPluginDiff(projectID)
	if err != nil {
		t.Fatalf("LocalPluginDiff: %v", err)
	}
	if overzicht.Branch != "release/1.0.x" {
		t.Errorf("Branch = %q; de gebruiker moet zien op welke branch de commit komt", overzicht.Branch)
	}
	perSlug := map[string]LocalPluginRow{}
	for _, r := range overzicht.Rows {
		perSlug[r.Slug] = r
	}
	acf := perSlug["acf-pro"]
	if acf.ProjectVersion != "6.3.0" || acf.FolderVersion != "6.4.1" || !acf.Newer {
		t.Errorf("acf-pro rij = %+v", acf)
	}
	ander := perSlug["niet-geinstalleerd"]
	if ander.ProjectVersion != "" || ander.Newer {
		t.Errorf("een plugin die niet in het project zit is geen update: %+v", ander)
	}
}

func TestApplyLocalPluginsCommitOpHuidigeBranch(t *testing.T) {
	ps, projectID, projectDir := setupApplyTestProject(t, "acf-pro", "6.3.0")
	dir := lokaleMapMet(t, map[string][]byte{
		"acf-pro.zip": bouwPluginZip(t, "acf-pro", "6.4.1", nil),
	})
	svc := lokalePluginService(t, ps, dir)

	res, err := svc.ApplyLocalPlugins(projectID, []string{"acf-pro"})
	if err != nil {
		t.Fatalf("ApplyLocalPlugins: %v", err)
	}
	if res.Branch != "release/1.0.x" {
		t.Errorf("Branch = %q, wil de huidige branch (geen nieuwe)", res.Branch)
	}
	if len(res.Plugins) != 1 || res.Plugins[0].Status != "updated" ||
		res.Plugins[0].From != "6.3.0" || res.Plugins[0].To != "6.4.1" {
		t.Fatalf("resultaat = %+v", res.Plugins)
	}

	// Het bestand op schijf is echt de nieuwe versie.
	inhoud, err := os.ReadFile(filepath.Join(projectDir, "public", "wp-content", "plugins", "acf-pro", "acf-pro.php"))
	if err != nil || !strings.Contains(string(inhoud), "Version: 6.4.1") {
		t.Errorf("pluginbestand niet bijgewerkt: %v %s", err, inhoud)
	}

	// Er staat exact één nieuwe commit op dezelfde branch, en de werkmap is schoon.
	branch := runGitApply(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "release/1.0.x" {
		t.Errorf("branch na afloop = %q; er mag geen branchwissel plaatsvinden", branch)
	}
	bericht := runGitApply(t, projectDir, "log", "-1", "--format=%s")
	if !strings.Contains(bericht, "acf-pro") || !strings.Contains(bericht, "6.3.0") || !strings.Contains(bericht, "6.4.1") {
		t.Errorf("commitbericht = %q", bericht)
	}
	if status := runGitApply(t, projectDir, "status", "--porcelain"); status != "" {
		t.Errorf("werkmap niet schoon na commit:\n%s", status)
	}
}

func TestApplyLocalPluginsWeigertBijStagedBestanden(t *testing.T) {
	ps, projectID, projectDir := setupApplyTestProject(t, "acf-pro", "6.3.0")
	dir := lokaleMapMet(t, map[string][]byte{
		"acf-pro.zip": bouwPluginZip(t, "acf-pro", "6.4.1", nil),
	})
	svc := lokalePluginService(t, ps, dir)

	// Iemand heeft al iets klaargezet: dat zou stilletjes in onze commit meegaan.
	if err := os.WriteFile(filepath.Join(projectDir, "los.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitApply(t, projectDir, "add", "los.txt")

	_, err := svc.ApplyLocalPlugins(projectID, []string{"acf-pro"})
	if err == nil || !strings.Contains(err.Error(), "klaargezet") {
		t.Fatalf("wil een weigering vanwege staged bestanden, kreeg %v", err)
	}
	if bericht := runGitApply(t, projectDir, "log", "-1", "--format=%s"); strings.Contains(bericht, "acf-pro 6.3.0") {
		t.Error("er is toch gecommit")
	}
}

func TestApplyLocalPluginsOnbekendeSlug(t *testing.T) {
	ps, projectID, _ := setupApplyTestProject(t, "acf-pro", "6.3.0")
	dir := lokaleMapMet(t, map[string][]byte{
		"acf-pro.zip": bouwPluginZip(t, "acf-pro", "6.4.1", nil),
	})
	svc := lokalePluginService(t, ps, dir)

	res, err := svc.ApplyLocalPlugins(projectID, []string{"bestaat-niet"})
	if err != nil {
		t.Fatalf("een onbekende slug hoort een per-plugin fout te zijn: %v", err)
	}
	if len(res.Plugins) != 1 || res.Plugins[0].Status != "error" {
		t.Errorf("resultaat = %+v", res.Plugins)
	}
}
