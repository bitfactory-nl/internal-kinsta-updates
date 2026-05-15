package kinsta

// Site represents a Kinsta site.
type Site struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

// ContainerInfo holds runtime version info for an environment.
type ContainerInfo struct {
	PHPEngineVersion string `json:"php_engine_version"` // e.g. "php8.3"
}

// SSHConnection holds SSH access details.
type SSHConnection struct {
	SSHPort string `json:"ssh_port"`
	SSHIP   struct {
		ExternalIP string `json:"external_ip"`
	} `json:"ssh_ip"`
}

// Environment represents a full Kinsta site environment (from /sites/{id}/environments).
type Environment struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	DisplayName      string        `json:"display_name"`
	IsBlocked        bool          `json:"is_blocked"`
	WordPressVersion string        `json:"wordpress_version"`
	ContainerInfo    ContainerInfo `json:"container_info"`
	SSHConnection    SSHConnection `json:"ssh_connection"`
}

// Plugin represents a WordPress plugin (from /wp-plugins).
type Plugin struct {
	Name                      string `json:"name"`
	Title                     string `json:"title"`
	Status                    string `json:"status"`         // "active" | "inactive"
	Version                   string `json:"version"`
	Update                    string `json:"update"`         // "available" or ""
	UpdateVersion             string `json:"update_version"`
	IsVersionVulnerable       bool   `json:"is_version_vulnerable"`
	IsUpdateVersionVulnerable bool   `json:"is_update_version_vulnerable"`
}

// Theme represents a WordPress theme (from /wp-themes).
type Theme struct {
	Name                string `json:"name"`
	Title               string `json:"title"`
	Status              string `json:"status"`
	Version             string `json:"version"`
	Update              string `json:"update"`
	UpdateVersion       string `json:"update_version"`
	IsVersionVulnerable bool   `json:"is_version_vulnerable"`
}

// SiteDetails wraps site + its environments (with full details).
type SiteDetails struct {
	Site         Site          `json:"site"`
	Environments []Environment `json:"environments"`
}

// EnvironmentDetails holds plugins + themes for an environment.
type EnvironmentDetails struct {
	Plugins []Plugin `json:"plugins"`
	Themes  []Theme  `json:"themes"`
}
