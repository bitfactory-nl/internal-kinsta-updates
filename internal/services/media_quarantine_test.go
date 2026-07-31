package services

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// quarantaineUitvoer bouwt serveruitvoer zoals het quarantainescript die geeft.
func quarantaineUitvoer(t *testing.T, payload map[string]any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return "RDM-ROOT:/www/site_1/public\n" +
		quarantineSentinelStart + "\n" +
		base64.StdEncoding.EncodeToString(buf.Bytes()) + "\n" +
		quarantineSentinelEnd + "\n"
}

// scanMetRijen slaat een scan op met de opgegeven detailregels, zodat de
// quarantainepoorten iets hebben om tegen te controleren.
func scanMetRijen(t *testing.T, svc *MediaService, scanID string, aanpas func(*domain.MediaScanSummary), rijen []domain.MediaFileRow) {
	t.Helper()
	sum := domain.MediaScanSummary{
		ID:        scanID,
		ProjectID: "p1",
		ScannedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
		Scope: domain.MediaScanScope{
			UploadsPath:           "/www/site_1/public/wp-content/uploads",
			ReferenceScanComplete: true,
		},
	}
	if aanpas != nil {
		aanpas(&sum)
	}
	if err := svc.store.Save(sum, rijen); err != nil {
		t.Fatalf("scan opslaan: %v", err)
	}
}

// oudeRij is een regel die ruim buiten de leeftijdsdrempel valt.
func oudeRij(pad string, cat domain.MediaCategory) domain.MediaFileRow {
	return domain.MediaFileRow{
		Path:       pad,
		Bytes:      1000,
		ModifiedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
		Category:   cat,
	}
}

func TestQuarantaineVerplaatstAlleenVeiligeCategorieën(t *testing.T) {
	runner := &fakeSSHRunner{uit: quarantaineUitvoer(t, map[string]any{
		"batch": "20260731-140000",
		"moved": []map[string]any{{"original": "2020/01/oud.jpg", "stored": "20260731-140000/2020/01/oud.jpg", "bytes": 1000}},
		"bytes": 1000,
	})}
	svc, _ := newMediaService(t, runner, t.TempDir())
	scanMetRijen(t, svc, "s1", nil, []domain.MediaFileRow{
		oudeRij("2020/01/oud.jpg", domain.MediaUnreferenced),
		oudeRij("2020/01/inbeeld.jpg", domain.MediaInUse),
		oudeRij("2020/01/weg.jpg", domain.MediaMissingFile),
	})

	res, err := svc.QuarantineFiles("p1", "env-1", "s1",
		[]string{"2020/01/oud.jpg", "2020/01/inbeeld.jpg", "2020/01/weg.jpg"}, 90)
	if err != nil {
		t.Fatalf("QuarantineFiles: %v", err)
	}

	// Alleen het ongebruikte bestand mag mee in de opdracht naar de server.
	if !strings.Contains(runner.laatste, "wp eval-file") {
		t.Fatalf("commando ziet er niet uit als een quarantaine-actie:\n%s", runner.laatste)
	}
	verstuurd := opdrachtUitCommando(t, runner.laatste)
	if len(verstuurd.Paths) != 1 || verstuurd.Paths[0] != "2020/01/oud.jpg" {
		t.Errorf("naar de server gestuurd: %v; wil alleen het ongebruikte bestand", verstuurd.Paths)
	}
	if verstuurd.Action != "quarantine" {
		t.Errorf("actie = %q", verstuurd.Action)
	}

	redenen := map[string]string{}
	for _, sk := range res.Skipped {
		redenen[sk.Path] = sk.Reason
	}
	if !strings.Contains(redenen["2020/01/inbeeld.jpg"], "niet als ongebruikt") {
		t.Errorf("een bestand dat in gebruik is moet geweigerd worden, kreeg: %q", redenen["2020/01/inbeeld.jpg"])
	}
	if _, ok := redenen["2020/01/weg.jpg"]; !ok {
		t.Error("een attachment zonder bestand hoort niet verplaatst te worden")
	}
}

func TestQuarantaineWeigertOnbekendPad(t *testing.T) {
	runner := &fakeSSHRunner{}
	svc, _ := newMediaService(t, runner, t.TempDir())
	scanMetRijen(t, svc, "s1", nil, []domain.MediaFileRow{oudeRij("2020/01/oud.jpg", domain.MediaUnreferenced)})

	// Een pad dat niet in de scan voorkomt: hier zou traversal of een verouderde
	// selectie binnenkomen, en dat mag de server nooit bereiken.
	_, err := svc.QuarantineFiles("p1", "env-1", "s1", []string{"../../wp-config.php"}, 90)
	if err == nil {
		t.Fatal("wil een fout voor een pad buiten de scan")
	}
	if runner.calls != 0 {
		t.Error("er mag geen SSH-opdracht uitgaan als er niets in aanmerking komt")
	}
}

func TestQuarantaineWeigertJongeBestanden(t *testing.T) {
	runner := &fakeSSHRunner{}
	svc, _ := newMediaService(t, runner, t.TempDir())
	vers := domain.MediaFileRow{
		Path:       "2026/07/vers.jpg",
		ModifiedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC).Unix(),
		Category:   domain.MediaUnreferenced,
	}
	scanMetRijen(t, svc, "s1", nil, []domain.MediaFileRow{vers})

	_, err := svc.QuarantineFiles("p1", "env-1", "s1", []string{"2026/07/vers.jpg"}, 90)
	if err == nil {
		t.Fatal("wil een fout: verse uploads zijn het risico niet waard")
	}
	if runner.calls != 0 {
		t.Error("er mag geen SSH-opdracht uitgaan")
	}
}

