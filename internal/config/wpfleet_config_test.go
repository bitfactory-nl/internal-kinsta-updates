package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWPFleetDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	g, err := LoadGlobal() // geen bestand → defaults
	if err != nil {
		t.Fatal(err)
	}
	if g.WPFleet.Image != DefaultWPFleetImage || g.WPFleet.ReadOnly {
		t.Fatalf("defaults: %+v", g.WPFleet)
	}

	// Bestaand bestand zonder wpfleet-sectie krijgt ook de default image.
	p := filepath.Join(home, ".config", "rdm", "config.yml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("editor: cursor\n"), 0o600)
	g, err = LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.WPFleet.Image != DefaultWPFleetImage {
		t.Fatalf("image = %q", g.WPFleet.Image)
	}
}
