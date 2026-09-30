package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// ImportCandidate is een map uit de oude projectscan die je in het register
// kunt opnemen.
type ImportCandidate struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	DeployType   string `json:"deployType"`
	URL          string `json:"url"`
	KinstaSiteID string `json:"kinstaSiteId"`
	WordPress    bool   `json:"wordPress"`
}

// ImportChoice is de keuze voor één kandidaat: als WordPress-site of als lokaal project.
type ImportChoice struct {
	Path   string `json:"path"`
	AsSite bool   `json:"asSite"`
}

// ImportResult is de uitkomst van Apply: het aantal toegevoegde items en
// notities over items die anders zijn opgenomen dan gekozen.
type ImportResult struct {
	Added int      `json:"added"`
	Notes []string `json:"notes"`
}

// ImportService doet de eenmalige overstap van de oude mappenscan naar het register.
type ImportService struct {
	cfg        *config.Global
	sites      *WPSiteService
	register   *RegisterService
	sitesPath  string
	localsPath string
}

func NewImportService(cfg *config.Global, sites *WPSiteService, register *RegisterService, sitesPath, localsPath string) *ImportService {
	return &ImportService{cfg: cfg, sites: sites, register: register, sitesPath: sitesPath, localsPath: localsPath}
}

// Needed is waar zolang de import niet is afgerond en minstens één projectmap
// bestaat. Een onderbroken import (al wat items in het register) gaat zo
// gewoon verder; Candidates slaat die items over.
func (s *ImportService) Needed() (bool, error) {
	if s.cfg.ImportDone {
		return false, nil
	}
	for _, root := range s.cfg.ProjectsRoots {
		if dirExists(config.ExpandHome(root)) {
			return true, nil
		}
	}
	return false, nil
}

// Candidates loopt over de submappen van elke projectmap, zoals de oude scan,
// en slaat mappen over die al in het register staan (als project_path van een
// site of als lokaal project).
func (s *ImportService) Candidates() ([]ImportCandidate, error) {
	sites, err := config.LoadWPSites(s.sitesPath)
	if err != nil {
		return nil, err
	}
	locals, err := config.LoadLocalProjects(s.localsPath)
	if err != nil {
		return nil, err
	}
	bezet := map[string]bool{}
	for _, w := range sites {
		if w.ProjectPath != "" {
			bezet[filepath.Clean(w.ProjectPath)] = true
		}
	}
	for _, lp := range locals {
		bezet[filepath.Clean(lp.Path)] = true
	}
	out := []ImportCandidate{}
	for _, root := range s.cfg.ProjectsRoots {
		root = config.ExpandHome(root)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(root, e.Name())
			if bezet[filepath.Clean(path)] {
				continue
			}
			out = append(out, loadCandidate(path))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func loadCandidate(path string) ImportCandidate {
	cfg, _ := config.LoadProject(path)
	deploy, _ := config.LoadDeployConf(path)
	c := ImportCandidate{
		Path:       path,
		Name:       cfg.DisplayName,
		DeployType: deploy.Type,
		URL:        deploy.Link.Prod,
		WordPress:  strings.HasPrefix(deploy.Type, "wordpress"),
	}
	if c.Name == "" {
		c.Name = filepath.Base(path)
	}
	if cfg.Kinsta != nil {
		c.KinstaSiteID = cfg.Kinsta.SiteID
	}
	return c
}

// Apply neemt de gekozen mappen op en markeert de import als afgerond, ook bij
// nul keuzes (overslaan). Een fout bij één item stopt de rest niet; alle fouten
// komen gebundeld terug. De projectlijst wordt één keer herladen, aan het eind.
// Een WordPress-map zonder geldige URL wordt een lokaal project, met een notitie.
func (s *ImportService) Apply(choices []ImportChoice) (ImportResult, error) {
	var errs []error
	res := ImportResult{Notes: []string{}}
	for _, ch := range choices {
		var err error
		if ch.AsSite {
			var note string
			note, err = s.addSite(ch.Path)
			if note != "" {
				res.Notes = append(res.Notes, note)
			}
		} else {
			err = s.addLocal(ch.Path)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch.Path, err))
			continue
		}
		res.Added++
	}
	if len(choices) > 0 {
		s.sites.herlaad()
	}
	next := *s.cfg
	next.ImportDone = true
	if err := config.SaveGlobal(next); err != nil {
		errs = append(errs, fmt.Errorf("import_done opslaan: %w", err))
	} else {
		s.cfg.ImportDone = true
	}
	return res, errors.Join(errs...)
}

// addLocal registreert een map als lokaal project, zonder te herladen.
func (s *ImportService) addLocal(path string) error {
	path, err := normaliseerPad(path)
	if err != nil {
		return err
	}
	if !dirExists(path) {
		return fmt.Errorf("map %s bestaat niet", path)
	}
	return s.register.voegLokaalToe(path, "")
}

// addSite maakt van een map een site zonder wachtwoord, met een uniek id,
// zonder te herladen. Is de site ongeldig (in de praktijk: geen of een
// ongeldige URL in deploy_conf.json), dan wordt de map een lokaal project en
// komt er een notitie terug; de map valt dus niet weg.
func (s *ImportService) addSite(path string) (string, error) {
	path, err := normaliseerPad(path)
	if err != nil {
		return "", err
	}
	sites, err := config.LoadWPSites(s.sitesPath)
	if err != nil {
		return "", err
	}
	c := loadCandidate(path)
	site := domain.WPSite{
		ID:           uniekSiteID(domain.SuggestWPSiteID(filepath.Base(path)), sites),
		Name:         c.Name,
		URL:          c.URL,
		Username:     defaultWPUser,
		ProjectPath:  path,
		KinstaSiteID: c.KinstaSiteID,
	}
	if err := site.Validate(); err != nil {
		if lokaalErr := s.addLocal(path); lokaalErr != nil {
			return "", lokaalErr
		}
		return fmt.Sprintf("%s: geen geldige URL in deploy_conf.json (%v), toegevoegd als lokaal project", c.Name, err), nil
	}
	_, err = s.sites.create(site, "", false)
	return "", err
}

// uniekSiteID voegt -2, -3, … toe tot het id nog niet in sites voorkomt.
func uniekSiteID(basis string, sites []domain.WPSite) string {
	bezet := make(map[string]bool, len(sites))
	for _, w := range sites {
		bezet[w.ID] = true
	}
	id := basis
	for n := 2; bezet[id]; n++ {
		id = fmt.Sprintf("%s-%d", basis, n)
	}
	return id
}
