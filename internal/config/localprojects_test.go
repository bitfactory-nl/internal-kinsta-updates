package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestLocalProjectsRoundtrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := LocalProjectsPath()
	in := []domain.LocalProject{{Path: filepath.Join(home, "Projects", "web-laravel"), Name: "Laravel"}}
	if err := SaveLocalProjects(path, in); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "path: ~/Projects/web-laravel") {
		t.Fatalf("pad niet met ~:\n%s", raw)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("rechten %v", info.Mode().Perm())
	}
	got, err := LoadLocalProjects(path)
	if err != nil || len(got) != 1 || got[0].Path != in[0].Path || got[0].Name != "Laravel" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLocalProjectsOntbrekend(t *testing.T) {
	got, err := LoadLocalProjects(filepath.Join(t.TempDir(), "nee.yml"))
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestLocalProjectsWeigertLeegOfDubbelPad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "l.yml")
	if err := SaveLocalProjects(p, []domain.LocalProject{{Path: ""}}); err == nil {
		t.Fatal("leeg pad moet geweigerd worden")
	}
	if err := SaveLocalProjects(p, []domain.LocalProject{{Path: "/a"}, {Path: "/a"}}); err == nil {
		t.Fatal("dubbel pad moet geweigerd worden")
	}
}

func TestValidateRegisterPadBotsing(t *testing.T) {
	sites := []domain.WPSite{{ID: "aa", Name: "a", URL: "https://a.nl", Username: "u", ProjectPath: "/p"}}
	err := ValidateRegister(sites, []domain.LocalProject{{Path: "/p"}})
	if err == nil || !strings.Contains(err.Error(), "map /p staat al in het register") {
		t.Fatalf("fout %v", err)
	}
	if err := ValidateRegister(sites, []domain.LocalProject{{Path: "/q"}}); err != nil {
		t.Fatal(err)
	}
}

func TestCloneDirDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	g, _ := LoadGlobal()
	if g.CloneDir != "~/Projects" {
		t.Fatalf("clone_dir %q", g.CloneDir)
	}
}
