package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// fakeCloner legt vast met welke argumenten er "gecloned" is en maakt de
// doelmap net als git zou doen, zodat vervolgcontroles op het bestandssysteem
// realistisch zijn.
type fakeCloner struct {
	mu       sync.Mutex
	calls    []string // "parentDir|url|name"
	err      map[string]error
	blokkeer chan struct{}
}

func (f *fakeCloner) clone(_ context.Context, parentDir, url, name string) error {
	f.mu.Lock()
	f.calls = append(f.calls, strings.Join([]string{parentDir, url, name}, "|"))
	blok := f.blokkeer
	err := f.err[name]
	f.mu.Unlock()

	if blok != nil {
		<-blok
	}
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(parentDir, name, ".git"), 0o755)
}

func (f *fakeCloner) aanroepen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// cloneOpstelling bouwt een OrgSyncService met een fake cloner, een projects-root
// onder t.TempDir(), en een org-sync cache gevuld met de gegeven repo's.
func cloneOpstelling(t *testing.T, repos []domain.OrgSyncRepo) (*OrgSyncService, *fakeCloner, string) {
	t.Helper()
	root := t.TempDir()
	ps := NewProjectService([]string{root})
	cloner := &fakeCloner{err: map[string]error{}}
	svc := &OrgSyncService{
		projects:  ps,
		clone:     cloner.clone,
		now:       time.Now,
		bezig:     map[string]bool{},
		storePath: filepath.Join(t.TempDir(), "org-sync.json"),
	}
	if repos != nil {
		schrijfCloneCache(t, svc, repos)
	}
	return svc, cloner, root
}

func schrijfCloneCache(t *testing.T, svc *OrgSyncService, repos []domain.OrgSyncRepo) {
	t.Helper()
	cache := orgSyncCacheFile{
		Result:  domain.OrgSyncResult{Org: "acme", Repos: repos, FetchedAt: time.Now()},
		Entries: map[string]orgSyncCacheEntry{},
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatalf("cache serialiseren: %v", err)
	}
	if err := os.WriteFile(svc.storePath, data, 0o644); err != nil {
		t.Fatalf("cache schrijven: %v", err)
	}
}

func drieCloneRepos() []domain.OrgSyncRepo {
	return []domain.OrgSyncRepo{
		{Name: "web-alpha", FullName: "acme/web-alpha", DeployType: "wordpress_kinsta", IsWordPress: true},
		{Name: "web-beta", FullName: "acme/web-beta", DeployType: "wordpress_5_2", IsWordPress: true},
		{Name: "web-gamma", FullName: "acme/web-gamma", DeployType: "laravel_9"},
	}
}

func outcomeVoor(res domain.OrgCloneResult, repo string) (domain.OrgCloneOutcome, bool) {
	for _, o := range res.Outcomes {
		if o.Repo == repo {
			return o, true
		}
	}
	return domain.OrgCloneOutcome{}, false
}

