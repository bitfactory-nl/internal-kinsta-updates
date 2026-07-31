package services

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// TestMediaScanPHPIsReadOnly houdt vast dat het scanscript niets kan wijzigen.
// Dit blijft gelden als iemand later "even snel" een opruimfunctie toevoegt.
func TestMediaScanPHPIsReadOnly(t *testing.T) {
	data, err := os.ReadFile("media_scan.php")
	if err != nil {
		t.Fatalf("lees scanscript: %v", err)
	}
	verboden := regexp.MustCompile(`(?i)\b(unlink|rmdir|rename|file_put_contents|fputs|wp_delete_\w+|wp_update_\w+|\$wpdb->(query|update|insert|delete|replace))\b|\b(DELETE\s+FROM|UPDATE\s+\w+\s+SET|INSERT\s+INTO|TRUNCATE|DROP\s+TABLE|ALTER\s+TABLE)\b`)
	if m := verboden.FindAll(zonderCommentaar(data), -1); m != nil {
		var namen []string
		for _, hit := range m {
			namen = append(namen, string(hit))
		}
		t.Errorf("scanscript bevat schrijfbewerkingen: %s", strings.Join(namen, ", "))
	}
}

// zonderCommentaar haalt PHP-commentaar weg, zodat de guard op code let en niet op
// een regel die juist uitlegt dat het script niets verwijdert.
func zonderCommentaar(src []byte) []byte {
	blok := regexp.MustCompile(`(?s)/\*.*?\*/`)
	regel := regexp.MustCompile(`(?m)(^|\s)(//|#).*$`)
	return regel.ReplaceAll(blok.ReplaceAll(src, nil), nil)
}

