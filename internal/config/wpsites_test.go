package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestWPSitesRoundtripEnTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "rdm", "wp-sites.yml")

	sites := []domain.WPSite{
		{ID: "klant-a", Name: "Klant A", URL: "https://a.nl", Username: "mcp-bot",
			ProjectPath: filepath.Join(home, "Projects", "web-a"), Tags: []string{"kinsta"}, Bridge: true},
		{ID: "depot", Name: "Depot", URL: "https://depot.nl", Username: "mcp-bot"},
	}
	if err := SaveWPSites(path, sites); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "project_path: ~/Projects/web-a") {
		t.Fatalf("project_path niet met ~ opgeslagen:\n%s", raw)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("rechten %v, want 0600", info.Mode().Perm())
	}

	got, err := LoadWPSites(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ProjectPath != filepath.Join(home, "Projects", "web-a") || got[1].ProjectPath != "" {
		t.Fatalf("onverwacht resultaat: %+v", got)
	}
}

func TestLoadWPSitesOntbrekendBestand(t *testing.T) {
	got, err := LoadWPSites(filepath.Join(t.TempDir(), "bestaat-niet.yml"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want lege lijst zonder fout", got, err)
	}
}

func TestLoadWPSitesOngeldig(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"yaml-syntax":    "sites: [\n",
		"ongeldige site": "sites:\n  - id: X\n    name: a\n    url: https://a.nl\n    username: u\n",
		"dubbele id":     "sites:\n  - {id: aa, name: a, url: https://a.nl, username: u}\n  - {id: aa, name: b, url: https://b.nl, username: u}\n",
		"dubbel project": "sites:\n  - {id: aa, name: a, url: https://a.nl, username: u, project_path: /p}\n  - {id: bb, name: b, url: https://b.nl, username: u, project_path: /p}\n",
	}
	for naam, inhoud := range cases {
		p := filepath.Join(dir, naam+".yml")
		os.WriteFile(p, []byte(inhoud), 0o600)
		if _, err := LoadWPSites(p); err == nil {
			t.Errorf("%s: verwacht fout", naam)
		} else if !strings.Contains(err.Error(), "wp-sites") && naam != "yaml-syntax" {
			t.Errorf("%s: foutmelding noemt bestand niet: %v", naam, err)
		}
	}
}

func TestSaveWPSitesWeigertOngeldigEnLaatBestandStaan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-sites.yml")
	goed := []domain.WPSite{{ID: "aa", Name: "a", URL: "https://a.nl", Username: "u"}}
	if err := SaveWPSites(path, goed); err != nil {
		t.Fatal(err)
	}
	fout := []domain.WPSite{{ID: "X", Name: "a", URL: "https://a.nl", Username: "u"}}
	if err := SaveWPSites(path, fout); err == nil {
		t.Fatal("verwacht fout")
	}
	got, _ := LoadWPSites(path)
	if len(got) != 1 || got[0].ID != "aa" {
		t.Fatalf("bestand is aangetast: %+v", got)
	}
}
