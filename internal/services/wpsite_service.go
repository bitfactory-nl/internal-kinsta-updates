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
	List() []domain.Project
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
	runMu    sync.Mutex
	running  map[string]bool
	path     string
	projects WPProjectLookup
	secrets  WPSecretStore
	http     *http.Client
}

const defaultWPUser = "mcp-bot"

func NewWPSiteService(path string, projects WPProjectLookup, secrets WPSecretStore) *WPSiteService {
	return &WPSiteService{
		path: path, projects: projects, secrets: secrets,
		http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: weigerDowngrade},
	}
}

// weigerDowngrade stopt een redirect van https naar http: de Basic Auth met
// het Application Password zou dan onversleuteld meegaan.
func weigerDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("te veel redirects")
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("redirect van https naar http geweigerd")
	}
	return nil
}

// longClient is de gewone client met een andere timeout. Transport en
// redirectbeleid blijven gelijk, zodat tests (Transport) en de https→http-
// weigering ook hier gelden.
func (s *WPSiteService) longClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: s.http.Transport, CheckRedirect: s.http.CheckRedirect, Timeout: timeout}
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

// SuggestForProject vult een nieuw formulier voor een project voor. De
// bridge-vlag volgt de mu-plugin in de checkout; een leesfout telt als geen bridge.
func (s *WPSiteService) SuggestForProject(projectID string) (domain.WPSite, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return domain.WPSite{}, fmt.Errorf("project %s niet gevonden", projectID)
	}
	bridge, err := s.DetectBridge(projectID)
	if err != nil {
		bridge = false
	}
	return domain.WPSite{
		Bridge:      bridge,
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
// De bridge-vlag komt niet uit het formulier: die houdt de opgeslagen waarde,
// want alleen TestConnection mag hem bijwerken. wp-fleet-mcp weigert elke
// bridge-call als de vlag op false staat.
func (s *WPSiteService) Update(site domain.WPSite, password string) (WPSiteView, error) {
	saved, err := s.mutate(site.ID, func(w *domain.WPSite) {
		bridge := w.Bridge
		*w = site
		w.Bridge = bridge
	})
	if err != nil {
		return WPSiteView{}, err
	}
	if password != "" {
		if err := s.secrets.Set(config.WPSecretAccount(site.ID), password); err != nil {
			return WPSiteView{}, err
		}
	}
	return s.view(saved), nil
}

// Unlink maakt van een gekoppelde site een losse site; er gaat niets verloren.
func (s *WPSiteService) Unlink(id string) error {
	_, err := s.mutate(id, func(site *domain.WPSite) { site.ProjectPath = "" })
	return err
}

// mutate laadt, wijzigt en bewaart één site onder één lock, zodat een
// gelijktijdige Update niet overschreven wordt.
func (s *WPSiteService) mutate(id string, fn func(*domain.WPSite)) (domain.WPSite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := config.LoadWPSites(s.path)
	if err != nil {
		return domain.WPSite{}, err
	}
	next := append([]domain.WPSite{}, sites...)
	for i := range next {
		if next[i].ID == id {
			fn(&next[i])
			if err := config.SaveWPSites(s.path, next); err != nil {
				return domain.WPSite{}, err
			}
			return next[i], nil
		}
	}
	return domain.WPSite{}, fmt.Errorf("site %q niet gevonden", id)
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
