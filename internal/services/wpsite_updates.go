package services

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// SiteUpdates zijn de updates die WordPress zelf op de site klaar heeft staan.
type SiteUpdates struct {
	SiteID       string            `json:"siteId"`
	CheckedAt    string            `json:"checkedAt"`
	Core         []CoreUpdateOffer `json:"core"`
	Plugins      []ItemUpdate      `json:"plugins"`
	Themes       []ItemUpdate      `json:"themes"`
	Translations int               `json:"translations"`
}

type CoreUpdateOffer struct {
	Current string `json:"current"`
	Version string `json:"version"`
	Type    string `json:"type"` // major | minor
}

// ItemUpdate is één plugin (ID = plugin-bestand) of thema (ID = stylesheet).
type ItemUpdate struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CurrentVersion string `json:"currentVersion"`
	NewVersion     string `json:"newVersion"`
}

// bridgeUpdates is het antwoord van GET /nb-mcp/v1/updates.
type bridgeUpdates struct {
	CheckedAt string `json:"checked_at"`
	Core      []struct {
		Current string `json:"current"`
		Version string `json:"version"`
		Type    string `json:"type"`
	} `json:"core"`
	Plugins []struct {
		Plugin         string `json:"plugin"`
		Name           string `json:"name"`
		CurrentVersion string `json:"current_version"`
		NewVersion     string `json:"new_version"`
	} `json:"plugins"`
	Themes []struct {
		Stylesheet     string `json:"stylesheet"`
		Name           string `json:"name"`
		CurrentVersion string `json:"current_version"`
		NewVersion     string `json:"new_version"`
	} `json:"themes"`
	Translations struct {
		Count int `json:"count"`
	} `json:"translations"`
}

// AvailableUpdates vraagt de bridge welke updates er klaarstaan. Met refresh
// laat de bridge WordPress eerst opnieuw bij wp.org kijken (trager).
func (s *WPSiteService) AvailableUpdates(siteID string, refresh bool) (SiteUpdates, error) {
	site, pw, err := s.siteMetSecret(siteID)
	if err != nil {
		return SiteUpdates{}, err
	}
	path, timeout := "/updates", bridgeReadTimeout
	if refresh {
		path, timeout = "/updates?refresh=1", bridgeRefreshTimeout
	}
	var raw bridgeUpdates
	if err := s.bridgeCall(site, pw, http.MethodGet, path, nil, timeout, &raw); err != nil {
		return SiteUpdates{}, err
	}
	out := SiteUpdates{
		SiteID: siteID, CheckedAt: raw.CheckedAt, Translations: raw.Translations.Count,
		Core: []CoreUpdateOffer{}, Plugins: []ItemUpdate{}, Themes: []ItemUpdate{},
	}
	for _, c := range raw.Core {
		// De update_core-transient kan na een update nog een aanbod voor de
		// al geïnstalleerde versie bevatten; dat is geen update.
		if !versieNieuwer(c.Version, c.Current) {
			continue
		}
		out.Core = append(out.Core, CoreUpdateOffer{Current: c.Current, Version: c.Version, Type: c.Type})
	}
	for _, p := range raw.Plugins {
		out.Plugins = append(out.Plugins, ItemUpdate{ID: p.Plugin, Name: p.Name, CurrentVersion: p.CurrentVersion, NewVersion: p.NewVersion})
	}
	for _, t := range raw.Themes {
		out.Themes = append(out.Themes, ItemUpdate{ID: t.Stylesheet, Name: t.Name, CurrentVersion: t.CurrentVersion, NewVersion: t.NewVersion})
	}
	return out, nil
}

// versieNieuwer meldt of versie a hoger is dan b. Vergelijkt numeriek per
// punt-segment; een ontbrekend segment telt als 0 (7.1 == 7.1.0) en een
// niet-numeriek segment ook.
func versieNieuwer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(as), len(bs)); i++ {
		x, y := versieSegment(as, i), versieSegment(bs, i)
		if x != y {
			return x > y
		}
	}
	return false
}

func versieSegment(delen []string, i int) int {
	if i >= len(delen) {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(delen[i]))
	if err != nil {
		return 0
	}
	return n
}

const fleetConcurrency = 4

// FleetSiteUpdates is één regel in het vlootoverzicht.
type FleetSiteUpdates struct {
	Site      WPSiteView   `json:"site"`
	ProjectID string       `json:"projectId"`
	Updates   *SiteUpdates `json:"updates"`
	Error     string       `json:"error"`
}

// FleetUpdates haalt voor alle sites de beschikbare updates op, hooguit vier
// tegelijk. Een fout op één site komt in die regel en stopt de rest niet.
func (s *WPSiteService) FleetUpdates(refresh bool) ([]FleetSiteUpdates, error) {
	views, err := s.List()
	if err != nil {
		return nil, err
	}
	projectByPath := map[string]string{}
	for _, p := range s.projects.List() {
		projectByPath[p.Path] = p.ID
	}
	out := make([]FleetSiteUpdates, len(views))
	sem := make(chan struct{}, fleetConcurrency)
	var wg sync.WaitGroup
	for i, v := range views {
		out[i] = FleetSiteUpdates{Site: v, ProjectID: projectByPath[v.Site.ProjectPath]}
		if v.Site.ProjectPath == "" {
			out[i].ProjectID = ""
		}
		if !v.HasPassword {
			out[i].Error = "geen Application Password opgeslagen"
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			u, err := s.AvailableUpdates(id, refresh)
			if err != nil {
				out[i].Error = err.Error()
				return
			}
			out[i].Updates = &u
		}(i, v.Site.ID)
	}
	wg.Wait()
	return out, nil
}
