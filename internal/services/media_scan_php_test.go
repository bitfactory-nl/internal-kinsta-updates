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
	schrijf("2023/01/oud.jpg", 700)          // andere maand: valt buiten een selectie op 2024/05
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
			{"ID": 5, "post_title": "Oud", "post_mime_type": "image/jpeg", "post_date_gmt": "2023-01-09 10:00:00", "file": "2023/01/oud.jpg"},
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

	out := draaiHarness(t, php, fixturePad, nil)

	payload, err := parseMediaScanOutput(out)
	if err != nil {
		t.Fatalf("parseMediaScanOutput: %v\nuitvoer:\n%s", err, out)
	}
	sum, detail := payload.summary("s1", "p1", "web-test", "live", timeNulpunt(), 0)

	if sum.TotalFiles != 7 {
		t.Errorf("TotalFiles = %d, wil 7", sum.TotalFiles)
	}
	if sum.TotalBytes != 3150 {
		t.Errorf("TotalBytes = %d, wil 3150", sum.TotalBytes)
	}
	if sum.AttachmentCount != 5 {
		t.Errorf("AttachmentCount = %d, wil 5", sum.AttachmentCount)
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
	if ongebruikt.Files != 2 {
		t.Errorf("categorie C = %+v; wil attachment 2 en 5", ongebruikt)
	}
	if ongebruikt.Hard {
		t.Error("categorie C is een heuristiek en mag nooit als hard feit gelden")
	}
	if ongebruikt.Bytes != 1200 {
		t.Errorf("categorie C bytes = %d, wil 1200 (grootte uit de bestandsdoorloop)", ongebruikt.Bytes)
	}

	if len(detail) != 6 {
		t.Errorf("detailregels = %d, wil 6 (2 in gebruik, 2 zonder referentie, 1 zwerver, 1 ontbrekend)", len(detail))
	}

	// De gebruikte kant moet er óók staan, met het bewijs erbij: zonder "waar is dit
	// gevonden" valt een lijst niet na te lopen.
	inGebruik := categorie(t, sum, domain.MediaInUse)
	if inGebruik.Files != 2 || len(inGebruik.Samples) != 2 {
		t.Fatalf("categorie in gebruik = %+v; wil attachment 1 en 4", inGebruik)
	}
	if !inGebruik.Hard {
		t.Error("een gevonden referentie is positief bewijs en dus een hard feit")
	}
	bewijzen := map[int]string{}
	for _, r := range inGebruik.Samples {
		if len(r.Evidence) == 0 {
			t.Errorf("rij %s heeft geen bewijs", r.Path)
		}
		bewijzen[r.AttachmentID] = string(r.Evidence[0])
	}
	if bewijzen[1] != "content" {
		t.Errorf("attachment 1 bewijs = %q, wil content", bewijzen[1])
	}
	if bewijzen[4] != "meta" {
		t.Errorf("attachment 4 bewijs = %q, wil meta (_thumbnail_id)", bewijzen[4])
	}

	// En de tellers moeten laten zien dat er werkelijk content is doorzocht.
	if sum.Scope.RowsScanned["posts"] < 1 || sum.Scope.RowsScanned["postmeta"] < 1 {
		t.Errorf("RowsScanned = %v; wil doorzochte rijen per bron", sum.Scope.RowsScanned)
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

// draaiHarness voert het scanscript uit via de stub-WordPress en geeft de ruwe
// uitvoer terug. Met mappen wordt een gerichte scan gedraaid.
func draaiHarness(t *testing.T, php, fixturePad string, mappen []string) string {
	t.Helper()
	cmd := exec.Command(php, filepath.Join("testdata", "media_scan_harness.php"))
	cmd.Env = append(os.Environ(), "RDM_TEST_FIXTURE="+fixturePad)
	if len(mappen) > 0 {
		cmd.Env = append(cmd.Env, "RDM_MEDIA_FOLDERS="+base64.StdEncoding.EncodeToString([]byte(strings.Join(mappen, "\n"))))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness: %v\n%s", err, out)
	}
	return string(out)
}

func TestMediaScanPHPAlleenGekozenMappen(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php niet beschikbaar")
	}
	_, fixturePad := mediaFixtureTree(t)

	payload, err := parseMediaScanOutput(draaiHarness(t, php, fixturePad, []string{"2024/05"}))
	if err != nil {
		t.Fatalf("parseMediaScanOutput: %v", err)
	}
	sum, _ := payload.summary("s2", "p1", "web-test", "live", timeNulpunt(), 0)

	// Alleen de vijf bestanden uit 2024/05: niet 2023/01 en niet de plugin-cache.
	if sum.TotalFiles != 5 {
		t.Errorf("TotalFiles = %d, wil 5 (alleen 2024/05)", sum.TotalFiles)
	}
	if sum.TotalBytes != 2400 {
		t.Errorf("TotalBytes = %d, wil 2400", sum.TotalBytes)
	}
	// En de index is óók beperkt: attachment 5 zit in 2023/01 en hoort er buiten.
	if sum.AttachmentCount != 4 {
		t.Errorf("AttachmentCount = %d, wil 4; de LIKE-filter op de index werkt niet", sum.AttachmentCount)
	}
	for _, blok := range sum.Categories {
		for _, r := range blok.Samples {
			if strings.HasPrefix(r.Path, "2023/") {
				t.Errorf("categorie %s bevat %s, buiten de selectie", blok.Category, r.Path)
			}
		}
	}
	if len(sum.Scope.Folders) != 1 || sum.Scope.Folders[0] != "2024/05" {
		t.Errorf("Scope.Folders = %v, wil [2024/05]", sum.Scope.Folders)
	}
}
