package config

// Global is loaded from ~/.config/rdm/config.yml
type Global struct {
	ProjectsRoots []string        `yaml:"projects_roots"`
	Editor        string          `yaml:"editor"` // cursor | vscode | phpstorm
	DBApp         string          `yaml:"db_app"` // Sequel Ace | TablePlus | ... (optioneel)
	Kinsta        KinstaGlobal    `yaml:"kinsta"`
	PluginRepo    PluginRepo      `yaml:"plugin_repo"`
	Notifications Notifications   `yaml:"notifications"`
	Git           GitGlobal       `yaml:"git"`
	AI            AIGlobal        `yaml:"ai"`
	Wordfence     WordfenceGlobal `yaml:"wordfence"`
}

type KinstaGlobal struct {
	APIKey    string `yaml:"api_key"` // keychain:rdm.kinsta.apiKey or literal (dev only)
	CompanyID string `yaml:"company_id"`
}

type PluginRepo struct {
	GithubToken string `yaml:"github_token"` // keychain:rdm.github.token
	Repo        string `yaml:"repo"`         // org/repo-name
	Ref         string `yaml:"ref"`          // branch/tag, default: main
	// LocalDir is een (tijdelijke) map op deze machine met de nieuwste zips van
	// betaalde plugins; een alternatief naast de repo, niet in plaats daarvan.
	LocalDir string `yaml:"local_dir,omitempty"`
	// ReferenceProjectPath is een lokale WordPress-checkout die als levende
	// referentie-installatie dient voor betaalde plugins: buiten deze tool om
	// actueel gehouden, en hier gebruikt als (a) bron voor de "laatste versie"
	// in het Plugins-overzicht — ook wanneer wp.org een andere versie kent, want
	// voor betaalde plugins is deze installatie de waarheid, niet wp.org — en
	// (b) bron om projecten vanuit bij te werken. Het project zelf hoort geen
	// klantsite te lijken, dus het wordt uit het Plugins-overzicht uitgesloten.
	ReferenceProjectPath string `yaml:"reference_project_path,omitempty"`
}

type Notifications struct {
	EnableVulnerabilityAlerts bool `yaml:"enable_vulnerability_alerts"`
	ScanIntervalMinutes       int  `yaml:"scan_interval_minutes"`
}

type GitGlobal struct {
	DefaultRemote string `yaml:"default_remote"` // default: origin
	PruneOnFetch  bool   `yaml:"prune_on_fetch"`
}

type AIGlobal struct {
	APIKey string `yaml:"api_key"` // keychain:rdm.anthropic.apiKey or literal (dev only)
}

type WordfenceGlobal struct {
	APIKey string `yaml:"api_key"` // keychain:rdm.wordfence.apiKey or literal (dev)
}