// mediaFixtureTree zet een uploads-map neer die alle categorieën raakt en levert
// de bijbehorende fixture voor de stub-WordPress.
func mediaFixtureTree(t *testing.T) (uploadsDir, fixturePad string) {
	t.Helper()
	root := t.TempDir()
	content := filepath.Join(root, "wp-content")
	uploads := filepath.Join(content, "uploads")

	schrijf := func(rel string, bytes int) {
		p := filepath.Join(uploads, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, bytes), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	schrijf("2024/05/foto.jpg", 1000)        // in de bibliotheek én gebruikt
	schrijf("2024/05/foto-300x200.jpg", 200) // gegenereerd formaat
	schrijf("2024/05/ongebruikt.jpg", 500)   // in de bibliotheek, nergens gebruikt
	schrijf("2024/05/zwerver.jpg", 300)      // niet in de bibliotheek
	schrijf("2024/05/uitgelicht.jpg", 400)   // alleen via _thumbnail_id in gebruik
	schrijf("cache/rommel.css", 50)          // plugin-rommel
	// 2024/05/weg.jpg staat bewust NIET op schijf: dat is categorie B.

	fixture := map[string]any{
		"uploads": uploads,
		"content": content,
		"attachments": []map[string]any{
			{"ID": 1, "post_title": "Foto", "post_mime_type": "image/jpeg", "post_date_gmt": "2024-05-01 10:00:00", "file": "2024/05/foto.jpg"},
			{"ID": 2, "post_title": "Ongebruikt", "post_mime_type": "image/jpeg", "post_date_gmt": "2024-05-02 10:00:00", "file": "2024/05/ongebruikt.jpg"},
			{"ID": 3, "post_title": "Weg", "post_mime_type": "image/jpeg", "post_date_gmt": "2024-05-03 10:00:00", "file": "2024/05/weg.jpg"},
			{"ID": 4, "post_title": "Uitgelicht", "post_mime_type": "image/jpeg", "post_date_gmt": "2024-05-04 10:00:00", "file": "2024/05/uitgelicht.jpg"},
		},
		"attachmentMeta": []map[string]any{
			{"meta_id": 1, "post_id": 1, "meta_value": `a:1:{s:5:"sizes";a:1:{s:6:"medium";a:1:{s:4:"file";s:16:"foto-300x200.jpg";}}}`},
		},
		"posts": []map[string]any{
			{"ID": 10, "post_type": "page", "post_status": "publish",
				"post_content": `<p>Hallo</p><img class="wp-image-1" src="https://voorbeeld.test/wp-content/uploads/2024/05/foto.jpg">`,
				"post_excerpt": ""},
		},
		"postmeta": []map[string]any{
			{"meta_id": 1, "meta_key": "_yoast_wpseo_title", "meta_value": "Titel zonder media"},
			// De uitgelichte afbeelding: alleen een ID in meta, nergens een URL.
			{"meta_id": 2, "meta_key": "_thumbnail_id", "meta_value": "4"},
		},
		"options":  []map[string]any{{"option_id": 1, "option_name": "blogname", "option_value": "Voorbeeld"}},
		"termmeta": []map[string]any{},
		"usermeta": []map[string]any{},
		"offload":  0,
	}

	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixturePad = filepath.Join(root, "fixture.json")
	if err := os.WriteFile(fixturePad, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return uploads, fixturePad
}

// categorie zoekt één categorieblok op in de samenvatting.
func categorie(t *testing.T, sum domain.MediaScanSummary, c domain.MediaCategory) domain.MediaCategoryResult {
	t.Helper()
	for _, blok := range sum.Categories {
		if blok.Category == c {
			return blok
		}
	}
	t.Fatalf("categorie %q ontbreekt in %+v", c, sum.Categories)
	return domain.MediaCategoryResult{}
}

// klasse zoekt de totalen van één bestandsklasse op.
func klasse(sum domain.MediaScanSummary, c domain.MediaFileClass) domain.MediaClassTotals {
	for _, t := range sum.ByClass {
		if t.Class == c {
			return t
		}
	}
	return domain.MediaClassTotals{}
}

func TestMediaScanPHPTegenStubWordpress(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php niet beschikbaar")
	}
	_, fixturePad := mediaFixtureTree(t)

	cmd := exec.Command(php, filepath.Join("testdata", "media_scan_harness.php"))
	cmd.Env = append(os.Environ(), "RDM_TEST_FIXTURE="+fixturePad)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness: %v\n%s", err, out)
	}

	payload, err := parseMediaScanOutput(string(out))
	if err != nil {
		t.Fatalf("parseMediaScanOutput: %v\nuitvoer:\n%s", err, out)
	}
	sum, detail := payload.summary("s1", "p1", "web-test", "live", timeNulpunt(), 0)

	if sum.TotalFiles != 6 {
		t.Errorf("TotalFiles = %d, wil 6", sum.TotalFiles)
	}
	if sum.TotalBytes != 2450 {
		t.Errorf("TotalBytes = %d, wil 2450", sum.TotalBytes)
	}
	if sum.AttachmentCount != 4 {
		t.Errorf("AttachmentCount = %d, wil 4", sum.AttachmentCount)
	}
	if sum.ReferencedCount != 2 {
		t.Errorf("ReferencedCount = %d, wil 2: foto.jpg via de content en uitgelicht.jpg via _thumbnail_id", sum.ReferencedCount)
	}

	if g := klasse(sum, domain.MediaClassGenerated); g.Files != 1 || g.Bytes != 200 {
		t.Errorf("gegenereerd = %+v, wil 1 bestand van 200 bytes", g)
	}
	if s := klasse(sum, domain.MediaClassSystem); s.Files != 1 {
		t.Errorf("systeem = %+v, wil 1 bestand (cache/rommel.css)", s)
	}

	zwerver := categorie(t, sum, domain.MediaOrphanFile)
	if zwerver.Files != 1 || len(zwerver.Samples) != 1 || zwerver.Samples[0].Path != "2024/05/zwerver.jpg" {
		t.Errorf("categorie A = %+v; wil alleen zwerver.jpg (niet het thumbnail, niet de plugin-cache)", zwerver)
	}
	if !zwerver.Hard {
		t.Error("categorie A moet als hard feit gelden")
	}

	weg := categorie(t, sum, domain.MediaMissingFile)
	if weg.Files != 1 || len(weg.Samples) != 1 || weg.Samples[0].AttachmentID != 3 {
		t.Errorf("categorie B = %+v; wil attachment 3", weg)
	}

	ongebruikt := categorie(t, sum, domain.MediaUnreferenced)
	if ongebruikt.Files != 1 || len(ongebruikt.Samples) != 1 || ongebruikt.Samples[0].AttachmentID != 2 {
		t.Errorf("categorie C = %+v; wil attachment 2", ongebruikt)
	}
	if ongebruikt.Hard {
		t.Error("categorie C is een heuristiek en mag nooit als hard feit gelden")
	}
	if ongebruikt.Bytes != 500 {
		t.Errorf("categorie C bytes = %d, wil 500 (grootte uit de bestandsdoorloop)", ongebruikt.Bytes)
	}

	if len(detail) != 3 {
		t.Errorf("detailregels = %d, wil 3 (één per categorie)", len(detail))
	}
	if sum.Scope.UploadsPath == "" || sum.Scope.Truncated {
		t.Errorf("scope = %+v; wil een pad en geen afgekapte scan", sum.Scope)
	}
	if len(sum.Scope.TablesScanned) == 0 {
		t.Error("scope noemt geen gescande tabellen")
	}
}

// timeNulpunt geeft een vaste tijd, zodat tests niet van de klok afhangen.
func timeNulpunt() time.Time {
	return time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
}
