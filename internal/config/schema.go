package config

// Global is loaded from ~/.config/rdm/config.yml
type Global struct {
	ProjectsRoots []string      `yaml:"projects_roots"`
	Editor        string        `yaml:"editor"` // cursor | vscode | phpstorm
	Kinsta        KinstaGlobal  `yaml:"kinsta"`
	PluginRepo    PluginRepo    `yaml:"plugin_repo"`
	Notifications Notifications `yaml:"notifications"`
	Git           GitGlobal     `yaml:"git"`
}

type KinstaGlobal struct {
	APIKey    string `yaml:"api_key"`    // keychain:rdm.kinsta.apiKey or literal (dev only)
	CompanyID string `yaml:"company_id"`
}

type PluginRepo struct {
	GithubToken string `yaml:"github_token"` // keychain:rdm.github.token
	Repo        string `yaml:"repo"`         // org/repo-name
	Ref         string `yaml:"ref"`          // branch/tag, default: main
}

type Notifications struct {
	EnableVulnerabilityAlerts bool `yaml:"enable_vulnerability_alerts"`
	ScanIntervalMinutes       int  `yaml:"scan_interval_minutes"`
}

type GitGlobal struct {
	DefaultRemote string `yaml:"default_remote"` // default: origin
	PruneOnFetch  bool   `yaml:"prune_on_fetch"`
}
