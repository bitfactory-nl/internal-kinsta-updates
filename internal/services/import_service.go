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

// Needed is waar zolang de import niet is afgerond, er nog geen lokale
// projecten zijn geregistreerd en minstens één projectmap bestaat. Sites in
// wp-sites.yml (uit een eerdere feature) sluiten de import niet uit.
func (s *ImportService) Needed() (bool, error) {
	if s.cfg.ImportDone {
		return false, nil
	}
	locals, err := config.LoadLocalProjects(s.localsPath)
	if err != nil {
		return false, err
	}
	if len(locals) > 0 {
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
// en slaat mappen over die al het project_path van een site zijn.
func (s *ImportService) Candidates() ([]ImportCandidate, error) {
	sites, err := config.LoadWPSites(s.sitesPath)
	if err != nil {
		return nil, err
	}
	bezet := map[string]bool{}
	for _, w := range sites {
		if w.ProjectPath != "" {
			bezet[filepath.Clean(w.ProjectPath)] = true
		}
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
// komen gebundeld terug. Het resultaat is het aantal toegevoegde items.
func (s *ImportService) Apply(choices []ImportChoice) (int, error) {
	var errs []error
	added := 0
	for _, ch := range choices {
		var err error
		if ch.AsSite {
			err = s.addSite(ch.Path)
		} else {
			_, err = s.register.AddLocalProjectPath(ch.Path, "")
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch.Path, err))
			continue
		}
		added++
	}
	s.cfg.ImportDone = true
	if err := config.SaveGlobal(*s.cfg); err != nil {
		errs = append(errs, fmt.Errorf("import_done opslaan: %w", err))
	}
	return added, errors.Join(errs...)
}

// addSite maakt van een map een site zonder wachtwoord, met een uniek id.
func (s *ImportService) addSite(path string) error {
	path, err := normaliseerPad(path)
	if err != nil {
		return err
	}
	sites, err := config.LoadWPSites(s.sitesPath)
	if err != nil {
		return err
	}
	c := loadCandidate(path)
	_, err = s.sites.CreateWithoutPassword(domain.WPSite{
		ID:           uniekSiteID(domain.SuggestWPSiteID(filepath.Base(path)), sites),
		Name:         c.Name,
		URL:          c.URL,
		Username:     defaultWPUser,
		ProjectPath:  path,
		KinstaSiteID: c.KinstaSiteID,
	})
	return err
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
