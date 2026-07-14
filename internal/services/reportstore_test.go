package services

import (
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestReportStoreSaveGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewReportStore(dir)

	r := domain.Report{
		ProjectID:  "p1",
		Period:     "Q3 2026",
		ClientName: "Cefetra",
		Acties:     []domain.ActieRow{{Actie: "Upgrade PHP", Wie: "Bitfactory"}},
	}
	if err := store.Save(r); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Get("p1", "Q3 2026")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ClientName != "Cefetra" || len(got.Acties) != 1 || got.Acties[0].Actie != "Upgrade PHP" {
		t.Fatalf("unexpected report: %+v", got)
	}
}

func TestReportStoreGetMissingReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	store := NewReportStore(dir)

	got, err := store.Get("nope", "Q1 2026")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got.ProjectID != "" || got.ClientName != "" || len(got.Acties) != 0 {
		t.Fatalf("expected zero-value report, got %+v", got)
	}
}

func TestReportStoreSlugifiesPeriod(t *testing.T) {
	dir := t.TempDir()
	store := NewReportStore(dir)

	if err := store.Save(domain.Report{ProjectID: "p1", Period: "Q3 2026", ClientName: "X"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := reportFilePath(dir, "p1", "Q3 2026")
	if got, want := path[len(path)-len("q3-2026.json"):], "q3-2026.json"; got != want {
		t.Fatalf("expected slugified filename ending %q, got %q (full: %s)", want, got, path)
	}
}

func TestReportStoreListNewestFirst(t *testing.T) {
	dir := t.TempDir()
	store := NewReportStore(dir)

	older := domain.Report{ProjectID: "p1", Period: "Q1 2026", ClientName: "X", UpdatedAt: time.Now().Add(-time.Hour)}
	newer := domain.Report{ProjectID: "p1", Period: "Q2 2026", ClientName: "X", UpdatedAt: time.Now()}
	if err := store.Save(older); err != nil {
		t.Fatalf("save older: %v", err)
	}
	if err := store.Save(newer); err != nil {
		t.Fatalf("save newer: %v", err)
	}

	list, err := store.List("p1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(list))
	}
	if list[0].Period != "Q2 2026" || list[1].Period != "Q1 2026" {
		t.Fatalf("expected newest-first order, got %+v then %+v", list[0], list[1])
	}
}

func TestReportStoreListMissingProjectReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	store := NewReportStore(dir)

	list, err := store.List("nope")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if list != nil {
		t.Fatalf("expected nil list, got %+v", list)
	}
}
