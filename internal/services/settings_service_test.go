package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

func TestSettingsWordfenceRoundTrip(t *testing.T) {
	cfg := &config.Global{Editor: "cursor"}
	s := NewSettingsService(cfg)
	in := s.Get()
	in.WordfenceAPIKey = "wf-secret"
	// Save writes to disk; only assert the in-memory cfg mutation here by
	// ignoring the persistence error path via a temp HOME.
	t.Setenv("HOME", t.TempDir())
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if cfg.Wordfence.APIKey != "wf-secret" {
		t.Errorf("cfg not updated: %q", cfg.Wordfence.APIKey)
	}
	if got := s.Get(); got.WordfenceAPIKey != "wf-secret" {
		t.Errorf("Get roundtrip: %q", got.WordfenceAPIKey)
	}
}