func TestQuarantaineWeigertOngeschikteScan(t *testing.T) {
	gevallen := []struct {
		naam   string
		aanpas func(*domain.MediaScanSummary)
		wil    string
	}{
		{"afgekapte scan", func(s *domain.MediaScanSummary) { s.Scope.Truncated = true }, "afgekapt"},
		{"onvolledige referentiescan", func(s *domain.MediaScanSummary) { s.Scope.ReferenceScanComplete = false }, "referentiescan"},
		{"offload-plugin", func(s *domain.MediaScanSummary) { s.Scope.OffloadDetected = true }, "offload"},
	}
	for _, g := range gevallen {
		t.Run(g.naam, func(t *testing.T) {
			runner := &fakeSSHRunner{}
			svc, _ := newMediaService(t, runner, t.TempDir())
			scanMetRijen(t, svc, "s1", g.aanpas, []domain.MediaFileRow{oudeRij("2020/01/oud.jpg", domain.MediaUnreferenced)})

			_, err := svc.QuarantineFiles("p1", "env-1", "s1", []string{"2020/01/oud.jpg"}, 90)
			if err == nil {
				t.Fatalf("wil een fout bij %s", g.naam)
			}
			if !strings.Contains(err.Error(), g.wil) {
				t.Errorf("foutmelding = %q; wil iets over %q", err, g.wil)
			}
			if runner.calls != 0 {
				t.Error("er mag geen SSH-opdracht uitgaan")
			}
		})
	}
}

func TestQuarantaineHerstel(t *testing.T) {
	runner := &fakeSSHRunner{uit: quarantaineUitvoer(t, map[string]any{
		"batch": "20260731-140000",
		"moved": []map[string]any{{"original": "2020/01/oud.jpg", "bytes": 1000}},
		"bytes": 1000,
	})}
	svc, _ := newMediaService(t, runner, t.TempDir())

	res, err := svc.RestoreQuarantine("p1", "env-1", "20260731-140000")
	if err != nil {
		t.Fatalf("RestoreQuarantine: %v", err)
	}
	if len(res.Moved) != 1 {
		t.Errorf("teruggezet = %+v", res.Moved)
	}
	verstuurd := opdrachtUitCommando(t, runner.laatste)
	if verstuurd.Action != "restore" || verstuurd.Batch != "20260731-140000" {
		t.Errorf("opdracht = %+v", verstuurd)
	}
}

func TestQuarantaineLijst(t *testing.T) {
	runner := &fakeSSHRunner{uit: quarantaineUitvoer(t, map[string]any{
		"action": "list",
		"batches": []map[string]any{
			{"batch": "20260731-140000", "created": "2026-07-31T14:00:00+00:00", "files": 12, "bytes": 4096},
		},
	})}
	svc, _ := newMediaService(t, runner, t.TempDir())

	batches, err := svc.ListQuarantine("p1", "env-1")
	if err != nil {
		t.Fatalf("ListQuarantine: %v", err)
	}
	if len(batches) != 1 || batches[0].Files != 12 {
		t.Errorf("batches = %+v", batches)
	}
}

// opdrachtUitCommando haalt de meegestuurde opdracht weer uit het shell-commando,
// zodat de test kan controleren wat de server werkelijk zou krijgen.
func opdrachtUitCommando(t *testing.T, cmd string) quarantineOpdracht {
	t.Helper()
	const marker = "RDM_Q_INPUT="
	i := strings.Index(cmd, marker)
	if i < 0 {
		t.Fatalf("geen opdracht in het commando:\n%s", cmd)
	}
	rest := cmd[i+len(marker):]
	if eind := strings.IndexAny(rest, " \n"); eind >= 0 {
		rest = rest[:eind]
	}
	data, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		t.Fatalf("opdracht niet leesbaar: %v", err)
	}
	var o quarantineOpdracht
	if err := json.Unmarshal(data, &o); err != nil {
		t.Fatalf("opdracht parsen: %v", err)
	}
	return o
}

func TestQuarantaineWeigertWatDeCrawlZag(t *testing.T) {
	runner := &fakeSSHRunner{}
	svc, _ := newMediaService(t, runner, t.TempDir())
	scanMetRijen(t, svc, "s1", nil, []domain.MediaFileRow{
		oudeRij("2020/01/oud.jpg", domain.MediaUnreferenced),
	})

	// De databasescan noemde dit bestand ongebruikt, maar een crawl zag de browser
	// het opvragen. Waarnemen weegt zwaarder dan afleiden.
	if err := svc.store.SaveCrawl("p1", "s1", MediaCrawlResult{
		ScanID: "s1",
		Pages:  map[string][]string{"2020/01/oud.jpg": {"https://klant.nl/over-ons"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.QuarantineFiles("p1", "env-1", "s1", []string{"2020/01/oud.jpg"}, 90)
	if err == nil {
		t.Fatal("wil een fout: dit bestand wordt op de site geladen")
	}
	if runner.calls != 0 {
		t.Error("er mag geen SSH-opdracht uitgaan")
	}

	// En de reden moet ondubbelzinnig zijn.
	rijen, derr := svc.ScanDetail("p1", "s1", domain.MediaUnreferenced, "", 0, 10)
	if derr != nil {
		t.Fatalf("ScanDetail: %v", derr)
	}
	if len(rijen) != 1 || len(rijen[0].Evidence) == 0 || rijen[0].Evidence[0] != domain.EvidenceRendered {
		t.Errorf("rij = %+v; wil bewijs 'rendered' bij de regel", rijen)
	}
}
