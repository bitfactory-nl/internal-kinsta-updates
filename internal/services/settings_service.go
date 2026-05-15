package services

import (
	"fmt"

	"github.com/rdm/sites-tool/internal/config"
)

// AppSettings is the flat DTO exposed to the frontend.
type AppSettings struct {
	Editor           string `json:"editor"`
	KinstaAPIKey     string `json:"kinstaApiKey"`
	KinstaCompanyID  string `json:"kinstaCompanyId"`
	GitDefaultRemote string `json:"gitDefaultRemote"`
	GitPruneOnFetch  bool   `json:"gitPruneOnFetch"`
}

type SettingsService struct {
	cfg *config.Global
}

func NewSettingsService(cfg *config.Global) *SettingsService {
	return &SettingsService{cfg: cfg}
}

// Get returns the current settings.
func (s *SettingsService) Get() AppSettings {
	return AppSettings{
		Editor:           s.cfg.Editor,
		KinstaAPIKey:     s.cfg.Kinsta.APIKey,
		KinstaCompanyID:  s.cfg.Kinsta.CompanyID,
		GitDefaultRemote: s.cfg.Git.DefaultRemote,
		GitPruneOnFetch:  s.cfg.Git.PruneOnFetch,
	}
}

// Save persists updated settings to ~/.config/rdm/config.yml.
func (s *SettingsService) Save(settings AppSettings) error {
	if settings.Editor == "" {
		return fmt.Errorf("editor mag niet leeg zijn")
	}
	s.cfg.Editor = settings.Editor
	s.cfg.Kinsta.APIKey = settings.KinstaAPIKey
	s.cfg.Kinsta.CompanyID = settings.KinstaCompanyID
	if settings.GitDefaultRemote != "" {
		s.cfg.Git.DefaultRemote = settings.GitDefaultRemote
	}
	s.cfg.Git.PruneOnFetch = settings.GitPruneOnFetch
	return config.SaveGlobal(*s.cfg)
}
