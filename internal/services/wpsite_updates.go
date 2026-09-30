package services

import (
	"net/http"
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
