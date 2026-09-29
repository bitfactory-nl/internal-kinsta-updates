package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rdm/sites-tool/internal/domain"
)

type wpSitesFile struct {
	Sites []domain.WPSite `yaml:"sites"`
}

// WPSitesPath is het per-gebruiker bestand met WordPress-sites voor wp-fleet.
// Het staat bewust buiten elke projectmap: het hoort bij de gebruiker, niet bij
// de klantrepo.
// WPSecretAccountPrefix is het Keychain-account-voorvoegsel voor het
// Application Password van een WordPress-site.
const WPSecretAccountPrefix = "wp:"

func WPSecretAccount(id string) string { return WPSecretAccountPrefix + id }

// WPSitesPath is het per-gebruiker bestand met WordPress-sites voor wp-fleet.
// Het staat bewust buiten elke projectmap: het hoort bij de gebruiker, niet bij
// de klantrepo.
func WPSitesPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "wp-sites.yml")
}

func LoadWPSites(path string) ([]domain.WPSite, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.WPSite{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lees %s: %w", path, err)
	}
	var f wpSitesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range f.Sites {
		f.Sites[i].ProjectPath = ExpandHome(f.Sites[i].ProjectPath)
	}
	if err := validateWPSites(f.Sites); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Sites == nil {
		f.Sites = []domain.WPSite{}
	}
	return f.Sites, nil
}

func SaveWPSites(path string, sites []domain.WPSite) error {
	if err := validateWPSites(sites); err != nil {
		return fmt.Errorf("wp-sites: %w", err)
	}
	out := make([]domain.WPSite, len(sites))
	for i, s := range sites {
		s.ProjectPath = CompressHome(s.ProjectPath)
		out[i] = s
	}
	data, err := yaml.Marshal(wpSitesFile{Sites: out})
	if err != nil {
		return fmt.Errorf("wp-sites: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("wp-sites map: %w", err)
	}
	// Atomisch: eerst een tijdelijk bestand naast het doel, dan hernoemen. Een
	// crash halverwege laat zo nooit een half bestand achter.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wp-sites-*.yml")
	if err != nil {
		return fmt.Errorf("wp-sites tijdelijk bestand: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("wp-sites rechten: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("wp-sites schrijven: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("wp-sites schrijven: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}

func validateWPSites(sites []domain.WPSite) error {
	ids := map[string]bool{}
	paden := map[string]string{}
	for i, s := range sites {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("wp-sites site %d (%s): %w", i+1, s.ID, err)
		}
		if ids[s.ID] {
			return fmt.Errorf("wp-sites: id %q komt dubbel voor", s.ID)
		}
		ids[s.ID] = true
		if s.ProjectPath != "" {
			if ander, ok := paden[s.ProjectPath]; ok {
				return fmt.Errorf("wp-sites: project %s is al gekoppeld aan %q", s.ProjectPath, ander)
			}
			paden[s.ProjectPath] = s.ID
		}
	}
	return nil
}

// ExpandHome zet een pad dat met ~/ begint om naar een absoluut pad.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// CompressHome schrijft een pad onder de home-map als ~/…, zodat het bestand
// niet aan één gebruikersnaam vastzit.
func CompressHome(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
