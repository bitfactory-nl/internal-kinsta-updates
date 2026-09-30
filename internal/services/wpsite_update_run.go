package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

type RunItem struct {
	Name    string `json:"name"`
	From    string `json:"from"`
	To      string `json:"to"`
	Error   string `json:"error"`
	Success bool   `json:"success"`
}

type RunResult struct {
	Items       []RunItem `json:"items"`
	PullRequest string    `json:"pullRequest"`
	ActionsURL  string    `json:"actionsUrl"`
	Warning     string    `json:"warning"`
}

func (s *WPSiteService) claimRun(siteID string) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.running[siteID] {
		return false
	}
	if s.running == nil {
		s.running = map[string]bool{}
	}
	s.running[siteID] = true
	return true
}

func (s *WPSiteService) releaseRun(siteID string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.running, siteID)
}

// startRun doet de precheck voor een uitvoering: site, wachtwoord, bridge,
// niet read-only, en geen andere run op dezelfde site. De caller moet
// releaseRun aanroepen als err nil is.
func (s *WPSiteService) startRun(siteID string) (domain.WPSite, string, error) {
	site, err := s.find(siteID)
	if err != nil {
		return domain.WPSite{}, "", err
	}
	if site.ReadOnly {
		return domain.WPSite{}, "", errors.New("deze site staat op alleen-lezen")
	}
	site, pw, err := s.siteMetSecret(siteID)
	if err != nil {
		return domain.WPSite{}, "", err
	}
	if !s.claimRun(siteID) {
		return domain.WPSite{}, "", errors.New("er loopt al een update voor deze site")
	}
	return site, pw, nil
}

func (s *WPSiteService) UpdatePlugins(siteID string, plugins []string, all bool) (RunResult, error) {
	site, pw, err := s.startRun(siteID)
	if err != nil {
		return RunResult{}, err
	}
	defer s.releaseRun(siteID)
	if all {
		u, err := s.AvailableUpdates(siteID, false)
		if err != nil {
			return RunResult{}, err
		}
		plugins = nil
		for _, p := range u.Plugins {
			plugins = append(plugins, p.ID)
		}
	}
	if len(plugins) == 0 {
		return RunResult{}, errors.New("geen plugins geselecteerd")
	}
	var raw struct {
		Results []struct {
			Plugin  string `json:"plugin"`
			Success bool   `json:"success"`
			From    string `json:"from"`
			To      string `json:"to"`
			Error   string `json:"error"`
		} `json:"results"`
	}
	if err := s.bridgeCall(site, pw, http.MethodPost, "/updates/plugins", map[string][]string{"plugins": plugins}, bridgeRunTimeout, &raw); err != nil {
		return RunResult{}, err
	}
	res := RunResult{Items: []RunItem{}}
	for _, r := range raw.Results {
		res.Items = append(res.Items, RunItem{Name: r.Plugin, From: r.From, To: r.To, Error: r.Error, Success: r.Success})
	}
	s.afterRun(site, "plugins", &res)
	return res, nil
}

func (s *WPSiteService) UpdateCore(siteID string, allowMajor bool) (RunResult, error) {
	site, pw, err := s.startRun(siteID)
	if err != nil {
		return RunResult{}, err
	}
	defer s.releaseRun(siteID)
	var raw struct {
		Success bool   `json:"success"`
		From    string `json:"from"`
		To      string `json:"to"`
		Error   string `json:"error"`
	}
	if err := s.bridgeCall(site, pw, http.MethodPost, "/updates/core", map[string]bool{"allow_major": allowMajor}, bridgeRunTimeout, &raw); err != nil {
		if errors.Is(err, errBridgeTimeout) && site.Depot && site.ProjectPath != "" {
			return RunResult{}, fmt.Errorf("%w — maak de Composer-PR zo nodig handmatig", err)
		}
		return RunResult{}, err
	}
	res := RunResult{Items: []RunItem{{Name: "WordPress", From: raw.From, To: raw.To, Error: raw.Error, Success: raw.Success}}}
	s.afterRun(site, "core", &res)
	return res, nil
}

func (s *WPSiteService) UpdateTranslations(siteID string) (RunResult, error) {
	site, pw, err := s.startRun(siteID)
	if err != nil {
		return RunResult{}, err
	}
	defer s.releaseRun(siteID)
	var raw struct {
		Success bool     `json:"success"`
		Count   int      `json:"count"`
		Failed  int      `json:"failed"`
		Errors  []string `json:"errors"`
	}
	if err := s.bridgeCall(site, pw, http.MethodPost, "/updates/translations", nil, bridgeRunTimeout, &raw); err != nil {
		return RunResult{}, err
	}
	name := fmt.Sprintf("Vertalingen (%d bijgewerkt", raw.Count)
	if raw.Failed > 0 {
		name += fmt.Sprintf(", %d mislukt", raw.Failed)
	}
	name += ")"
	return RunResult{Items: []RunItem{{Name: name, Success: raw.Success, Error: strings.Join(raw.Errors, " ")}}}, nil
}

// afterRun koppelt de depot-informatie aan het resultaat: bij plugins de
// link naar de workflow die de PR maakt, bij een geslaagde core-update een
// eigen Composer-PR. Een mislukte PR is een waarschuwing, geen fout: de live
// update is dan al gebeurd.
func (s *WPSiteService) afterRun(site domain.WPSite, kind string, res *RunResult) {
	if s.depot == nil || !site.Depot || site.ProjectPath == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	switch kind {
	case "plugins":
		for _, it := range res.Items {
			if it.Success {
				if url, err := s.depot.PluginWorkflowURL(ctx, site.ProjectPath); err == nil {
					res.ActionsURL = url
				}
				return
			}
		}
	case "core":
		if len(res.Items) == 0 || !res.Items[0].Success {
			return
		}
		url, err := s.depot.OpenCorePR(ctx, site.ProjectPath, res.Items[0].To)
		if err != nil {
			res.Warning = "WordPress is live bijgewerkt, maar de Composer-PR is niet gemaakt: " + err.Error()
			return
		}
		res.PullRequest = url
	}
}
