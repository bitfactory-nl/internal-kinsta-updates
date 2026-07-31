package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

func mediaSummary(id string, at time.Time) domain.MediaScanSummary {
	return domain.MediaScanSummary{
		ID:          id,
		ProjectID:   "p1",
		ProjectName: "web-vanluykennl",
		Environment: "live",
		ScannedAt:   at,
		TotalFiles:  3,
		TotalBytes:  1024,
		Scope:       domain.MediaScanScope{UploadsPath: "/www/site/public/wp-content/uploads"},
	}
}

func mediaRows(n int) []domain.MediaFileRow {
	rows := make([]domain.MediaFileRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, domain.MediaFileRow{
			Path:     fmt.Sprintf("2024/05/foto-%d.jpg", i),
			Bytes:    int64(100 + i),
			Class:    domain.MediaClassOriginal,
			Category: domain.MediaUnreferenced,
		})
	}
	return rows
}

func TestMediaScanStoreRoundTrip(t *testing.T) {
	store := NewMediaScanStore(t.TempDir())
	at := time.Date(2026, 7, 29, 14, 2, 0, 0, time.UTC)

	if err := store.Save(mediaSummary("20260729-140200", at), mediaRows(5)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get("p1", "20260729-140200")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ProjectName != "web-vanluykennl" || got.TotalBytes != 1024 {
		t.Errorf("samenvatting niet rond: %+v", got)
	}
	if !got.ScannedAt.Equal(at) {
		t.Errorf("ScannedAt = %v, wil %v", got.ScannedAt, at)
	}
}

func TestMediaScanStoreDetailPaginering(t *testing.T) {
	store := NewMediaScanStore(t.TempDir())
	if err := store.Save(mediaSummary("s1", time.Now()), mediaRows(5)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	eerste, err := store.Detail("p1", "s1", 0, 2)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(eerste) != 2 || eerste[0].Path != "2024/05/foto-0.jpg" {
		t.Errorf("eerste pagina = %+v", eerste)
	}

	// Voorbij het einde vragen levert de rest op, geen fout.
	laatste, err := store.Detail("p1", "s1", 3, 10)
	if err != nil {
		t.Fatalf("Detail voorbij einde: %v", err)
	}
	if len(laatste) != 2 || laatste[1].Path != "2024/05/foto-4.jpg" {
		t.Errorf("laatste pagina = %+v", laatste)
	}
}

func TestMediaScanStoreListNieuwsteEerst(t *testing.T) {
	store := NewMediaScanStore(t.TempDir())
	oud := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	nieuw := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)

	if err := store.Save(mediaSummary("20260701-090000", oud), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(mediaSummary("20260729-090000", nieuw), nil); err != nil {
		t.Fatal(err)
	}

	list, err := store.List("p1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || !list[0].ScannedAt.Equal(nieuw) {
		t.Fatalf("wil nieuwste eerst, kreeg %+v", list)
	}

	laatste, err := store.Latest("p1")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if laatste == nil || !laatste.ScannedAt.Equal(nieuw) {
		t.Errorf("Latest = %+v, wil de scan van %v", laatste, nieuw)
	}
}

func TestMediaScanStoreZonderScans(t *testing.T) {
	store := NewMediaScanStore(t.TempDir())

	list, err := store.List("onbekend")
	if err != nil || len(list) != 0 {
		t.Errorf("List zonder scans = %v, %v; wil leeg en geen fout", list, err)
	}

	laatste, err := store.Latest("onbekend")
	if err != nil {
		t.Errorf("Latest zonder scans mag geen fout geven: %v", err)
	}
	if laatste != nil {
		t.Errorf("Latest = %+v, wil nil", laatste)
	}
}
