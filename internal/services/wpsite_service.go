package services

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// WPProjectLookup is het deel van ProjectService dat WPSiteService nodig heeft.
type WPProjectLookup interface {
	Get(id string) (domain.Project, bool)
}

// WPSecretStore abstraheert de Keychain zodat de service te testen is.
type WPSecretStore interface {
	Set(account, value string) error
	Get(account string) (string, error)
	Delete(account string) error
}

// KeychainSecrets is de echte WPSecretStore.
type KeychainSecrets struct{}

func (KeychainSecrets) Set(a, v string) error { return config.KeychainSet(a, v) }
func (KeychainSecrets) Get(a string) (string, error) {
	return config.ResolveSecret(config.KeychainPrefix + a)
}
func (KeychainSecrets) Delete(a string) error { return config.KeychainDelete(a) }

// WPSiteView is wat de frontend van een site ziet: nooit het wachtwoord zelf.
type WPSiteView struct {
	Site           domain.WPSite `json:"site"`
	HasPassword    bool          `json:"hasPassword"`
	ProjectMissing bool          `json:"projectMissing"`
}

// WPSiteService beheert de WordPress-sites voor wp-fleet (wp-sites.yml + Keychain).
type WPSiteService struct {
	mu       sync.Mutex
	path     string
	projects WPProjectLookup
	secrets  WPSecretStore
	http     *http.Client
}

const defaultWPUser = "mcp-bot"

func NewWPSiteService(path string, projects WPProjectLookup, secrets WPSecretStore) *WPSiteService {
	return &WPSiteService{
		path: path, projects: projects, secrets: secrets,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *WPSiteService) List() ([]WPSiteView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return nil, err
	}
	out := make([]WPSiteView, 0, len(sites))
	for _, site := range sites {
		out = append(out, s.view(site))
	}
	return out, nil
}

func (s *WPSiteService) ForProject(projectID string) (*WPSiteView, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return nil, fmt.Errorf("project %s niet gevonden", projectID)
	}
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v.Site.ProjectPath == p.Path {
			return &v, nil
		}
	}
	return nil, nil
}

// SuggestForProject vult een nieuw formulier voor een project voor.
func (s *WPSiteService) SuggestForProject(projectID string) (domain.WPSite, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return domain.WPSite{}, fmt.Errorf("project %s niet gevonden", projectID)
	}
	return domain.WPSite{
		ID:          domain.SuggestWPSiteID(filepath.Base(p.Path)),
		Name:        p.DisplayName,
		URL:         p.Deploy.Link.Prod,
		Username:    defaultWPUser,
		ProjectPath: p.Path,
	}, nil
}

// DetectBridge kijkt of de nb-mcp-bridge mu-plugin in de checkout staat.
func (s *WPSiteService) DetectBridge(projectID string) (bool, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return false, fmt.Errorf("project %s niet gevonden", projectID)
	}
	mu := filepath.Join(p.Path, "public", "wp-content", "mu-plugins")
	for _, naam := range []string{"nb-mcp-bridge.php", "nb-mcp-bridge"} {
		if _, err := os.Stat(filepath.Join(mu, naam)); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (s *WPSiteService) Create(site domain.WPSite, password string) (WPSiteView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return WPSiteView{}, err
	}
	if err := site.Validate(); err != nil {
		return WPSiteView{}, err
	}
	for _, bestaand := range sites {
		if bestaand.ID == site.ID {
			return WPSiteView{}, fmt.Errorf("id %q bestaat al", site.ID)
		}
	}
	if password == "" {
		return WPSiteView{}, errors.New("Application Password is verplicht bij een nieuwe site")
	}
	if err := s.secrets.Set(config.WPSecretAccount(site.ID), password); err != nil {
		return WPSiteView{}, err
	}
	if err := config.SaveWPSites(s.path, append(sites, site)); err != nil {
		_ = s.secrets.Delete(config.WPSecretAccount(site.ID))
		return WPSiteView{}, err
	}
	return s.view(site), nil
}

// Update wijzigt een bestaande site; een leeg wachtwoord laat het bestaande staan.
func (s *WPSiteService) Update(site domain.WPSite, password string) (WPSiteView, error) {
	if err := s.saveSite(site); err != nil {
		return WPSiteView{}, err
	}
	if password != "" {
		if err := s.secrets.Set(config.WPSecretAccount(site.ID), password); err != nil {
			return WPSiteView{}, err
		}
	}
	return s.view(site), nil
}

// Unlink maakt van een gekoppelde site een losse site; er gaat niets verloren.
func (s *WPSiteService) Unlink(id string) error {
	site, err := s.find(id)
	if err != nil {
		return err
	}
	site.ProjectPath = ""
	return s.saveSite(site)
}

func (s *WPSiteService) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return err
	}
	rest := make([]domain.WPSite, 0, len(sites))
	gevonden := false
	for _, site := range sites {
		if site.ID == id {
			gevonden = true
			continue
		}
		rest = append(rest, site)
	}
	if !gevonden {
		return fmt.Errorf("site %q niet gevonden", id)
	}
	if err := config.SaveWPSites(s.path, rest); err != nil {
		return err
	}
	// Een ontbrekend Keychain-item is geen fout: het doel is dat het weg is.
	_ = s.secrets.Delete(config.WPSecretAccount(id))
	return nil
}

// Stats telt sites en sites zonder wachtwoord, voor het instellingenblok.
func (s *WPSiteService) Stats() (int, int, error) {
	list, err := s.List()
	if err != nil {
		return 0, 0, err
	}
	zonder := 0
	for _, v := range list {
		if !v.HasPassword {
			zonder++
		}
	}
	return len(list), zonder, nil
}

func (s *WPSiteService) find(id string) (domain.WPSite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return domain.WPSite{}, err
	}
	for _, site := range sites {
		if site.ID == id {
			return site, nil
		}
	}
	return domain.WPSite{}, fmt.Errorf("site %q niet gevonden", id)
}

// saveSite vervangt de site met dezelfde id.
func (s *WPSiteService) saveSite(site domain.WPSite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return err
	}
	for i := range sites {
		if sites[i].ID == site.ID {
			next := append([]domain.WPSite{}, sites...)
			next[i] = site
			return config.SaveWPSites(s.path, next)
		}
	}
	return fmt.Errorf("site %q niet gevonden", site.ID)
}

func (s *WPSiteService) secret(id string) (string, error) {
	return s.secrets.Get(config.WPSecretAccount(id))
}

func (s *WPSiteService) view(site domain.WPSite) WPSiteView {
	pw, err := s.secret(site.ID)
	missing := false
	if site.ProjectPath != "" {
		if _, statErr := os.Stat(site.ProjectPath); statErr != nil {
			missing = true
		}
	}
	return WPSiteView{Site: site, HasPassword: err == nil && pw != "", ProjectMissing: missing}
}