func TestCloneHaaltRepoBinnenInProjectsRoot(t *testing.T) {
	svc, cloner, root := cloneOpstelling(t, drieCloneRepos())

	res, err := svc.Clone([]string{"web-alpha", "web-beta"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if res.Cloned != 2 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("tellers = cloned %d, skipped %d, failed %d; wil 2/0/0", res.Cloned, res.Skipped, res.Failed)
	}
	if res.Root != root {
		t.Errorf("Root = %q, wil %q", res.Root, root)
	}

	calls := cloner.aanroepen()
	wil := []string{
		root + "|git@github.com:acme/web-alpha.git|web-alpha",
		root + "|git@github.com:acme/web-beta.git|web-beta",
	}
	if len(calls) != len(wil) {
		t.Fatalf("aanroepen = %v, wil %v", calls, wil)
	}
	for i := range wil {
		if calls[i] != wil[i] {
			t.Errorf("aanroep %d = %q, wil %q", i, calls[i], wil[i])
		}
	}

	for _, naam := range []string{"web-alpha", "web-beta"} {
		if _, err := os.Stat(filepath.Join(root, naam, ".git")); err != nil {
			t.Errorf("checkout %s niet aangemaakt: %v", naam, err)
		}
		o, ok := outcomeVoor(res, naam)
		if !ok || o.Status != domain.OrgCloneCloned {
			t.Errorf("outcome voor %s = %+v, wil status %q", naam, o, domain.OrgCloneCloned)
		}
		if o.Path != filepath.Join(root, naam) {
			t.Errorf("Path voor %s = %q, wil %q", naam, o.Path, filepath.Join(root, naam))
		}
	}
}

// TestCloneScantProjectenNaEenGeslaagdeRun is de reden dat de clone-actie in
// deze service zit: zonder rescan blijft "Lokaal" leeg tot de gebruiker zelf
// ergens een scan uitlokt.
func TestCloneScantProjectenNaEenGeslaagdeRun(t *testing.T) {
	svc, _, root := cloneOpstelling(t, drieCloneRepos())

	if _, err := svc.Clone([]string{"web-alpha"}); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	var paden []string
	for _, p := range svc.projects.List() {
		paden = append(paden, p.Path)
	}
	wil := filepath.Join(root, "web-alpha")
	for _, p := range paden {
		if p == wil {
			return
		}
	}
	t.Fatalf("projecten na clone = %v, wil dat %q erbij staat", paden, wil)
}

func TestCloneSlaatBestaandeMapOverZonderTeClonen(t *testing.T) {
	svc, cloner, root := cloneOpstelling(t, drieCloneRepos())
	bestaand := filepath.Join(root, "web-alpha")
	if err := os.MkdirAll(bestaand, 0o755); err != nil {
		t.Fatalf("map aanmaken: %v", err)
	}
	kanarie := filepath.Join(bestaand, "niet-aanraken.txt")
	if err := os.WriteFile(kanarie, []byte("blijf"), 0o644); err != nil {
		t.Fatalf("bestand schrijven: %v", err)
	}

	res, err := svc.Clone([]string{"web-alpha"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if res.Skipped != 1 || res.Cloned != 0 || res.Failed != 0 {
		t.Fatalf("tellers = cloned %d, skipped %d, failed %d; wil 0/1/0", res.Cloned, res.Skipped, res.Failed)
	}
	if calls := cloner.aanroepen(); len(calls) != 0 {
		t.Errorf("er is gecloned over een bestaande map heen: %v", calls)
	}
	if data, err := os.ReadFile(kanarie); err != nil || string(data) != "blijf" {
		t.Errorf("bestaande inhoud is aangetast: data=%q err=%v", data, err)
	}
	if o, _ := outcomeVoor(res, "web-alpha"); !strings.Contains(o.Message, "bestaat al") {
		t.Errorf("Message = %q, wil een melding over een bestaande map", o.Message)
	}
}

func TestCloneGaatDoorNaEenMislukteRepo(t *testing.T) {
	svc, cloner, root := cloneOpstelling(t, drieCloneRepos())
	cloner.err["web-alpha"] = fmt.Errorf("git clone: Permission denied (publickey)")

	res, err := svc.Clone([]string{"web-alpha", "web-beta"})
	if err != nil {
		t.Fatalf("Clone gaf een fout terug i.p.v. per-repo uitkomsten: %v", err)
	}

	if res.Cloned != 1 || res.Failed != 1 {
		t.Fatalf("tellers = cloned %d, failed %d; wil 1/1", res.Cloned, res.Failed)
	}
	mislukt, _ := outcomeVoor(res, "web-alpha")
	if mislukt.Status != domain.OrgCloneFailed || !strings.Contains(mislukt.Message, "publickey") {
		t.Errorf("mislukte outcome = %+v, wil status failed met de git-fout erin", mislukt)
	}
	if gelukt, _ := outcomeVoor(res, "web-beta"); gelukt.Status != domain.OrgCloneCloned {
		t.Errorf("web-beta = %+v, wil dat de run doorging na de fout", gelukt)
	}
	if _, err := os.Stat(filepath.Join(root, "web-beta", ".git")); err != nil {
		t.Errorf("web-beta niet gecloned: %v", err)
	}
}

func TestCloneWeigertOnbekendeRepoNaam(t *testing.T) {
	svc, cloner, _ := cloneOpstelling(t, drieCloneRepos())

	_, err := svc.Clone([]string{"web-alpha", "../../etc"})
	if err == nil {
		t.Fatal("Clone accepteerde een repo die niet in de org-lijst staat")
	}
	if !strings.Contains(err.Error(), "org-lijst") {
		t.Errorf("fout = %v, wil een melding dat de repo niet in de org-lijst staat", err)
	}
	// Validatie gebeurt vóór de eerste clone, dus ook de geldige repo blijft
	// ongemoeid: de gebruiker krijgt een fout in plaats van een halve run.
	if calls := cloner.aanroepen(); len(calls) != 0 {
		t.Errorf("er is gecloned ondanks een ongeldige selectie: %v", calls)
	}
}

func TestCloneZonderSelectieOfCacheGeeftFout(t *testing.T) {
	t.Run("lege selectie", func(t *testing.T) {
		svc, _, _ := cloneOpstelling(t, drieCloneRepos())
		if _, err := svc.Clone(nil); err == nil {
			t.Fatal("lege selectie werd geaccepteerd")
		}
	})

	t.Run("nooit gesynchroniseerd", func(t *testing.T) {
		svc, _, _ := cloneOpstelling(t, nil)
		_, err := svc.Clone([]string{"web-alpha"})
		if err == nil {
			t.Fatal("clone zonder cache werd geaccepteerd")
		}
		if !strings.Contains(err.Error(), "synchroniseer") {
			t.Errorf("fout = %v, wil een melding dat er eerst gesynchroniseerd moet worden", err)
		}
	})
}

func TestCloneZonderProjectmapGeeftFout(t *testing.T) {
	svc, cloner, _ := cloneOpstelling(t, drieCloneRepos())
	svc.projects = NewProjectService(nil)

	_, err := svc.Clone([]string{"web-alpha"})
	if err == nil {
		t.Fatal("clone zonder ingestelde projectmap werd geaccepteerd")
	}
	if !strings.Contains(err.Error(), "projectmap") {
		t.Errorf("fout = %v, wil een melding over de ontbrekende projectmap", err)
	}
	if calls := cloner.aanroepen(); len(calls) != 0 {
		t.Errorf("er is gecloned zonder doelmap: %v", calls)
	}
}

func TestCloneWeigertTweedeGelijktijdigeRun(t *testing.T) {
	svc, cloner, _ := cloneOpstelling(t, drieCloneRepos())
	blokkeer := make(chan struct{})
	cloner.mu.Lock()
	cloner.blokkeer = blokkeer
	cloner.mu.Unlock()

	gestart := make(chan struct{})
	klaar := make(chan error, 1)
	go func() {
		close(gestart)
		_, err := svc.Clone([]string{"web-alpha"})
		klaar <- err
	}()
	<-gestart

	// Wacht tot de eerste run daadwerkelijk in de cloner hangt, zodat het slot
	// zeker geclaimd is voordat de tweede aanroep komt.
	wachtOpAanroep(t, cloner)

	if _, err := svc.Clone([]string{"web-beta"}); err == nil {
		t.Error("tweede gelijktijdige clone werd geaccepteerd")
	} else if !strings.Contains(err.Error(), "loopt al") {
		t.Errorf("fout = %v, wil een melding dat er al een clone loopt", err)
	}

	close(blokkeer)
	if err := <-klaar; err != nil {
		t.Fatalf("eerste clone: %v", err)
	}
}

func wachtOpAanroep(t *testing.T, cloner *fakeCloner) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cloner.aanroepen()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("eerste clone-run is nooit in de cloner aangekomen")
}

func TestCloneNegeertDubbeleNamen(t *testing.T) {
	svc, cloner, _ := cloneOpstelling(t, drieCloneRepos())

	res, err := svc.Clone([]string{"web-alpha", "WEB-ALPHA", " web-alpha "})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if res.Cloned != 1 {
		t.Errorf("Cloned = %d, wil 1 (dubbele namen samengevoegd)", res.Cloned)
	}
	if calls := cloner.aanroepen(); len(calls) != 1 {
		t.Errorf("aanroepen = %v, wil precies één clone", calls)
	}
}

func TestCloneURL(t *testing.T) {
	tests := []struct {
		fullName string
		wil      string
		wilFout  bool
	}{
		{fullName: "acme/web-alpha", wil: "git@github.com:acme/web-alpha.git"},
		{fullName: "acme/web-alpha/extra", wilFout: true},
		{fullName: "web-alpha", wilFout: true},
		{fullName: "", wilFout: true},
		{fullName: "acme/", wilFout: true},
	}
	for _, tt := range tests {
		got, err := cloneURL(tt.fullName)
		if tt.wilFout {
			if err == nil {
				t.Errorf("cloneURL(%q) = %q, wil een fout", tt.fullName, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("cloneURL(%q): %v", tt.fullName, err)
		} else if got != tt.wil {
			t.Errorf("cloneURL(%q) = %q, wil %q", tt.fullName, got, tt.wil)
		}
	}
}

func TestIsSafeDirName(t *testing.T) {
	veilig := []string{"web-alpha", "web_alpha", "web.alpha", "Web-Alpha42"}
	onveilig := []string{"", ".", "..", ".git", "../etc", "web/alpha", `web\alpha`, "/absoluut"}

	for _, n := range veilig {
		if !isSafeDirName(n) {
			t.Errorf("isSafeDirName(%q) = false, wil true", n)
		}
	}
	for _, n := range onveilig {
		if isSafeDirName(n) {
			t.Errorf("isSafeDirName(%q) = true, wil false", n)
		}
	}
}
