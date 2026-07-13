package services

import (
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestRunStoreRoundTrip(t *testing.T) {
	store := NewRunStore(t.TempDir())

	shotDir, err := store.ScreenshotDir("proj1", "run1")
	if err != nil {
		t.Fatalf("ScreenshotDir: %v", err)
	}
	if shotDir == "" {
		t.Fatal("empty screenshot dir")
	}

	run := domain.TestRun{
		ID: "run1", ProjectID: "proj1", FlowName: "F",
		BaselineEnv: domain.EnvProd, UpdateEnv: domain.EnvLocal,
		StartedAt: time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
		Steps:     []domain.StepResult{{Index: 0, Action: domain.StepNavigate}},
	}
	if err := store.Save(run); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get("proj1", "run1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FlowName != "F" || len(got.Steps) != 1 {
		t.Fatalf("Get mismatch: %+v", got)
	}

	list, err := store.List("proj1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "run1" {
		t.Fatalf("List mismatch: %+v", list)
	}
}

func TestRunStoreListMissing(t *testing.T) {
	store := NewRunStore(t.TempDir())
	list, err := store.List("nobody")
	if err != nil {
		t.Fatalf("List missing: %v", err)
	}
	if list != nil {
		t.Errorf("expected nil, got %+v", list)
	}
}

func TestRunStoreListSortedNewestFirst(t *testing.T) {
	store := NewRunStore(t.TempDir())
	older := domain.TestRun{ID: "a", ProjectID: "p", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	newer := domain.TestRun{ID: "b", ProjectID: "p", StartedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	if err := store.Save(older); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(newer); err != nil {
		t.Fatal(err)
	}
	list, err := store.List("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "b" {
		t.Fatalf("expected newest first, got %+v", list)
	}
}
