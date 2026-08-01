package services

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// quarantaineBoom zet een uploads-map neer met een origineel plus al zijn
// gegenereerde formaten, en een tweede bestand dat er niets mee te maken heeft.
func quarantaineBoom(t *testing.T) (root, uploads, fixturePad string) {
	t.Helper()
	root = t.TempDir()
	content := filepath.Join(root, "wp-content")
	uploads = filepath.Join(content, "uploads")

	schrijf := func(rel string, n int) {
		p := filepath.Join(uploads, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	schrijf("2020/01/foto.jpg", 1000)
	schrijf("2020/01/foto-300x200.jpg", 200)
	schrijf("2020/01/foto-scaled.jpg", 700)
	schrijf("2020/01/foto.jpg.webp", 150)
	schrijf("2020/01/fotoboek.jpg", 500) // lijkt erop, maar is een ander bestand
	schrijf("2020/01/ander.jpg", 300)

	// Een bestand buiten uploads dat onder geen enkele omstandigheid mag bewegen.
	if err := os.WriteFile(filepath.Join(content, "wp-config.php"), []byte("<?php // geheim"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixture := map[string]any{"uploads": uploads, "content": content}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixturePad = filepath.Join(root, "fixture.json")
	if err := os.WriteFile(fixturePad, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, uploads, fixturePad
}

// draaiQuarantaine voert het quarantainescript uit met één opdracht.
func draaiQuarantaine(t *testing.T, php, fixturePad, base string, opdracht quarantineOpdracht) QuarantineResult {
	t.Helper()
	data, err := json.Marshal(opdracht)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(php, filepath.Join("testdata", "media_quarantine_harness.php"))
	cmd.Env = append(os.Environ(),
		"RDM_TEST_FIXTURE="+fixturePad,
		"RDM_Q_BASE="+base,
		"RDM_Q_INPUT="+base64.StdEncoding.EncodeToString(data),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness: %v\n%s", err, out)
	}
	res, err := parseQuarantineOutput(string(out))
	if err != nil {
		t.Fatalf("parseQuarantineOutput: %v\nuitvoer:\n%s", err, out)
	}
	return res
}

func bestaat(pad string) bool {
	_, err := os.Stat(pad)
	return err == nil
}

func TestQuarantainePHPVerplaatstMetFormatenEnZetTerug(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php niet beschikbaar")
	}
	root, uploads, fixturePad := quarantaineBoom(t)
	base := filepath.Join(root, "rdm-quarantine")

	res := draaiQuarantaine(t, php, fixturePad, base, quarantineOpdracht{
		Action: "quarantine", Batch: "b1", Paths: []string{"2020/01/foto.jpg"},
	})

	// Het origineel én zijn formaten zijn weg uit uploads; alleen het origineel
	// verplaatsen zou de thumbnails laten staan en dus niets opleveren.
	for _, weg := range []string{"2020/01/foto.jpg", "2020/01/foto-300x200.jpg", "2020/01/foto-scaled.jpg", "2020/01/foto.jpg.webp"} {
		if bestaat(filepath.Join(uploads, weg)) {
			t.Errorf("%s staat nog in uploads", weg)
		}
		if !bestaat(filepath.Join(base, "b1", weg)) {
			t.Errorf("%s staat niet in quarantaine", weg)
		}
	}
	// Een bestand met een langere naam die met dezelfde stam begint hoort te blijven.
	if !bestaat(filepath.Join(uploads, "2020/01/fotoboek.jpg")) {
		t.Error("fotoboek.jpg is meegenomen; dat is een ander bestand")
	}
	if !bestaat(filepath.Join(uploads, "2020/01/ander.jpg")) {
		t.Error("ander.jpg is meegenomen")
	}
	if len(res.Moved) != 4 {
		t.Errorf("verplaatst = %d bestanden, wil 4: %+v", len(res.Moved), res.Moved)
	}
	if res.Bytes != 2050 {
		t.Errorf("bytes = %d, wil 2050", res.Bytes)
	}
	if !bestaat(filepath.Join(base, "b1", "manifest.json")) {
		t.Fatal("geen manifest weggeschreven; zonder manifest is terugzetten onmogelijk")
	}

	// Terugzetten moet alles weer op zijn plek brengen.
	herstel := draaiQuarantaine(t, php, fixturePad, base, quarantineOpdracht{Action: "restore", Batch: "b1"})
	if len(herstel.Moved) != 4 {
		t.Errorf("teruggezet = %d, wil 4: %+v", len(herstel.Moved), herstel.Moved)
	}
	for _, terug := range []string{"2020/01/foto.jpg", "2020/01/foto-300x200.jpg", "2020/01/foto-scaled.jpg", "2020/01/foto.jpg.webp"} {
		if !bestaat(filepath.Join(uploads, terug)) {
			t.Errorf("%s is niet teruggezet", terug)
		}
	}
}

func TestQuarantainePHPWeigertPadBuitenUploads(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php niet beschikbaar")
	}
	root, _, fixturePad := quarantaineBoom(t)
	base := filepath.Join(root, "rdm-quarantine")
	configPad := filepath.Join(root, "wp-content", "wp-config.php")

	res := draaiQuarantaine(t, php, fixturePad, base, quarantineOpdracht{
		Action: "quarantine", Batch: "b1",
		Paths: []string{"../wp-config.php", "/etc/hosts", "2020/01/../../wp-config.php"},
	})

	if len(res.Moved) != 0 {
		t.Errorf("er is iets verplaatst: %+v", res.Moved)
	}
	if len(res.Skipped) != 3 {
		t.Errorf("overgeslagen = %+v, wil alle drie geweigerd", res.Skipped)
	}
	if !bestaat(configPad) {
		t.Fatal("wp-config.php is verdwenen; padcontrole werkt niet")
	}
}

func TestQuarantainePHPZetNietTerugOverEenNieuwBestand(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php niet beschikbaar")
	}
	root, uploads, fixturePad := quarantaineBoom(t)
	base := filepath.Join(root, "rdm-quarantine")

	draaiQuarantaine(t, php, fixturePad, base, quarantineOpdracht{
		Action: "quarantine", Batch: "b1", Paths: []string{"2020/01/ander.jpg"},
	})

	// Iemand uploadt in de tussentijd een nieuw bestand met dezelfde naam.
	nieuw := filepath.Join(uploads, "2020/01/ander.jpg")
	if err := os.WriteFile(nieuw, []byte("nieuwe inhoud"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := draaiQuarantaine(t, php, fixturePad, base, quarantineOpdracht{Action: "restore", Batch: "b1"})
	if len(res.Moved) != 0 {
		t.Errorf("er is teruggezet over een nieuw bestand heen: %+v", res.Moved)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "weer een bestand") {
		t.Errorf("overgeslagen = %+v; wil een duidelijke reden", res.Skipped)
	}
	inhoud, err := os.ReadFile(nieuw)
	if err != nil || string(inhoud) != "nieuwe inhoud" {
		t.Errorf("het nieuwe bestand is overschreven: %q, %v", inhoud, err)
	}
}

func TestQuarantainePHPLijst(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php niet beschikbaar")
	}
	root, _, fixturePad := quarantaineBoom(t)
	base := filepath.Join(root, "rdm-quarantine")

	draaiQuarantaine(t, php, fixturePad, base, quarantineOpdracht{
		Action: "quarantine", Batch: "b1", Paths: []string{"2020/01/ander.jpg"},
	})
	res := draaiQuarantaine(t, php, fixturePad, base, quarantineOpdracht{Action: "list"})

	if len(res.Batches) != 1 || res.Batches[0].Batch != "b1" || res.Batches[0].Files != 1 {
		t.Errorf("batches = %+v", res.Batches)
	}
}

// TestQuarantainePHPVerwijdertNooit houdt vast dat het script geen verwijderfuncties
// gebruikt: verplaatsen is omkeerbaar, verwijderen niet, en dat verschil is de hele
// reden dat deze aanpak verantwoord is.
func TestQuarantainePHPVerwijdertNooit(t *testing.T) {
	data, err := os.ReadFile("media_quarantine.php")
	if err != nil {
		t.Fatalf("lees script: %v", err)
	}
	verboden := regexp.MustCompile(`(?i)\b(unlink|rmdir|wp_delete_\w+|\$wpdb->(query|update|insert|delete|replace))\b|\b(DELETE\s+FROM|UPDATE\s+\w+\s+SET|TRUNCATE|DROP\s+TABLE)\b`)
	if m := verboden.FindAll(zonderCommentaar(data), -1); m != nil {
		var namen []string
		for _, hit := range m {
			namen = append(namen, string(hit))
		}
		t.Errorf("quarantainescript bevat verwijderbewerkingen: %s", strings.Join(namen, ", "))
	}
}
