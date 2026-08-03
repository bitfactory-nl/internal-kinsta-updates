package services

import (
	"os"
	"path/filepath"
	"testing"
)

// legNeer maakt een leeg scriptbestand aan, inclusief tussenliggende mappen.
func legNeer(t *testing.T, pad string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(pad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pad, []byte("// test"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// doeAlsofBinaryStaatIn laat vindSidecar denken dat het programma in dir staat.
func doeAlsofBinaryStaatIn(t *testing.T, dir string) {
	t.Helper()
	origineel := sidecarExe
	sidecarExe = func() (string, error) { return filepath.Join(dir, "rdm-sites-tool"), nil }
	t.Cleanup(func() { sidecarExe = origineel })
}

func TestVindSidecarInAppBundel(t *testing.T) {
	root := t.TempDir()
	macos := filepath.Join(root, "Kinsta Updater.app", "Contents", "MacOS")
	script := filepath.Join(root, "Kinsta Updater.app", "Contents", "Resources", "sidecar", "pdf.mjs")
	legNeer(t, script)
	doeAlsofBinaryStaatIn(t, macos)

	// De werkmap is bewust een lege map: zo bewijst de test dat het bundel-pad wordt
	// gevonden en niet per ongeluk de werkmap.
	t.Chdir(t.TempDir())

	got := vindSidecar("pdf.mjs", "RDM_TEST_PDF_ONBEKEND")
	if got != filepath.Clean(script) {
		t.Errorf("vindSidecar = %q, wil het pad in Contents/Resources: %q", got, script)
	}
}

func TestVindSidecarNaastHetBinary(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "sidecar", "runner.mjs")
	legNeer(t, script)
	doeAlsofBinaryStaatIn(t, root)
	t.Chdir(t.TempDir())

	if got := vindSidecar("runner.mjs", "RDM_TEST_ONBEKEND"); got != filepath.Clean(script) {
		t.Errorf("vindSidecar = %q, wil %q", got, script)
	}
}

func TestVindSidecarInRepoNaastBin(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	script := filepath.Join(root, "sidecar", "crawl.mjs")
	legNeer(t, script)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	doeAlsofBinaryStaatIn(t, bin)
	t.Chdir(t.TempDir())

	if got := vindSidecar("crawl.mjs", "RDM_TEST_ONBEKEND"); got != filepath.Clean(script) {
		t.Errorf("vindSidecar = %q, wil %q", got, script)
	}
}

func TestVindSidecarValtTerugOpWerkmap(t *testing.T) {
	// Zo werkt de dev-server: het binary staat elders, de sidecar in de werkmap.
	werk := t.TempDir()
	legNeer(t, filepath.Join(werk, "sidecar", "runner.mjs"))
	doeAlsofBinaryStaatIn(t, t.TempDir())
	t.Chdir(werk)

	if got := vindSidecar("runner.mjs", "RDM_TEST_ONBEKEND"); got != filepath.Join("sidecar", "runner.mjs") {
		t.Errorf("vindSidecar = %q, wil het werkmap-pad", got)
	}
}

func TestVindSidecarOmgevingsvariabeleWint(t *testing.T) {
	root := t.TempDir()
	legNeer(t, filepath.Join(root, "sidecar", "runner.mjs"))
	doeAlsofBinaryStaatIn(t, root)
	t.Setenv("RDM_SIDECAR", "/eigen/pad/runner.mjs")

	if got := vindSidecar("runner.mjs", "RDM_SIDECAR"); got != "/eigen/pad/runner.mjs" {
		t.Errorf("vindSidecar = %q, wil de override", got)
	}
}

func TestVindSidecarZonderTreffer(t *testing.T) {
	doeAlsofBinaryStaatIn(t, t.TempDir())
	t.Chdir(t.TempDir())

	// Niets gevonden: dan een pad dat een mens herkent, geen lege string.
	if got := vindSidecar("pdf.mjs", "RDM_TEST_ONBEKEND"); got != filepath.Join("sidecar", "pdf.mjs") {
		t.Errorf("vindSidecar = %q, wil sidecar/pdf.mjs", got)
	}
}
