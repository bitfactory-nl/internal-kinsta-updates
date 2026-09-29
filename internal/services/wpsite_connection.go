package services

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

const (
	ConnOK          = "ok"
	ConnAuthFailed  = "auth_failed"
	ConnNoBridge    = "no_bridge"
	ConnUnreachable = "unreachable"
)

type ConnectionResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// TestConnection controleert auth (users/me vereist een ingelogde gebruiker)
// en de bridge, en zet de bridge-vlag van de site op de uitkomst zodra die
// eenduidig is.
func (s *WPSiteService) TestConnection(id string) (ConnectionResult, error) {
	site, err := s.find(id)
	if err != nil {
		return ConnectionResult{}, err
	}
	pw, err := s.secret(id)
	if err != nil || pw == "" {
		return ConnectionResult{Status: ConnAuthFailed, Message: "geen Application Password opgeslagen"}, nil
	}
	base := strings.TrimRight(site.URL, "/")

	code, err := s.get(base+"/wp-json/wp/v2/users/me", site.Username, pw)
	if err != nil {
		return ConnectionResult{Status: ConnUnreachable, Message: "site niet bereikbaar"}, nil
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return ConnectionResult{Status: ConnAuthFailed, Message: "gebruiker of Application Password klopt niet"}, nil
	}
	if code != http.StatusOK {
		return ConnectionResult{Status: ConnUnreachable, Message: fmt.Sprintf("WordPress antwoordde HTTP %d", code)}, nil
	}

	code, err = s.get(base+"/wp-json/nb-mcp/v1/status", site.Username, pw)
	if err != nil {
		return ConnectionResult{Status: ConnUnreachable, Message: "bridge-check niet bereikbaar"}, nil
	}
	// Alleen een eenduidig antwoord (200 of 404) wijzigt de bridge-vlag; bij
	// een rechten- of serverfout weten we niets en blijft de vlag staan.
	var res ConnectionResult
	var bridge bool
	switch code {
	case http.StatusOK:
		res, bridge = ConnectionResult{Status: ConnOK, Message: "verbinding en bridge in orde"}, true
	case http.StatusNotFound:
		res, bridge = ConnectionResult{Status: ConnNoBridge, Message: "verbinding ok, nb-mcp-bridge ontbreekt"}, false
	case http.StatusUnauthorized, http.StatusForbidden:
		return ConnectionResult{Status: ConnAuthFailed, Message: "nb-mcp-bridge weigert de gebruiker (rechten)"}, nil
	default:
		return ConnectionResult{Status: ConnUnreachable, Message: fmt.Sprintf("bridge-check antwoordde HTTP %d", code)}, nil
	}
	if _, err := s.mutate(id, func(w *domain.WPSite) { w.Bridge = bridge }); err != nil {
		return ConnectionResult{}, err
	}
	return res, nil
}

// get doet een GET met Basic Auth en geeft alleen de statuscode terug; de
// fout zelf kan de URL bevatten maar nooit het wachtwoord.
func (s *WPSiteService) get(url, user, pw string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(user, pw)
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
