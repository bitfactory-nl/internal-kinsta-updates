package services

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// herzetGereedschapPATH maakt de cache leeg en zet de seams terug na de test.
func herzetGereedschapPATH(t *testing.T, bestaat func(string) bool, loginPad string) {
	t.Helper()
	origBestaat, origLogin := padBestaat, loginShellPATHFn
	t.Cleanup(func() {
		padBestaat, loginShellPATHFn = origBestaat, origLogin
		gereedschapEenmalig = sync.Once{}
		gereedschapPATHCache = ""
	})
	gereedschapEenmalig = sync.Once{}
	gereedschapPATHCache = ""
	padBestaat = bestaat
	loginShellPATHFn = func() string { return loginPad }
}

// TestGereedschapPATHVultDeAppPATHAan is de test die het gerapporteerde probleem
// vastlegt: in de geïnstalleerde app is PATH=/usr/bin:/bin:/usr/sbin:/sbin, en
// dan moet /usr/local/bin erbij komen — daar staat docker-compose.
func TestGereedschapPATHVultDeAppPATHAan(t *testing.T) {
	herzetGereedschapPATH(t, func(pad string) bool {
		return pad == "/usr/local/bin" || pad == "/opt/homebrew/bin"
	}, "")
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	t.Setenv("SHELL", "")

	got := GereedschapPATH()
	delen := strings.Split(got, ":")

	for _, moet := range []string{"/usr/local/bin", "/opt/homebrew/bin"} {
		if !bevat(delen, moet) {
			t.Errorf("%q ontbreekt in %q", moet, got)
		}
	}
	// De oorspronkelijke PATH blijft staan: alleen aanvullen, nooit vervangen.
	for _, moet := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if !bevat(delen, moet) {
			t.Errorf("oorspronkelijke %q is weggevallen uit %q", moet, got)
		}
	}
	// Gereedschap moet vóór de systeempaden staan, anders wint een oude
	// systeemversie van een tool.
	if indexVan(delen, "/usr/local/bin") > indexVan(delen, "/usr/bin") {
		t.Errorf("/usr/local/bin hoort vóór /usr/bin te staan: %q", got)
	}
}

func TestGereedschapPATHSlaatNietBestaandeMappenOver(t *testing.T) {
	herzetGereedschapPATH(t, func(string) bool { return false }, "")
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("SHELL", "")

	got := GereedschapPATH()
	if strings.Contains(got, "/opt/homebrew/bin") || strings.Contains(got, "Docker.app") {
		t.Errorf("niet-bestaande mappen toegevoegd: %q", got)
	}
	if got != "/usr/bin:/bin" {
		t.Errorf("PATH = %q, wil ongewijzigd", got)
	}
}

func TestGereedschapPATHGeenDuplicaten(t *testing.T) {
	herzetGereedschapPATH(t, func(pad string) bool { return pad == "/usr/local/bin" }, "")
	// /usr/local/bin staat al in de PATH; het mag er niet twee keer in komen.
	t.Setenv("PATH", "/usr/local/bin:/usr/bin")
	t.Setenv("SHELL", "")

	delen := strings.Split(GereedschapPATH(), ":")
	if n := aantalVan(delen, "/usr/local/bin"); n != 1 {
		t.Errorf("/usr/local/bin komt %d keer voor: %v", n, delen)
	}
}

// De loginshell wordt alleen geraadpleegd als de PATH echt uitgekleed is; die
// aanroep start de rc-bestanden van de gebruiker op en is traag.
func TestGereedschapPATHGebruiktLoginshellAlleenBijMinimalePATH(t *testing.T) {
	geraadpleegd := false
	herzetGereedschapPATH(t, func(string) bool { return false }, "/eigen/asdf/shims")
	loginShellPATHFn = func() string {
		geraadpleegd = true
		return "/eigen/asdf/shims"
	}
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	t.Setenv("SHELL", "/bin/zsh")

	got := GereedschapPATH()
	if !geraadpleegd {
		t.Error("de loginshell is niet geraadpleegd bij een minimale PATH")
	}
	if !strings.Contains(got, "/eigen/asdf/shims") {
		t.Errorf("loginshell-PATH ontbreekt: %q", got)
	}
}

