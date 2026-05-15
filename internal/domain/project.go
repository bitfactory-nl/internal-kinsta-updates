package domain

import "time"

type Provider string

const (
	ProviderKinsta Provider = "kinsta"
	ProviderAWS    Provider = "aws"
	ProviderVPS    Provider = "vps"
	ProviderNone   Provider = "none"
)

type DeployLinks struct {
	Test string `json:"test"`
	Acc  string `json:"acc"`
	Prod string `json:"prod"`
}

type DeployConf struct {
	Type  string            `json:"type"`
	Link  DeployLinks       `json:"link"`
	Vars  map[string]string `json:"vars,omitempty"`
}

type Project struct {
	ID          string        `json:"id"`
	Path        string        `json:"path"`
	DisplayName string        `json:"displayName"`
	Provider    Provider      `json:"provider"`
	Config      ProjectConfig `json:"config"`
	Deploy      DeployConf    `json:"deploy"`
	Git         GitStatus     `json:"git"`
	LastScanAt  time.Time     `json:"lastScanAt"`
}

type SSHTarget struct {
	Host string `json:"host" yaml:"host"`
	Port int    `json:"port" yaml:"port"`
	User string `json:"user" yaml:"user"`
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

type KinstaProjectCfg struct {
	SiteID       string                      `yaml:"site_id"      json:"siteId"`
	Environments map[string]KinstaEnvBinding `yaml:"environments" json:"environments"`
}

type KinstaEnvBinding struct {
	EnvID  string `yaml:"env_id" json:"envId"`
	Branch string `yaml:"branch" json:"branch"`
}

type AWSProjectCfg struct {
	Region     string `yaml:"region"      json:"region"`
	Service    string `yaml:"service"     json:"service"`
	ResourceID string `yaml:"resource_id" json:"resourceId"`
	URL        string `yaml:"url"         json:"url"`
	Notes      string `yaml:"notes"       json:"notes"`
}

type VPSProjectCfg struct {
	Provider string    `yaml:"provider" json:"provider"`
	SSH      SSHTarget `yaml:"ssh"      json:"ssh"`
	URL      string    `yaml:"url"      json:"url"`
	Notes    string    `yaml:"notes"    json:"notes"`
}

type ProjectConfig struct {
	SchemaVersion int               `yaml:"rdm_schema_version" json:"schemaVersion"`
	Provider      Provider          `yaml:"provider"           json:"provider"`
	DisplayName   string            `yaml:"display_name"       json:"displayName"`
	Kinsta        *KinstaProjectCfg `yaml:"kinsta,omitempty"   json:"kinsta,omitempty"`
	AWS           *AWSProjectCfg    `yaml:"aws,omitempty"      json:"aws,omitempty"`
	VPS           *VPSProjectCfg    `yaml:"vps,omitempty"      json:"vps,omitempty"`
	SSH           *SSHTarget        `yaml:"ssh,omitempty"      json:"ssh,omitempty"`
}
