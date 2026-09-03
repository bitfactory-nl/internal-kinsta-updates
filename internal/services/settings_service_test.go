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

func TestSettingsDBAppDefault(t *testing.T) {
	cfg := &config.Global{Editor: "cursor"}
	s := NewSettingsService(cfg)
	if got := s.Get(); got.DBApp != "Sequel Ace" {
		t.Errorf("default DBApp = %q, want %q", got.DBApp, "Sequel Ace")
	}
	if cfg.DBApp != "" {
		t.Errorf("Get() should not persist the default, got cfg.DBApp = %q", cfg.DBApp)
	}
}

func TestSettingsDBAppRoundTrip(t *testing.T) {
	cfg := &config.Global{Editor: "cursor"}
	s := NewSettingsService(cfg)
	in := s.Get()
	in.DBApp = "TablePlus"
	t.Setenv("HOME", t.TempDir())
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if cfg.DBApp != "TablePlus" {
		t.Errorf("cfg not updated: %q", cfg.DBApp)
	}
	if got := s.Get(); got.DBApp != "TablePlus" {
		t.Errorf("Get roundtrip: %q", got.DBApp)
	}
}

func TestSettingsRondritMetUpdateVelden(t *testing.T) {
	autoAan := true
	cfg := &config.Global{
		Editor:  "cursor",
		Updates: config.UpdatesGlobal{AutoCheck: &autoAan, GithubToken: "keychain:rdm.github.token"},
	}
	s := NewSettingsService(cfg)

	got := s.Get()
	if !got.UpdatesAutoCheck {
		t.Error("UpdatesAutoCheck = false, wil true")
	}
	if got.UpdatesGithubToken != "keychain:rdm.github.token" {
		t.Errorf("UpdatesGithubToken = %q", got.UpdatesGithubToken)
	}
}

func TestSettingsSaveZetAutoCheckUit(t *testing.T) {
	// SaveGlobal schrijft naar ~/.config/rdm/config.yml; die kant is hier niet
	// interessant, alleen dat de waarde in cfg landt. HOME wijzen naar een
	// tempmap houdt de echte config van de gebruiker buiten de test.
	t.Setenv("HOME", t.TempDir())

	autoAan := true
	cfg := &config.Global{Editor: "cursor", Updates: config.UpdatesGlobal{AutoCheck: &autoAan}}
	s := NewSettingsService(cfg)

	instellingen := s.Get()
	instellingen.UpdatesAutoCheck = false
	instellingen.UpdatesGithubToken = " ghp_met_spaties "
	if err := s.Save(instellingen); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if cfg.Updates.AutoCheckEnabled() {
		t.Error("AutoCheckEnabled() = true na uitzetten")
	}
	if cfg.Updates.GithubToken != "ghp_met_spaties" {
		t.Errorf("GithubToken = %q, wil getrimd", cfg.Updates.GithubToken)
	}
}
