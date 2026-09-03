package services

import (
	"fmt"
	"strings"

	"github.com/rdm/sites-tool/internal/config"
)

// AppSettings is the flat DTO exposed to the frontend.
type AppSettings struct {
	Editor              string `json:"editor"`
	DBApp               string `json:"dbApp"`
	KinstaAPIKey        string `json:"kinstaApiKey"`
	KinstaCompanyID     string `json:"kinstaCompanyId"`
	GithubToken         string `json:"githubToken"`
	PluginRepo          string `json:"pluginRepo"`
	PluginRepoRef       string `json:"pluginRepoRef"`
	PluginLocalDir      string `json:"pluginLocalDir"`
	PluginReferencePath string `json:"pluginReferencePath"`
	GitDefaultRemote    string `json:"gitDefaultRemote"`
	GitPruneOnFetch     bool   `json:"gitPruneOnFetch"`
	AnthropicAPIKey     string `json:"anthropicApiKey"`
	WordfenceAPIKey     string `json:"wordfenceApiKey"`
	UpdatesAutoCheck    bool   `json:"updatesAutoCheck"`
	UpdatesGithubToken  string `json:"updatesGithubToken"`
}

// defaultDBApp is shown when the user hasn't picked a database app yet. It is
// only a display default and is never written back to disk by Get().
const defaultDBApp = "Sequel Ace"

type SettingsService struct {
	cfg *config.Global
}

func NewSettingsService(cfg *config.Global) *SettingsService {
	return &SettingsService{cfg: cfg}
}

// Get returns the current settings.
func (s *SettingsService) Get() AppSettings {
	dbApp := s.cfg.DBApp
	if dbApp == "" {
		dbApp = defaultDBApp
	}
	return AppSettings{
		Editor:              s.cfg.Editor,
		DBApp:               dbApp,
		KinstaAPIKey:        s.cfg.Kinsta.APIKey,
		KinstaCompanyID:     s.cfg.Kinsta.CompanyID,
		GithubToken:         s.cfg.PluginRepo.GithubToken,
		PluginRepo:          s.cfg.PluginRepo.Repo,
		PluginRepoRef:       s.cfg.PluginRepo.Ref,
		PluginLocalDir:      s.cfg.PluginRepo.LocalDir,
		PluginReferencePath: s.cfg.PluginRepo.ReferenceProjectPath,
		GitDefaultRemote:    s.cfg.Git.DefaultRemote,
		GitPruneOnFetch:     s.cfg.Git.PruneOnFetch,
		AnthropicAPIKey:     s.cfg.AI.APIKey,
		WordfenceAPIKey:     s.cfg.Wordfence.APIKey,
		UpdatesAutoCheck:    s.cfg.Updates.AutoCheckEnabled(),
		UpdatesGithubToken:  s.cfg.Updates.GithubToken,
	}
}

// Save persists updated settings to ~/.config/rdm/config.yml.
func (s *SettingsService) Save(settings AppSettings) error {
	if settings.Editor == "" {
		return fmt.Errorf("editor mag niet leeg zijn")
	}
	s.cfg.Editor = settings.Editor
	s.cfg.DBApp = strings.TrimSpace(settings.DBApp)
	s.cfg.Kinsta.APIKey = settings.KinstaAPIKey
	s.cfg.Kinsta.CompanyID = settings.KinstaCompanyID
	s.cfg.PluginRepo.GithubToken = settings.GithubToken
	s.cfg.PluginRepo.Repo = settings.PluginRepo
	if settings.PluginRepoRef != "" {
		s.cfg.PluginRepo.Ref = settings.PluginRepoRef
	}
	s.cfg.PluginRepo.LocalDir = strings.TrimSpace(settings.PluginLocalDir)
	s.cfg.PluginRepo.ReferenceProjectPath = strings.TrimSpace(settings.PluginReferencePath)
	if settings.GitDefaultRemote != "" {
		s.cfg.Git.DefaultRemote = settings.GitDefaultRemote
	}
	s.cfg.Git.PruneOnFetch = settings.GitPruneOnFetch
	s.cfg.AI.APIKey = settings.AnthropicAPIKey
	s.cfg.Wordfence.APIKey = settings.WordfenceAPIKey
	// AutoCheck is een pointer: expliciet zetten, zodat een uitgezette toggle
	// niet als "niet ingevuld" wordt weggeschreven en dus weer aan zou staan.
	autoCheck := settings.UpdatesAutoCheck
	s.cfg.Updates.AutoCheck = &autoCheck
	s.cfg.Updates.GithubToken = strings.TrimSpace(settings.UpdatesGithubToken)
	return config.SaveGlobal(*s.cfg)
}
