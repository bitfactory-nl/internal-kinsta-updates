// Package wpfleet bouwt de configuratie voor de wp-fleet-mcp-container en
// start die. De tool is de bron van de sitelijst; secrets gaan alleen via de
// procesomgeving naar docker, nooit via argv of een bestand.
package wpfleet

import (
	"encoding/json"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

type fleetSite struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Username    string   `json:"username"`
	PasswordEnv string   `json:"passwordEnv"`
	Tags        []string `json:"tags"`
	ReadOnly    bool     `json:"readOnly"`
	AllowHTTP   bool     `json:"allowHttp"`
	Bridge      bool     `json:"bridge"`
	// Github is het PR-doel van upload_plugin; nil als de projectmap onbekend is.
	Github *GithubTarget `json:"github,omitempty"`
}

type fleetFile struct {
	Sites []fleetSite `json:"sites"`
}

// EnvName is de omgevingsvariabele waarin wp-fleet het Application Password
// van een site zoekt.
func EnvName(id string) string {
	return "WP_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_APP_PASSWORD"
}

// BuildSitesJSON zet sites om naar het sites.json-formaat van wp-fleet-mcp.
// project_path is iets van de tool en gaat niet mee; wat eruit afgeleid is
// (repo + pluginmap) komt via targets, per site-id.
func BuildSitesJSON(sites []domain.WPSite, targets map[string]GithubTarget) ([]byte, error) {
	out := fleetFile{Sites: make([]fleetSite, 0, len(sites))}
	for _, s := range sites {
		tags := s.Tags
		if tags == nil {
			tags = []string{}
		}
		site := fleetSite{
			ID: s.ID, Name: s.Name, URL: s.URL, Username: s.Username,
			PasswordEnv: EnvName(s.ID), Tags: tags,
			ReadOnly: s.ReadOnly, AllowHTTP: s.AllowHTTP(), Bridge: s.Bridge,
		}
		if t, ok := targets[s.ID]; ok {
			site.Github = &t
		}
		out.Sites = append(out.Sites, site)
	}
	return json.MarshalIndent(out, "", "  ")
}

// RunOptions beschrijft één start van de container.
type RunOptions struct {
	Image         string
	ReadOnly      bool
	SitesJSONPath string
	EnvNames      []string
	// ZipDirs worden read-only op hetzelfde pad gemount en als PLUGIN_ZIP_DIR
	// doorgegeven, zodat upload_plugin een gesleepte zip kan lezen.
	ZipDirs []string
	// GitHubToken: geef GITHUB_TOKEN (alleen de naam) door voor de PR's.
	GitHubToken bool
}

// DockerArgs bouwt de argumenten voor `docker run`. Van de secret-variabelen
// staan alleen de namen erin: `-e NAAM` laat docker de waarde uit de eigen
// omgeving overnemen. --init omdat Node als PID 1 SIGTERM negeert.
func DockerArgs(o RunOptions) []string {
	args := []string{"run", "-i", "--rm", "--init"}
	for _, n := range o.EnvNames {
		args = append(args, "-e", n)
	}
	if o.ReadOnly {
		args = append(args, "-e", "MCP_READ_ONLY=true")
	}
	if o.GitHubToken {
		args = append(args, "-e", "GITHUB_TOKEN")
	}
	if len(o.ZipDirs) > 0 {
		args = append(args, "-e", "PLUGIN_ZIP_DIR="+strings.Join(o.ZipDirs, ","))
		for _, d := range o.ZipDirs {
			args = append(args, "-v", d+":"+d+":ro")
		}
	}
	return append(args,
		"-v", o.SitesJSONPath+":/app/config/sites.json:ro",
		"-v", "wp-fleet-logs:/app/logs",
		o.Image,
	)
}
