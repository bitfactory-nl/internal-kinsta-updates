package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

func TestRequireLocal(t *testing.T) {
	if _, err := requireLocal(domain.Project{ID: "site:x"}); !errors.Is(err, errGeenLokaleMap) {
		t.Fatalf("site zonder map: %v", err)
	}
	if _, err := requireLocal(domain.Project{Local: true, Missing: true, Path: "/weg"}); err == nil || err.Error() != "map /weg niet gevonden" {
		t.Fatalf("missing: %v", err)
	}
	if p, err := requireLocal(domain.Project{Local: true, Path: "/ok"}); err != nil || p != "/ok" {
		t.Fatalf("ok: %q %v", p, err)
	}
}

func TestLocalProjectsFilter(t *testing.T) {
	in := []domain.Project{{ID: "a", Local: true, Path: "/a"}, {ID: "site:b"}, {ID: "c", Local: true, Missing: true, Path: "/c"}}
	got := localProjects(in)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %+v", got)
	}
}

func TestGitServiceZonderMap(t *testing.T) {
	ps := newProjectServiceWith([]domain.Project{{ID: "site:x", SiteID: "x"}})
	g := NewGitService(ps)
	if _, err := g.GetStatus("site:x"); !errors.Is(err, errGeenLokaleMap) || !strings.HasSuffix(err.Error(), "koppel eerst een lokale map") {
		t.Fatalf("fout %v", err)
	}
}

// siteZonderMap is een WordPress-site uit het register zonder lokale checkout.
func siteZonderMap() domain.Project {
	return domain.Project{
		ID: "site:x", SiteID: "x", DisplayName: "Site X",
		Git:    domain.GitStatus{IsRepo: true},
		Deploy: domain.DeployConf{Type: "wordpress_kinsta"},
	}
}

func TestSearchOverslaatSiteZonderMap(t *testing.T) {
	svc := NewSearchService(newProjectServiceWith([]domain.Project{siteZonderMap()}))
	if hits := svc.GrepAll("iets"); len(hits) != 0 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestBatchOverslaatSiteZonderMap(t *testing.T) {
	svc := NewBatchService(newProjectServiceWith([]domain.Project{siteZonderMap()}))
	if res := svc.FetchAll(); len(res) != 0 {
		t.Fatalf("resultaten = %+v", res)
	}
}

func TestInventoryToontSiteZonderMapNiet(t *testing.T) {
	svc := newTestInventory(
		fakeInventoryLister{list: []domain.Project{siteZonderMap()}},
		fakeInventoryResolver{},
	)
	items, err := svc.Plugins()
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
}

func TestBulkUpdateSiteZonderMapGeenKandidaat(t *testing.T) {
	ps := newProjectServiceWith([]domain.Project{siteZonderMap()})
	svc := NewBulkUpdateService(&config.Global{}, ps, NewGitService(ps), &PluginService{cfg: &config.Global{}})
	if got := svc.wordpressProjecten(); len(got) != 0 {
		t.Fatalf("kandidaten = %+v", got)
	}
	r := svc.updateOneProject("site:x", "Site X", "b", "6.0", nil, func() error { return nil }, nil, 1, 1)
	if r.Status != "error" || r.Error != "koppel eerst een lokale map" {
		t.Fatalf("resultaat = %+v", r)
	}
}

func TestMigrationZonderMap(t *testing.T) {
	svc := NewMigrationService(newProjectServiceWith([]domain.Project{siteZonderMap()}), nil)
	if _, err := svc.GetSettings("site:x"); !errors.Is(err, errGeenLokaleMap) {
		t.Fatalf("fout %v", err)
	}
}

func TestDBCloneZonderMap(t *testing.T) {
	svc := NewDBCloneService(newProjectServiceWith([]domain.Project{siteZonderMap()}), nil)
	if _, err := svc.LocalDefaults("site:x"); !errors.Is(err, errGeenLokaleMap) {
		t.Fatalf("fout %v", err)
	}
}
