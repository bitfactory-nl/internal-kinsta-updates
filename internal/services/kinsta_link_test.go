package services

import (
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// newKinstaService bouwt een service met twee projecten in eigen tijdelijke mappen,
// zodat LinkSite een echt .rdm/config.yml kan schrijven.
func newKinstaService(t *testing.T) (*KinstaService, *ProjectService) {
	t.Helper()
	ps := NewProjectService(nil)
	ps.projects = []domain.Project{
		{ID: "p1", Path: t.TempDir(), DisplayName: "web-steinwegcom"},
		{ID: "p2", Path: t.TempDir(), DisplayName: "web-vanderlelienl"},
	}
	return NewKinstaService(&config.Global{}, ps), ps
}

func TestLinkSiteWeigertKoppelingAanTweedeProject(t *testing.T) {
	svc, _ := newKinstaService(t)
	const siteID = "439d5d52-7c28-4807-bdb8-505c41f27c1f"

	if err := svc.LinkSite("p1", siteID); err != nil {
		t.Fatalf("eerste LinkSite: %v", err)
	}

	err := svc.LinkSite("p2", siteID)
	if err == nil {
		t.Fatal("wil een fout: dezelfde site aan een tweede project koppelen laat één project andermans data zien")
	}
	if !strings.Contains(err.Error(), "web-steinwegcom") {
		t.Errorf("foutmelding %q noemt het andere project niet", err)
	}
}

func TestLinkSiteZelfdeProjectMagOpnieuw(t *testing.T) {
	svc, _ := newKinstaService(t)
	const siteID = "e4678d1f-573b-46e0-bfc2-f5d24d91fdd5"

	if err := svc.LinkSite("p1", siteID); err != nil {
		t.Fatalf("eerste LinkSite: %v", err)
	}
	if err := svc.LinkSite("p1", siteID); err != nil {
		t.Errorf("opnieuw koppelen aan hetzelfde project moet mogen: %v", err)
	}
}

func TestSiteLinkConflicts(t *testing.T) {
	svc, ps := newKinstaService(t)
	const gedeeld = "439d5d52-7c28-4807-bdb8-505c41f27c1f"

	if got := svc.SiteLinkConflicts(); len(got) != 0 {
		t.Fatalf("zonder koppelingen wil je geen conflicten, kreeg %+v", got)
	}

	// Rechtstreeks in de in-memory config zetten: zo zien we ook conflicten die
	// al op schijf stonden voordat de guard bestond.
	ps.projects[0].Config.Kinsta = &domain.KinstaProjectCfg{SiteID: gedeeld}
	ps.projects[1].Config.Kinsta = &domain.KinstaProjectCfg{SiteID: gedeeld}

	got := svc.SiteLinkConflicts()
	if len(got) != 1 {
		t.Fatalf("wil 1 conflict, kreeg %+v", got)
	}
	if got[0].SiteID != gedeeld {
		t.Errorf("SiteID = %q, wil %q", got[0].SiteID, gedeeld)
	}
	if len(got[0].Projects) != 2 || got[0].Projects[0] != "web-steinwegcom" {
		t.Errorf("Projects = %v, wil beide projectnamen gesorteerd", got[0].Projects)
	}
}
