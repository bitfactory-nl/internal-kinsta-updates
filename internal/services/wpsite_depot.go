package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// DepotCoreStatus vergelijkt de live WordPress-versie van de depotsite met
// de versie in composer.lock op de default-branch van de repo.
type DepotCoreStatus struct {
	Live    string `json:"live"`
	Locked  string `json:"locked"`
	NeedsPR bool   `json:"needsPr"`
}

const (
	depotStatusTimeout = 2 * time.Minute
	depotPRTimeout     = 10 * time.Minute
)

var errGeenDepot = errors.New("alleen beschikbaar voor de depotsite met een gekoppelde projectmap")

// DepotCoreStatus meldt of composer.lock achterloopt op de live versie.
func (s *WPSiteService) DepotCoreStatus(siteID string) (DepotCoreStatus, error) {
	site, err := s.vindDepotSite(siteID)
	if err != nil {
		return DepotCoreStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), depotStatusTimeout)
	defer cancel()
	return s.depotCoreStatus(ctx, site)
}

// OpenDepotCorePR maakt los van een live update de Composer-PR die
// composer.lock op de live versie zet. Raakt de live site niet, dus
// alleen-lezen houdt hem niet tegen; wel één run per site tegelijk.
func (s *WPSiteService) OpenDepotCorePR(siteID string) (RunResult, error) {
	site, err := s.vindDepotSite(siteID)
	if err != nil {
		return RunResult{}, err
	}
	if !s.claimRun(siteID) {
		return RunResult{}, errors.New("er loopt al een update voor deze site")
	}
	defer s.releaseRun(siteID)
	ctx, cancel := context.WithTimeout(context.Background(), depotPRTimeout)
	defer cancel()
	st, err := s.depotCoreStatus(ctx, site)
	if err != nil {
		return RunResult{}, err
	}
	if !st.NeedsPR {
		return RunResult{}, fmt.Errorf("composer.lock staat al op %s", st.Locked)
	}
	url, err := s.depot.OpenCorePR(ctx, site.ProjectPath, st.Live)
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{
		Items:       []RunItem{{Name: "Composer-PR", From: st.Locked, To: st.Live, Success: true}},
		PullRequest: url,
	}, nil
}

// vindDepotSite geeft de site als hij de depotsite is met een projectmap.
func (s *WPSiteService) vindDepotSite(siteID string) (domain.WPSite, error) {
	site, err := s.find(siteID)
	if err != nil {
		return domain.WPSite{}, err
	}
	if !site.Depot || site.ProjectPath == "" || s.depot == nil {
		return domain.WPSite{}, errGeenDepot
	}
	return site, nil
}

func (s *WPSiteService) depotCoreStatus(ctx context.Context, site domain.WPSite) (DepotCoreStatus, error) {
	site, pw, err := s.siteMetSecret(site.ID)
	if err != nil {
		return DepotCoreStatus{}, err
	}
	var raw struct {
		WPVersion string `json:"wp_version"`
	}
	if err := s.bridgeCall(site, pw, http.MethodGet, "/status", nil, bridgeReadTimeout, &raw); err != nil {
		return DepotCoreStatus{}, err
	}
	locked, err := s.depot.LockedCoreVersion(ctx, site.ProjectPath)
	if err != nil {
		return DepotCoreStatus{}, err
	}
	return DepotCoreStatus{Live: raw.WPVersion, Locked: locked, NeedsPR: versieNieuwer(raw.WPVersion, locked)}, nil
}
