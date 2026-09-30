package services

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// RegisterService beheert het register: lokale projecten (local-projects.yml)
// en de koppeling tussen een WordPress-site en een lokale map.
//
// Beide bestanden worden geschreven onder WPSiteService.mu: die lock bewaakt
// het hele register, zodat een site die een map overneemt en een nieuw lokaal
// project elkaar niet kruisen.
type RegisterService struct {
	projects   *ProjectService
	sites      *WPSiteService
	localsPath string
	app        *application.App
}

func NewRegisterService(projects *ProjectService, sites *WPSiteService, localsPath string) *RegisterService {
	return &RegisterService{projects: projects, sites: sites, localsPath: localsPath}
}

// SetApp geeft de service de Wails-app voor de map-dialoog.
func (s *RegisterService) SetApp(app *application.App) { s.app = app }

// ChooseFolder toont de map-dialoog en geeft het pad terug; leeg bij annuleren.
func (s *RegisterService) ChooseFolder() (string, error) {
	if s.app == nil {
		return "", errors.New("app niet geïnitialiseerd")
	}
	chosen, err := s.app.Dialog.OpenFile().
		SetTitle("Kies de projectmap").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(false).
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("dialoog: %w", err)
	}
	return chosen, nil
}

// AddLocalProject laat een map kiezen en registreert die als lokaal project;
// annuleren geeft nil, nil.
func (s *RegisterService) AddLocalProject() (*domain.Project, error) {
	chosen, err := s.ChooseFolder()
	if err != nil {
		return nil, err
	}
	if chosen == "" {
		return nil, nil
	}
	p, err := s.AddLocalProjectPath(chosen, "")
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// AddLocalProjectPath registreert een bestaande map als lokaal project.
func (s *RegisterService) AddLocalProjectPath(path, name string) (domain.Project, error) {
	path, err := normaliseerPad(path)
	if err != nil {
		return domain.Project{}, err
	}
	if !dirExists(path) {
		return domain.Project{}, fmt.Errorf("map %s bestaat niet", path)
	}
	if err := s.voegLokaalToe(path, name); err != nil {
		return domain.Project{}, err
	}
	if _, err := s.projects.Reload(); err != nil {
		return domain.Project{}, err
	}
	p, ok := s.projects.Get(projectID(path))
	if !ok {
		return domain.Project{}, fmt.Errorf("project voor %s niet gevonden na herladen", path)
	}
	return p, nil
}

// voegLokaalToe schrijft een (al genormaliseerd) pad in local-projects.yml,
// zonder de projectlijst te herladen.
func (s *RegisterService) voegLokaalToe(path, name string) error {
	s.sites.mu.Lock()
	defer s.sites.mu.Unlock()
	sites, err := config.LoadWPSites(s.sites.path)
	if err != nil {
		return err
	}
	locals, err := config.LoadLocalProjects(s.localsPath)
	if err != nil {
		return err
	}
	next := append(append([]domain.LocalProject{}, locals...), domain.LocalProject{Path: path, Name: strings.TrimSpace(name)})
	if err := config.ValidateRegister(sites, next); err != nil {
		return err
	}
	return config.SaveLocalProjects(s.localsPath, next)
}

// RemoveLocalProject haalt een lokaal project uit het register; de map zelf
// blijft staan. Een WordPress-site verwijder of ontkoppel je via de site.
func (s *RegisterService) RemoveLocalProject(projectID string) error {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return fmt.Errorf("project %s niet gevonden", projectID)
	}
	if p.SiteID != "" || strings.HasPrefix(projectID, "site:") {
		return errors.New("dit is een WordPress-site; ontkoppel of verwijder de site")
	}
	if err := s.verwijderLokaal(p.Path); err != nil {
		return err
	}
	_, err := s.projects.Reload()
	return err
}

func (s *RegisterService) verwijderLokaal(path string) error {
	s.sites.mu.Lock()
	defer s.sites.mu.Unlock()
	locals, err := config.LoadLocalProjects(s.localsPath)
	if err != nil {
		return err
	}
	rest, gevonden := zonderPad(locals, path)
	if !gevonden {
		return fmt.Errorf("project %s staat niet in het register", path)
	}
	return config.SaveLocalProjects(s.localsPath, rest)
}

// LinkSiteFolder koppelt een map aan een site. Stond de map als lokaal project
// in het register, dan neemt de site dat project over (zie
// WPSiteService.schrijfSites): een pad staat nooit in beide bestanden.
func (s *RegisterService) LinkSiteFolder(siteID, path string) error {
	path, err := normaliseerPad(path)
	if err != nil {
		return err
	}
	if !dirExists(path) {
		return fmt.Errorf("map %s bestaat niet", path)
	}
	sites, err := config.LoadWPSites(s.sites.path)
	if err != nil {
		return err
	}
	for _, w := range sites {
		if w.ID != siteID && w.ProjectPath != "" && filepath.Clean(w.ProjectPath) == path {
			return fmt.Errorf("map %s staat al in het register", path)
		}
	}
	// mutate leest en schrijft onder de lock van WPSiteService; wachtwoord en
	// bridge-vlag blijven staan en de projecten worden herladen.
	_, err = s.sites.mutate(siteID, func(w *domain.WPSite) { w.ProjectPath = path })
	return err
}

// normaliseerPad maakt van invoer een schoon absoluut pad, of een nette fout.
func normaliseerPad(path string) (string, error) {
	path = config.ExpandHome(strings.TrimSpace(path))
	if path == "" {
		return "", errors.New("kies een map")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("pad moet absoluut zijn")
	}
	return filepath.Clean(path), nil
}

// ConvertLocalToSite geeft een voorstel voor het siteformulier van een lokaal
// project; er wordt nog niets opgeslagen.
func (s *RegisterService) ConvertLocalToSite(projectID string) (domain.WPSite, error) {
	return s.sites.SuggestForProject(projectID)
}

// zonderPad geeft de lijst zonder het project op pad, en of het erin stond.
func zonderPad(locals []domain.LocalProject, path string) ([]domain.LocalProject, bool) {
	rest := make([]domain.LocalProject, 0, len(locals))
	gevonden := false
	for _, lp := range locals {
		if filepath.Clean(lp.Path) == filepath.Clean(path) {
			gevonden = true
			continue
		}
		rest = append(rest, lp)
	}
	return rest, gevonden
}