func TestGereedschapPATHSlaatLoginshellOverBijNormalePATH(t *testing.T) {
	geraadpleegd := false
	herzetGereedschapPATH(t, func(string) bool { return false }, "")
	loginShellPATHFn = func() string {
		geraadpleegd = true
		return "/mag/niet"
	}
	t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")
	t.Setenv("SHELL", "/bin/zsh")

	if got := GereedschapPATH(); strings.Contains(got, "/mag/niet") {
		t.Errorf("loginshell onterecht gebruikt: %q", got)
	}
	if geraadpleegd {
		t.Error("de loginshell is onnodig geraadpleegd")
	}
}

func TestIsMinimalePATH(t *testing.T) {
	tests := []struct {
		pad string
		wil bool
	}{
		{"/usr/bin:/bin:/usr/sbin:/sbin", true},
		{"/opt/homebrew/bin:/usr/bin", false},
		{"/usr/local/bin:/usr/bin", false},
		{"/opt/local/bin:/usr/bin", false},
		{"", true},
	}
	for _, tt := range tests {
		if got := isMinimalePATH(tt.pad); got != tt.wil {
			t.Errorf("isMinimalePATH(%q) = %v, wil %v", tt.pad, got, tt.wil)
		}
	}
}

func TestMetPATH(t *testing.T) {
	got := MetPATH([]string{"HOME=/x", "PATH=/oud", "LANG=nl"}, "/nieuw")
	if len(got) != 3 {
		t.Fatalf("got = %v", got)
	}
	if got[1] != "PATH=/nieuw" {
		t.Errorf("PATH niet op zijn plek vervangen: %v", got)
	}
	if aantalVan(got, "PATH=/nieuw") != 1 {
		t.Errorf("PATH komt meer dan één keer voor: %v", got)
	}
}

func TestMetPATHVoegtToeAlsHijOntbreekt(t *testing.T) {
	got := MetPATH([]string{"HOME=/x"}, "/nieuw")
	if len(got) != 2 || got[1] != "PATH=/nieuw" {
		t.Errorf("got = %v", got)
	}
}

func TestMetPATHRuimtDubbelePATHOp(t *testing.T) {
	got := MetPATH([]string{"PATH=/a", "HOME=/x", "PATH=/b"}, "/nieuw")
	if n := aantalVan(got, "PATH=/nieuw"); n != 1 {
		t.Errorf("PATH komt %d keer voor: %v", n, got)
	}
	for _, kv := range got {
		if kv == "PATH=/a" || kv == "PATH=/b" {
			t.Errorf("oude PATH blijft staan: %v", got)
		}
	}
}

// TestMakeRunKrijgtVerrijktePATH bewijst dat het echte make-commando de
// aangevulde PATH meekrijgt; anders is de fix theorie.
func TestMakeRunKrijgtVerrijktePATH(t *testing.T) {
	dir := t.TempDir()
	// Een Makefile die alleen zijn eigen PATH afdrukt.
	makefile := ".PHONY: padtest\npadtest:\n\t@printf '%s' \"$$PATH\"\n"
	if err := os.WriteFile(dir+"/Makefile", []byte(makefile), 0o644); err != nil {
		t.Fatal(err)
	}

	herzetGereedschapPATH(t, func(pad string) bool { return pad == "/usr/local/bin" }, "")
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	t.Setenv("SHELL", "")

	projects := NewProjectService(nil)
	projects.projects = []domain.Project{{ID: "p1", DisplayName: "Test", Path: dir}}
	svc := NewMakeService(projects)

	res, err := svc.Run("p1", "padtest")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Fatalf("make faalde: %s", res.Output)
	}
	if !strings.Contains(res.Output, "/usr/local/bin") {
		t.Errorf("make kreeg geen verrijkte PATH: %q", res.Output)
	}
}

func bevat(lijst []string, waarde string) bool {
	for _, x := range lijst {
		if x == waarde {
			return true
		}
	}
	return false
}

func indexVan(lijst []string, waarde string) int {
	for i, x := range lijst {
		if x == waarde {
			return i
		}
	}
	return len(lijst) + 1
}

func aantalVan(lijst []string, waarde string) int {
	n := 0
	for _, x := range lijst {
		if x == waarde {
			n++
		}
	}
	return n
}
