package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

type fakeInventoryLister struct{ list []domain.Project }

func (f fakeInventoryLister) List() []domain.Project { return f.list }

type fakeInventoryResolver struct {
	plugins map[string]string
	themes  map[string]string
	core    string
}

func (f fakeInventoryResolver) LatestVersion(_ context.Context, slug string) (string, string, error) {
	if v, ok := f.plugins[slug]; ok {
		return v, "https://example.test/" + slug + ".zip", nil
	}
	return "", "", wporg.ErrNotFound
}

func (f fakeInventoryResolver) LatestThemeVersion(_ context.Context, slug string) (string, error) {
	if v, ok := f.themes[slug]; ok {
		return v, nil
	}
	return "", wporg.ErrNotFound
}

func (f fakeInventoryResolver) LatestCoreVersion(_ context.Context) (string, error) {
	return f.core, nil
}

func writePlugin(t *testing.T, root, slug, version string) {
	t.Helper()
	dir := filepath.Join(root, "public", "wp-content", "plugins", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	main := "<?php\n/*\nPlugin Name: " + slug + "\nVersion: " + version + "\n*/\n"
	if err := os.WriteFile(filepath.Join(dir, slug+".php"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeWPVersion(t *testing.T, root, version string) {
	t.Helper()
	inc := filepath.Join(root, "public", "wp-includes")
	if err := os.MkdirAll(inc, 0o755); err != nil {
		t.Fatal(err)
	}
	php := "<?php\n$wp_version = '" + version + "';\n"
	if err := os.WriteFile(filepath.Join(inc, "version.php"), []byte(php), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestInventory(lister projectLister, resolver inventoryResolver) *InventoryService {
	return &InventoryService{
		projects: lister,
		wporg:    resolver,
		cache:    make(map[string]cachedVersion),
	}
}

func TestInventoryPlugins(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writePlugin(t, rootA, "contact-form-7", "5.8")
	writePlugin(t, rootB, "contact-form-7", "5.9.2")
	writePlugin(t, rootB, "acf-pro", "6.2.0")

	svc := newTestInventory(
		fakeInventoryLister{list: []domain.Project{
			{ID: "a", Path: rootA, DisplayName: "Site A"},
			{ID: "b", Path: rootB, DisplayName: "Site B"},
		}},
		fakeInventoryResolver{plugins: map[string]string{"contact-form-7": "5.9.2"}},
	)

	items, err := svc.Plugins()
	if err != nil {
		t.Fatalf("Plugins: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}

	// sorted by slug: acf-pro first
	acf, cf7 := items[0], items[1]
	if acf.Slug != "acf-pro" || acf.Source != "manual" || acf.LatestVersion != "" {
		t.Errorf("acf-pro mismatch: %+v", acf)
	}
	if cf7.Slug != "contact-form-7" || cf7.Source != "wporg" || cf7.LatestVersion != "5.9.2" {
		t.Errorf("cf7 mismatch: %+v", cf7)
	}
	if len(cf7.Projects) != 2 {
		t.Fatalf("cf7 want 2 projects, got %d", len(cf7.Projects))
	}
	// Site A on 5.8 is outdated, Site B on 5.9.2 is current
	if !cf7.Projects[0].Outdated || cf7.Projects[0].ProjectName != "Site A" {
		t.Errorf("Site A should be outdated: %+v", cf7.Projects[0])
	}
	if cf7.Projects[1].Outdated {
		t.Errorf("Site B should be current: %+v", cf7.Projects[1])
	}
	if cf7.OutdatedCount != 1 {
		t.Errorf("cf7 OutdatedCount = %d", cf7.OutdatedCount)
	}
}

func TestInventoryWordPress(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeWPVersion(t, rootA, "6.4.1")
	writeWPVersion(t, rootB, "6.5.3")
	rootC := t.TempDir() // not a WP project

	svc := newTestInventory(
		fakeInventoryLister{list: []domain.Project{
			{ID: "a", Path: rootA, DisplayName: "Site A"},
			{ID: "b", Path: rootB, DisplayName: "Site B"},
			{ID: "c", Path: rootC, DisplayName: "Site C"},
		}},
		fakeInventoryResolver{core: "6.5.3"},
	)

	report, err := svc.WordPress()
	if err != nil {
		t.Fatalf("WordPress: %v", err)
	}
	if report.LatestVersion != "6.5.3" {
		t.Errorf("latest = %q", report.LatestVersion)
	}
	if len(report.Projects) != 2 {
		t.Fatalf("want 2 projects (C is no WP), got %d", len(report.Projects))
	}
	if !report.Projects[0].Outdated || report.Projects[1].Outdated {
		t.Errorf("outdated flags wrong: %+v", report.Projects)
	}
}

// TestInventoryReadsFromDefaultBranch proves the GitHub column comes from the
// git default branch while the local column shows the working tree, so both are
// visible side by side.
func TestInventoryReadsFromDefaultBranch(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "contact-form-7", "5.8")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.nl")
	run("config", "user.name", "t")
	run("checkout", "-q", "-b", "release/1.0.x")
	run("add", "-A")
	run("commit", "-q", "-m", "init")

	// Working tree diverges: bump to 5.9 without committing.
	writePlugin(t, root, "contact-form-7", "5.9")

	svc := newTestInventory(
		fakeInventoryLister{list: []domain.Project{
			{ID: "a", Path: root, DisplayName: "Site A"},
		}},
		fakeInventoryResolver{plugins: map[string]string{"contact-form-7": "5.9"}},
	)

	items, err := svc.Plugins()
	if err != nil {
		t.Fatalf("Plugins: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	p := items[0].Projects[0]
	if p.GithubVersion != "5.8" {
		t.Errorf("github-kolom = %q, want committed 5.8", p.GithubVersion)
	}
	if p.LocalVersion != "5.9" {
		t.Errorf("lokale kolom = %q, want working tree 5.9", p.LocalVersion)
	}
	if p.Ref != "release/1.0.x" {
		t.Errorf("ref = %q, want release/1.0.x", p.Ref)
	}
	if !p.Outdated {
		t.Error("5.8 < latest 5.9 should be outdated")
	}
}

// TestInventoryDisjuncteSlugs dekt de kernbelofte van de drie-kolommen-feature:
// een plugin die alleen lokaal bestaat (nog niet gecommit) of alleen op de
// GitHub-branch (net gemerged, lokaal nog niet gepulled) moet zichtbaar blijven,
// met een lege cel in de andere kolom.
func TestInventoryDisjuncteSlugs(t *testing.T) {
	root := t.TempDir()
	// Alleen op de GitHub-branch: committen en daarna lokaal weghalen.
	writePlugin(t, root, "alleen-github", "1.0")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.nl")
	run("config", "user.name", "t")
	run("checkout", "-q", "-b", "release/1.0.x")
	run("add", "-A")
	run("commit", "-q", "-m", "init")

	if err := os.RemoveAll(filepath.Join(root, "public", "wp-content", "plugins", "alleen-github")); err != nil {
		t.Fatal(err)
	}
	// Alleen lokaal: na de commit toegevoegd.
	writePlugin(t, root, "alleen-lokaal", "2.0")

	svc := newTestInventory(
		fakeInventoryLister{list: []domain.Project{{ID: "a", Path: root, DisplayName: "Site A"}}},
		fakeInventoryResolver{plugins: map[string]string{"alleen-github": "1.0", "alleen-lokaal": "2.0"}},
	)

	items, err := svc.Plugins()
	if err != nil {
		t.Fatalf("Plugins: %v", err)
	}
	byslug := map[string]InventoryProjectRef{}
	for _, it := range items {
		if len(it.Projects) > 0 {
			byslug[it.Slug] = it.Projects[0]
		}
	}
	if len(byslug) != 2 {
		t.Fatalf("verwachtte beide slugs, kreeg %d: %+v", len(byslug), byslug)
	}
	if g := byslug["alleen-github"]; g.GithubVersion != "1.0" || g.LocalVersion != "" {
		t.Errorf("alleen-github = %+v, want github 1.0 en lege lokale cel", g)
	}
	if l := byslug["alleen-lokaal"]; l.LocalVersion != "2.0" || l.GithubVersion != "" {
		t.Errorf("alleen-lokaal = %+v, want lokaal 2.0 en lege github-cel", l)
	}
}

func newTestInventoryMetReferentie(lister projectLister, resolver inventoryResolver, referencePath string) *InventoryService {
	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = referencePath
	return &InventoryService{
		projects: lister,
		wporg:    resolver,
		cfg:      cfg,
		cache:    make(map[string]cachedVersion),
	}
}

// TestInventoryPluginsSluitReferentieProjectUit is de kern van de opdracht: de
// referentie-installatie mag niet als klantproject in het Plugins-overzicht
// verschijnen, ook al staat hij gewoon onder projects_roots.
func TestInventoryPluginsSluitReferentieProjectUit(t *testing.T) {
	referentieRoot := t.TempDir()
	writePlugin(t, referentieRoot, "acf-pro", "6.9.0")

	klantRoot := t.TempDir()
	writePlugin(t, klantRoot, "acf-pro", "6.4.1")

	lister := fakeInventoryLister{list: []domain.Project{
		{ID: "ref", DisplayName: "referentie-installatie", Path: referentieRoot},
		{ID: "klant", DisplayName: "klant", Path: klantRoot},
	}}
	svc := newTestInventoryMetReferentie(lister, fakeInventoryResolver{}, referentieRoot)

	items, err := svc.Plugins()
	if err != nil {
		t.Fatalf("Plugins: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v, wil precies 1 (acf-pro, alleen de klant)", items)
	}
	acf := items[0]
	if len(acf.Projects) != 1 || acf.Projects[0].ProjectID != "klant" {
		t.Errorf("acf-pro.Projects = %+v; de referentie-installatie hoort er niet in te staan", acf.Projects)
	}
}

// TestInventoryPluginsReferentieWintVanWporg dekt de expliciete keuze: als een
// plugin zowel op wp.org staat als in de referentie-installatie, bepaalt de
// referentie de "laatste versie" — ook als die ouder lijkt dan wp.org zegt.
func TestInventoryPluginsReferentieWintVanWporg(t *testing.T) {
	referentieRoot := t.TempDir()
	writePlugin(t, referentieRoot, "jetpack-achtig", "3.0.0") // de referentie is de waarheid

	klantRoot := t.TempDir()
	writePlugin(t, klantRoot, "jetpack-achtig", "2.9.0")

	lister := fakeInventoryLister{list: []domain.Project{
		{ID: "ref", DisplayName: "referentie-installatie", Path: referentieRoot},
		{ID: "klant", DisplayName: "klant", Path: klantRoot},
	}}
	// wp.org kent een hogere versie dan de referentie; de referentie moet
	// desondanks winnen.
	resolver := fakeInventoryResolver{plugins: map[string]string{"jetpack-achtig": "5.0.0"}}
	svc := newTestInventoryMetReferentie(lister, resolver, referentieRoot)

	items, err := svc.Plugins()
	if err != nil {
		t.Fatalf("Plugins: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if it.LatestVersion != "3.0.0" {
		t.Errorf("LatestVersion = %q, wil 3.0.0 (uit de referentie, niet wp.org's 5.0.0)", it.LatestVersion)
	}
	if it.Source != "reference" {
		t.Errorf("Source = %q, wil reference", it.Source)
	}
}

// Een plugin die alleen op wp.org staat (niet in de referentie) blijft
// gewoon via wp.org werken.
func TestInventoryPluginsZonderReferentieBlijftWporgGebruiken(t *testing.T) {
	referentieRoot := t.TempDir()
	writePlugin(t, referentieRoot, "acf-pro", "6.9.0")

	klantRoot := t.TempDir()
	writePlugin(t, klantRoot, "hello-dolly", "1.7.2")

	lister := fakeInventoryLister{list: []domain.Project{
		{ID: "ref", DisplayName: "referentie-installatie", Path: referentieRoot},
		{ID: "klant", DisplayName: "klant", Path: klantRoot},
	}}
	resolver := fakeInventoryResolver{plugins: map[string]string{"hello-dolly": "1.7.2"}}
	svc := newTestInventoryMetReferentie(lister, resolver, referentieRoot)

	items, err := svc.Plugins()
	if err != nil {
		t.Fatalf("Plugins: %v", err)
	}
	if len(items) != 1 || items[0].Slug != "hello-dolly" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Source != "wporg" {
		t.Errorf("Source = %q, wil wporg", items[0].Source)
	}
}

// Zonder ingestelde referentie verandert er niets: Source blijft manual/wporg
// zoals voorheen, en Themes() kent het begrip referentie sowieso niet.
func TestInventoryThemesGeenReferentieBegrip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "public", "wp-content", "themes", "mytheme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	css := "/*\nTheme Name: mytheme\nVersion: 1.0.0\n*/\n"
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	lister := fakeInventoryLister{list: []domain.Project{{ID: "klant", DisplayName: "klant", Path: root}}}
	// De referentie wijst bewust naar een ándere map dan het testproject: anders
	// zou scannableProjects() het enige project uitsluiten en is de test zinloos.
	svc := newTestInventoryMetReferentie(lister, fakeInventoryResolver{}, t.TempDir())

	items, err := svc.Themes()
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(items) != 1 || items[0].Source == "reference" {
		t.Errorf("items = %+v; Themes() kent geen reference-source", items)
	}
}
