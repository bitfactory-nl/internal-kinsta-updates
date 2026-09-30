package services

import (
	"errors"
	"fmt"

	"github.com/rdm/sites-tool/internal/domain"
)

// errGeenLokaleMap is de fout voor een project (bijv. een WordPress-site uit
// Kinsta) dat nog niet aan een lokale checkout is gekoppeld.
var errGeenLokaleMap = errors.New("koppel eerst een lokale map")

// requireLocal is de ene plek die zegt of een functie een lokale checkout heeft:
// het geeft het pad terug, of een nette fout als er geen (bestaande) map is.
func requireLocal(p domain.Project) (string, error) {
	if !p.Local || p.Path == "" {
		return "", errGeenLokaleMap
	}
	if p.Missing {
		return "", fmt.Errorf("map %s niet gevonden", p.Path)
	}
	return p.Path, nil
}

// localProjects houdt alleen projecten over met een bestaande lokale map; voor
// lussen die een checkout nodig hebben.
func localProjects(ps []domain.Project) []domain.Project {
	uit := make([]domain.Project, 0, len(ps))
	for _, p := range ps {
		if p.Local && !p.Missing {
			uit = append(uit, p)
		}
	}
	return uit
}
