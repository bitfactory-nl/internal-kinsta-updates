package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestProjectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveProject(dir, domain.ProjectConfig{DisplayName: "abc"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ProjectConfigFile)); err != nil {
		t.Fatalf("config hoort in %s te staan: %v", ProjectConfigFile, err)
	}
	got, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.DisplayName != "abc" {
		t.Errorf("display_name = %q; wil abc", got.DisplayName)
	}
}

// Projecten die nog het oude .rdm.yml in de root hebben, moeten gewoon
// gelezen blijven worden — anders zou de tool hun config kwijt lijken.
func TestProjectLeestOudeLocatie(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LegacyProjectConfigFile),
		[]byte("rdm_schema_version: 1\ndisplay_name: oud\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasProjectConfig(dir) {
		t.Error("HasProjectConfig moet true zijn bij alleen een oud .rdm.yml")
	}
	got, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.DisplayName != "oud" {
		t.Errorf("display_name = %q; wil oud", got.DisplayName)
	}
}

// De eerstvolgende save verhuist het project naar .rdm/ en laat geen los
// bestand in de root achter.
func TestProjectMigreertBijSave(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, LegacyProjectConfigFile)
	if err := os.WriteFile(legacy, []byte("rdm_schema_version: 1\ndisplay_name: oud\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.DisplayName = "nieuw"
	if err := SaveProject(dir, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("oude %s hoort opgeruimd te zijn, err = %v", LegacyProjectConfigFile, err)
	}
	got, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("herlezen: %v", err)
	}
	if got.DisplayName != "nieuw" {
		t.Errorf("display_name = %q; wil nieuw", got.DisplayName)
	}
}

// Staan beide bestanden er, dan wint .rdm/config.yml.
func TestProjectNieuweLocatieWint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LegacyProjectConfigFile),
		[]byte("rdm_schema_version: 1\ndisplay_name: oud\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveProject(dir, domain.ProjectConfig{DisplayName: "nieuw"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.DisplayName != "nieuw" {
		t.Errorf("display_name = %q; wil nieuw", got.DisplayName)
	}
}
