package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// WPSite is een WordPress-site waarmee wp-fleet mag praten: gekoppeld aan een
// projectmap (ProjectPath gevuld) of los. Het Application Password staat
// nooit hier maar in de Keychain onder "wp:<ID>".
type WPSite struct {
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	URL         string   `json:"url" yaml:"url"`
	Username    string   `json:"username" yaml:"username"`
	ProjectPath string   `json:"projectPath,omitempty" yaml:"project_path,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	ReadOnly    bool     `json:"readOnly" yaml:"read_only"`
	Bridge      bool     `json:"bridge" yaml:"bridge"`
	// Depot markeert de SatisPress-depotsite: na een core-update daar maakt de
	// tool een Composer-PR. Er is er hooguit één.
	Depot bool `json:"depot,omitempty" yaml:"depot,omitempty"`
}

// Zelfde regel als wp-fleet-mcp's sites.json, zodat elke id daar ook geldig is.
var wpSiteIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}$`)

func (s WPSite) Validate() error {
	if !wpSiteIDPattern.MatchString(s.ID) {
		return fmt.Errorf("id %q ongeldig: kleine letters, cijfers en -, 2 tot 49 tekens", s.ID)
	}
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("naam is verplicht")
	}
	if strings.TrimSpace(s.Username) == "" {
		return errors.New("WP-gebruiker is verplicht")
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("url %q ongeldig", s.URL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !s.AllowHTTP() {
			return errors.New("url moet https zijn (http alleen voor localhost of *.test)")
		}
	default:
		return fmt.Errorf("url %q moet met https:// beginnen", s.URL)
	}
	return nil
}

// AllowHTTP meldt of de site een lokale http-URL heeft. Application Passwords
// werken alleen over http als WordPress in local/development draait.
func (s WPSite) AllowHTTP() bool {
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || strings.HasSuffix(host, ".test")
}

var nietIDTeken = regexp.MustCompile(`[^a-z0-9]+`)

// SuggestWPSiteID maakt van een naam of mapnaam een voorstel voor een id.
// Het resultaat kan nog ongeldig zijn (bijv. te kort); Validate beslist.
func SuggestWPSiteID(name string) string {
	id := nietIDTeken.ReplaceAllString(strings.ToLower(name), "-")
	id = strings.Trim(id, "-")
	if len(id) > 49 {
		id = strings.TrimRight(id[:49], "-")
	}
	return id
}
