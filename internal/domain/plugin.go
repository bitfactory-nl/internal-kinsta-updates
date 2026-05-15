package domain

import "time"

type KinstaPlugin struct {
	Name                string `json:"name"`
	Slug                string `json:"slug"`
	Status              string `json:"status"`
	Version             string `json:"version"`
	Update              string `json:"update"`
	UpdateVersion       string `json:"updateVersion,omitempty"`
	IsVersionVulnerable bool   `json:"isVersionVulnerable"`
}

type KinstaTheme struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Status        string `json:"status"`
	Version       string `json:"version"`
	Update        string `json:"update"`
	UpdateVersion string `json:"updateVersion,omitempty"`
}

type PaidPlugin struct {
	Slug          string    `json:"slug"`
	DisplayName   string    `json:"displayName"`
	LatestVersion string    `json:"latestVersion"`
	ZipPath       string    `json:"zipPath"`
	UpdatedAt     time.Time `json:"updatedAt"`
	DownloadURL   string    `json:"downloadUrl"`
}

type DiffStatus string

const (
	DiffUpToDate   DiffStatus = "up_to_date"
	DiffUpdate     DiffStatus = "update_available"
	DiffNotFound   DiffStatus = "not_in_repo"
	DiffVulnerable DiffStatus = "vulnerable"
)

type PluginSource string

const (
	SourcePrivateRepo PluginSource = "private_repo"
	SourceWPOrg       PluginSource = "wp_org"
	SourceUnknown     PluginSource = "unknown"
)

type PluginDiff struct {
	Slug             string       `json:"slug"`
	InstalledVersion string       `json:"installedVersion"`
	AvailableVersion string       `json:"availableVersion"`
	Status           DiffStatus   `json:"status"`
	IsVulnerable     bool         `json:"isVulnerable"`
	Source           PluginSource `json:"source"`
}
