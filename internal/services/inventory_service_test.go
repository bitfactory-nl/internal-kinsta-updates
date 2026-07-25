package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/wporg"
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
