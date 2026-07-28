package services

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// bouwCoreZip maakt een mini "no-content" WordPress-zip: entries onder
// wordpress/, zonder wp-content.
func bouwCoreZip(t *testing.T, bestanden map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for naam, inhoud := range bestanden {
		w, err := zw.Create("wordpress/" + naam)
		if err != nil {
			t.Fatalf("zip create %s: %v", naam, err)
		}
		if _, err := w.Write([]byte(inhoud)); err != nil {
			t.Fatalf("zip write %s: %v", naam, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// bouwWPRoot maakt een fixture-boom die lijkt op public/ in een echt project.
func bouwWPRoot(t *testing.T, bestanden map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, inhoud := range bestanden {
		pad := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(pad), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", pad, err)
		}
		if err := os.WriteFile(pad, []byte(inhoud), 0o644); err != nil {
			t.Fatalf("write %s: %v", pad, err)
		}
	}
	return root
}

func TestIsCoreRootFile(t *testing.T) {
	tests := []struct {
		naam string
		want bool
	}{
		{"wp-login.php", true},
		{"wp-settings.php", true},
		{"index.php", true},
		{"xmlrpc.php", true},
		{"readme.html", true},
		{"license.txt", true},
		{"wp-config.php", false},       // nooit aanraken
		{"custom-cronfile.php", false}, // project-eigen bestand
		{"robots.txt", false},
		{".htaccess", false},
	}
	for _, tt := range tests {
		if got := isCoreRootFile(tt.naam); got != tt.want {
			t.Errorf("isCoreRootFile(%q) = %v, want %v", tt.naam, got, tt.want)
		}
	}
}

func TestReplaceCoreVervangtEnBehoudt(t *testing.T) {
	root := bouwWPRoot(t, map[string]string{
		// Core-dirs met oude inhoud, incl. een bestand dat in de nieuwe
		// versie niet meer bestaat.
		"wp-admin/admin.php":           "oud",
		"wp-admin/verwijderd-in-7.php": "oud",
		"wp-includes/version.php":      "$wp_version = '6.7.2';",
		// Core-rootbestanden.
		"wp-login.php": "oud",
		"index.php":    "oud",
		// Core-rootbestand dat in de nieuwe versie is verwijderd.
		"wp-links-opml.php": "oud",
		// Moet blijven staan:
		"wp-config.php":              "define('DB_NAME', 'geheim');",
		"custom-cronfile.php":        "custom",
		"robots.txt":                 "User-agent: *",
		"wp-content/themes/x/a.php":  "thema",
		"wp-content/plugins/y/b.php": "plugin",
	})

	zipData := bouwCoreZip(t, map[string]string{
		"wp-admin/admin.php":      "nieuw",
		"wp-admin/nieuw-in-7.php": "nieuw",
		"wp-includes/version.php": "$wp_version = '7.0.2';",
		"wp-login.php":            "nieuw",
		"index.php":               "nieuw",
		"readme.html":             "nieuw",
		"wp-config-sample.php":    "sample",
	})

	if err := replaceCore(zipData, root); err != nil {
		t.Fatalf("replaceCore: %v", err)
	}

	lees := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("lees %s: %v", rel, err)
		}
		return string(b)
	}
	bestaat := func(rel string) bool {
		_, err := os.Stat(filepath.Join(root, rel))
		return err == nil
	}

	// Nieuwe core is uitgepakt (zip-prefix wordpress/ gestript).
	if got := lees("wp-includes/version.php"); got != "$wp_version = '7.0.2';" {
		t.Errorf("version.php = %q, want nieuwe versie", got)
	}
	if got := lees("wp-login.php"); got != "nieuw" {
		t.Errorf("wp-login.php = %q, want nieuw", got)
	}
	if !bestaat("wp-admin/nieuw-in-7.php") {
		t.Error("nieuw core-bestand ontbreekt")
	}
	if !bestaat("readme.html") {
		t.Error("readme.html uit de zip ontbreekt")
	}

	// Verwijderde core-bestanden zijn opgeruimd.
	if bestaat("wp-admin/verwijderd-in-7.php") {
		t.Error("verwijderd core-bestand in wp-admin staat er nog")
	}
	if bestaat("wp-links-opml.php") {
		t.Error("verwijderd core-rootbestand staat er nog")
	}

	// Behouden bestanden.
	if got := lees("wp-config.php"); got != "define('DB_NAME', 'geheim');" {
		t.Errorf("wp-config.php is aangetast: %q", got)
	}
	if got := lees("custom-cronfile.php"); got != "custom" {
		t.Errorf("custom bestand aangetast: %q", got)
	}
	if !bestaat("robots.txt") {
		t.Error("robots.txt is verdwenen")
	}
	if !bestaat("wp-content/themes/x/a.php") || !bestaat("wp-content/plugins/y/b.php") {
		t.Error("wp-content is aangetast — mag nooit gebeuren")
	}
}

func TestReplaceCoreWeigertZipZonderCore(t *testing.T) {
	root := bouwWPRoot(t, map[string]string{"wp-includes/version.php": "oud"})
	// Zip zonder wp-includes/wp-admin: verdacht (verkeerde download, HTML-
	// foutpagina, ...). Niets mag verwijderd zijn.
	zipData := bouwCoreZip(t, map[string]string{"leesmij.txt": "geen core"})

	if err := replaceCore(zipData, root); err == nil {
		t.Fatal("replaceCore accepteerde een zip zonder core-mappen")
	}
	if _, err := os.Stat(filepath.Join(root, "wp-includes", "version.php")); err != nil {
		t.Errorf("bestaande core is aangetast bij een ongeldige zip: %v", err)
	}
}

func TestReplaceCoreWeigertOngeldigeZip(t *testing.T) {
	root := bouwWPRoot(t, map[string]string{"wp-includes/version.php": "oud"})
	if err := replaceCore([]byte("<html>404</html>"), root); err == nil {
		t.Fatal("replaceCore accepteerde niet-zip data")
	}
	if _, err := os.Stat(filepath.Join(root, "wp-includes", "version.php")); err != nil {
		t.Errorf("bestaande core is aangetast bij ongeldige data: %v", err)
	}
}

func TestReplaceCoreWeigertZipSlip(t *testing.T) {
	root := bouwWPRoot(t, map[string]string{"wp-admin/a.php": "x", "wp-includes/version.php": "x"})
	zipData := bouwCoreZip(t, map[string]string{
		"wp-admin/a.php":          "nieuw",
		"wp-includes/version.php": "nieuw",
		"../../ontsnapt.txt":      "kwaad",
	})
	if err := replaceCore(zipData, root); err == nil {
		t.Fatal("replaceCore accepteerde een pad buiten de doelmap")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(root)), "ontsnapt.txt")); err == nil {
		t.Error("bestand buiten de doelmap geschreven (zip slip)")
	}
}
