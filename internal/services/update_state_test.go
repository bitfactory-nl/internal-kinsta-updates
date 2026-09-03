package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestLoadUpdateStateOntbrekendBestand(t *testing.T) {
	st, err := loadUpdateState(filepath.Join(t.TempDir(), "bestaat-niet.json"))
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if st.SkippedVersion != "" || !st.LastCheck.IsZero() {
		t.Errorf("state = %+v, wil een lege state", st)
	}
}

func TestSaveEnLoadUpdateStateRondrit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "update-state.json")
	wil := updateState{
		LastCheck:        time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
		SkippedVersion:   "v0.2.10",
		LastRunVersion:   "v0.2.9",
		InstalledVersion: "v0.2.10",
		InstalledChanges: []domain.ChangeEntry{{Kind: domain.ChangeNieuw, Text: "Zelf-update"}},
		InstallLog:       "/tmp/update.log",
	}

	if err := saveUpdateState(path, wil); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	got, err := loadUpdateState(path)
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if !got.LastCheck.Equal(wil.LastCheck) {
		t.Errorf("LastCheck = %v, wil %v", got.LastCheck, wil.LastCheck)
	}
	if got.SkippedVersion != wil.SkippedVersion || got.LastRunVersion != wil.LastRunVersion {
		t.Errorf("versies = %+v, wil %+v", got, wil)
	}
	if len(got.InstalledChanges) != 1 || got.InstalledChanges[0].Text != "Zelf-update" {
		t.Errorf("InstalledChanges = %+v, wil één regel", got.InstalledChanges)
	}
}

func TestSaveUpdateStateLaatGeenTempbestandenAchter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-state.json")

	if err := saveUpdateState(path, updateState{SkippedVersion: "v1.0.0"}); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "update-state.json" {
		var namen []string
		for _, e := range entries {
			namen = append(namen, e.Name())
		}
		t.Errorf("map bevat %v, wil alleen update-state.json", namen)
	}
}

func TestLoadUpdateStateKapotteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-state.json")
	if err := os.WriteFile(path, []byte("{dit is geen json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadUpdateState(path); err == nil {
		t.Error("loadUpdateState gaf geen fout bij kapotte JSON")
	}
}

func TestDefaultUpdateStatePath(t *testing.T) {
	got := DefaultUpdateStatePath()
	if filepath.Base(got) != "update-state.json" {
		t.Errorf("pad = %q, wil eindigen op update-state.json", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("pad = %q, wil een absoluut pad", got)
	}
}
