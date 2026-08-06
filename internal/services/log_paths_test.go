package services

import (
	"os"
	"path/filepath"
	"testing"
)

func maakBestand(t *testing.T, wortel, rel string) {
	t.Helper()
	pad := filepath.Join(wortel, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(pad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pad, []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWebrootRelatiefBeideKinstaVormen(t *testing.T) {
	tests := []struct {
		naam string
		in   string
		want string
	}{
		{
			"pad zonder web-tussenmap",
			"/www/voorbeeld_706/public/wp-content/themes/voorbeeld/inc/listing-card.php",
			"wp-content/themes/voorbeeld/inc/listing-card.php",
		},
		{
			"pad met web-tussenmap zoals in dezelfde log voorkomt",
			"/www/voorbeeld_706/web/public/wp-settings.php",
			"wp-settings.php",
		},
		{
			"zonder public maar met wp-content",
			"/www/voorbeeld_706/wp-content/plugins/x/y.php",
			"wp-content/plugins/x/y.php",
		},
		{
			"zonder public maar met wp-includes",
			"/srv/site/wp-includes/class-wp-hook.php",
			"wp-includes/class-wp-hook.php",
		},
		{
			"alleen het www-patroon",
			"/www/voorbeeld_706/iets/anders.php",
			"iets/anders.php",
		},
		{"leeg", "", ""},
		{"onbruikbaar", "relatief/pad.php", ""},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			if got := webrootRelatief(tt.in); got != tt.want {
				t.Errorf("webrootRelatief(%q) = %q, wil %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsCoreWebrootPad(t *testing.T) {
	core := []string{
		"wp-includes/class-wp-hook.php",
		"wp-admin/includes/plugin.php",
		"wp-settings.php",
		"wp-load.php",
		"wp-config.php",
		"xmlrpc.php",
		"index.php",
	}
	for _, p := range core {
		if !isCoreWebrootPad(p) {
			t.Errorf("%q hoort core te zijn", p)
		}
	}
	eigen := []string{
		"wp-content/themes/voorbeeld/functions.php",
		"wp-content/plugins/eigen-plugin/src/Widget.php",
		"wp-content/mu-plugins/iets.php",
		"",
	}
	for _, p := range eigen {
		if isCoreWebrootPad(p) {
			t.Errorf("%q hoort geen core te zijn", p)
		}
	}
}

func TestMapProdPathToRepoVindtThemabestand(t *testing.T) {
	wortel := t.TempDir()
	maakBestand(t, wortel, "public/wp-content/themes/voorbeeld/inc/listing-card.php")

	got := mapProdPathToRepo("/www/voorbeeld_706/public/wp-content/themes/voorbeeld/inc/listing-card.php", wortel)
	if !got.Bestaat {
		t.Fatalf("bestand niet gevonden: %+v", got)
	}
	if got.RepoPad != "public/wp-content/themes/voorbeeld/inc/listing-card.php" {
		t.Errorf("RepoPad = %q", got.RepoPad)
	}
	if got.WebrootPad != "wp-content/themes/voorbeeld/inc/listing-card.php" {
		t.Errorf("WebrootPad = %q", got.WebrootPad)
	}
	if got.IsCore {
		t.Error("een themabestand is geen core")
	}
}

// Het productiepad met /web/public/ moet ook op de lokale public/-layout landen:
// precies de inconsistentie die in het echte log zat.
func TestMapProdPathToRepoWebPublicNaarPublic(t *testing.T) {
	wortel := t.TempDir()
	maakBestand(t, wortel, "public/wp-settings.php")

	got := mapProdPathToRepo("/www/voorbeeld_706/web/public/wp-settings.php", wortel)
	if !got.Bestaat {
		t.Fatalf("bestand niet gevonden: %+v", got)
	}
	if got.RepoPad != "public/wp-settings.php" {
		t.Errorf("RepoPad = %q", got.RepoPad)
	}
	if !got.IsCore {
		t.Error("wp-settings.php hoort als core gemarkeerd te worden")
	}
}

func TestMapProdPathToRepoWebLayout(t *testing.T) {
	wortel := t.TempDir()
	maakBestand(t, wortel, "web/wp-content/plugins/p/p.php")

	got := mapProdPathToRepo("/www/voorbeeld_706/public/wp-content/plugins/p/p.php", wortel)
	if !got.Bestaat || got.RepoPad != "web/wp-content/plugins/p/p.php" {
		t.Errorf("got = %+v", got)
	}
}

func TestMapProdPathToRepoOnbekendBestand(t *testing.T) {
	wortel := t.TempDir()
	got := mapProdPathToRepo("/www/voorbeeld_706/public/wp-content/plugins/weg/weg.php", wortel)
	if got.Bestaat {
		t.Error("een niet-bestaand bestand werd als gevonden gemeld")
	}
	if got.WebrootPad == "" {
		t.Error("WebrootPad hoort ook zonder treffer gevuld te zijn")
	}
	if got.RepoPad != "" {
		t.Errorf("RepoPad = %q, wil leeg", got.RepoPad)
	}
}

// TestMapProdPathToRepoWeigertPadOntsnapping is de vangrail: een logregel is
// onvertrouwde invoer, dus een pad met .. mag nooit buiten de checkout uitkomen.
func TestMapProdPathToRepoWeigertPadOntsnapping(t *testing.T) {
	wortel := t.TempDir()
	buiten := filepath.Join(wortel, "..", "geheim.php")
	if err := os.WriteFile(buiten, []byte("<?php // geheim"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(buiten)

	for _, prod := range []string{
		"/www/voorbeeld_706/public/../../geheim.php",
		"/www/voorbeeld_706/public/wp-content/../../../geheim.php",
		"/www/voorbeeld_706/public/../../../../../../etc/passwd",
	} {
		got := mapProdPathToRepo(prod, wortel)
		if got.Bestaat {
			t.Errorf("pad %q ontsnapte uit de checkout: %+v", prod, got)
		}
	}
}

func TestMapProdPathToRepoNegeertMappen(t *testing.T) {
	wortel := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wortel, "public", "wp-content", "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := mapProdPathToRepo("/www/voorbeeld_706/public/wp-content/uploads/", wortel)
	if got.Bestaat {
		t.Error("een map mag niet als bestand gelden")
	}
}

func TestBinnenWortel(t *testing.T) {
	wortel := "/tmp/repo"
	if !binnenWortel(wortel, "/tmp/repo/public/x.php") {
		t.Error("pad binnen de wortel werd geweigerd")
	}
	if binnenWortel(wortel, "/tmp/andere/x.php") {
		t.Error("pad buiten de wortel werd toegelaten")
	}
	if binnenWortel(wortel, "/tmp/repo/../x.php") {
		t.Error("ontsnapping via .. werd toegelaten")
	}
}
