package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// maakBundle bouwt een minimale .app-structuur in dir en geeft het pad terug.
func maakBundle(t *testing.T, dir, naam string, uitvoerbaar bool) string {
	t.Helper()
	app := filepath.Join(dir, naam)
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	if uitvoerbaar {
		mode = 0o755
	}
	if err := os.WriteFile(filepath.Join(macos, "rdm-sites-tool"), []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestValidateStagedAppVindtDeBundle(t *testing.T) {
	dir := t.TempDir()
	wil := maakBundle(t, dir, "Kinsta Updater.app", true)

	got, err := validateStagedApp(dir)
	if err != nil {
		t.Fatalf("validateStagedApp: %v", err)
	}
	if got != wil {
		t.Errorf("pad = %q, wil %q", got, wil)
	}
}

func TestValidateStagedAppZonderBundle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hoi"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := validateStagedApp(dir); err == nil {
		t.Fatal("validateStagedApp gaf geen fout voor een archief zonder .app")
	}
}

func TestValidateStagedAppMetTweeBundles(t *testing.T) {
	dir := t.TempDir()
	maakBundle(t, dir, "Een.app", true)
	maakBundle(t, dir, "Twee.app", true)

	_, err := validateStagedApp(dir)
	if err == nil {
		t.Fatal("validateStagedApp gaf geen fout voor twee bundles")
	}
	if !strings.Contains(err.Error(), "twee") && !strings.Contains(err.Error(), "meer dan") {
		t.Errorf("foutmelding = %q, wil uitleggen dat er meer dan één bundle is", err.Error())
	}
}

func TestValidateStagedAppZonderUitvoerbaarBinair(t *testing.T) {
	dir := t.TempDir()
	maakBundle(t, dir, "Kinsta Updater.app", false)

	if _, err := validateStagedApp(dir); err == nil {
		t.Fatal("validateStagedApp accepteerde een niet-uitvoerbaar binair bestand")
	}
}

func TestValidateStagedAppZonderInfoPlist(t *testing.T) {
	dir := t.TempDir()
	app := maakBundle(t, dir, "Kinsta Updater.app", true)
	if err := os.Remove(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		t.Fatal(err)
	}

	if _, err := validateStagedApp(dir); err == nil {
		t.Fatal("validateStagedApp accepteerde een bundle zonder Info.plist")
	}
}

func TestRenderUpdateScriptIsGeldigBashEnBevatDePaden(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "update.sh")
	d := scriptData{
		PID:        4242,
		BundlePath: "/Applications/Kinsta Updater.app",
		StagedApp:  filepath.Join(dir, "staged", "Kinsta Updater.app"),
		LogPath:    filepath.Join(dir, "update.log"),
		NodePath:   "/opt/homebrew/bin/node",
		PATH:       "/opt/homebrew/bin:/usr/bin:/bin",
	}

	if err := renderUpdateScript(script, d); err != nil {
		t.Fatalf("renderUpdateScript: %v", err)
	}

	// Syntaxcontrole zonder uitvoeren: bash -n leest het script alleen.
	cmd := exec.Command("bash", "-n", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}

	// Extra syntax- en stijlcontrole met shellcheck, als die op deze machine
	// beschikbaar is; zonder shellcheck blijft bash -n de enige controle.
	if _, err := exec.LookPath("shellcheck"); err == nil {
		if out, err := exec.Command("shellcheck", script).CombinedOutput(); err != nil {
			t.Errorf("shellcheck: %v\n%s", err, out)
		}
	}

	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	inhoud := string(data)
	for _, wil := range []string{
		"4242",
		d.BundlePath,
		d.StagedApp,
		d.LogPath,
		d.NodePath,
		d.PATH,
		"ditto",
		"com.apple.quarantine",
		"playwright",
		"open",
		`"$NODE"`,
	} {
		if !strings.Contains(inhoud, wil) {
			t.Errorf("script bevat %q niet", wil)
		}
	}
	if strings.Contains(inhoud, "command -v node >/dev/null") {
		t.Errorf("script gebruikt nog command -v node >/dev/null in plaats van $NODE")
	}

	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("rechten = %v, wil uitvoerbaar", info.Mode().Perm())
	}
}

func TestRenderUpdateScriptQuotPadenMetSpaties(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "update.sh")
	d := scriptData{
		PID:        1,
		BundlePath: "/Applications/Kinsta Updater.app",
		StagedApp:  "/tmp/rdm update/Kinsta Updater.app",
		LogPath:    "/tmp/rdm logs/update.log",
		NodePath:   "/Users/x y/.nvm/versions/node/v24.2.0/bin/node",
	}

	if err := renderUpdateScript(script, d); err != nil {
		t.Fatalf("renderUpdateScript: %v", err)
	}

	data, _ := os.ReadFile(script)
	// Elk pad moet tussen dubbele quotes staan, anders breekt het script op de
	// spatie in "Kinsta Updater.app".
	for _, pad := range []string{d.BundlePath, d.StagedApp, d.LogPath, d.NodePath} {
		if !strings.Contains(string(data), `"`+pad+`"`) {
			t.Errorf("pad %q staat niet gequote in het script", pad)
		}
	}
}

func TestBekendNodePadNegeertKaalNode(t *testing.T) {
	origineel := zoekNodeBin
	defer func() { zoekNodeBin = origineel }()

	zoekNodeBin = func() string { return "node" }
	if got := bekendNodePad(); got != "" {
		t.Errorf("bekendNodePad() = %q, wil \"\" voor een kaal \"node\"", got)
	}

	zoekNodeBin = func() string { return "/opt/homebrew/bin/node" }
	if got := bekendNodePad(); got != "/opt/homebrew/bin/node" {
		t.Errorf("bekendNodePad() = %q, wil /opt/homebrew/bin/node", got)
	}
}

func TestInstallZonderBeschikbareUpdate(t *testing.T) {
	s, _ := testService(t, "v0.2.9", &nepFetcher{})

	if err := s.Install(); err == nil {
		t.Fatal("Install gaf geen fout zonder beschikbare update")
	}
}

func TestInstallInDevBuild(t *testing.T) {
	s, _ := testService(t, "dev", &nepFetcher{})

	if err := s.Install(); err == nil {
		t.Fatal("Install gaf geen fout in een dev-build")
	}
}
