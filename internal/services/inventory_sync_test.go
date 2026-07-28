package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

type fakeSHASource struct {
	// mu beschermt de tellers/slices: de syncer roept deze fake vanuit
	// meerdere goroutines aan.
	mu       sync.Mutex
	shas     map[string]string // "owner/repo@branch" -> sha
	err      error
	calls    int
	locals   map[string]string // path -> lokale origin-sha
	def      map[string]string // path -> default branch
	remote   map[string]string // path -> origin-URL
	fetched  []string
	fetchErr error
}

func (f *fakeSHASource) BranchSHA(_ context.Context, repo, branch string) (string, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	sha, ok := f.shas[repo+"@"+branch]
	if !ok {
		return "", errors.New("niet gevonden")
	}
	return sha, nil
}

func (f *fakeSHASource) LocalRefSHA(_ context.Context, path, ref string) (string, error) {
	sha, ok := f.locals[path]
	if !ok {
		return "", errors.New("ref onbekend")
	}
	return sha, nil
}

func (f *fakeSHASource) DefaultBranchName(_ context.Context, path string) (string, error) {
	b, ok := f.def[path]
	if !ok {
		return "", errors.New("geen default branch")
	}
	return b, nil
}

func (f *fakeSHASource) RemoteURL(_ context.Context, path string) (string, error) {
	u, ok := f.remote[path]
	if !ok {
		return "", errors.New("geen remote")
	}
	return u, nil
}

func (f *fakeSHASource) FetchRepo(_ context.Context, path string) error {
	f.mu.Lock()
	f.fetched = append(f.fetched, path)
	f.mu.Unlock()
	return f.fetchErr
}

// gefetcht geeft een kopie van de gefetchte paden.
func (f *fakeSHASource) gefetcht() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.fetched...)
}

// shaCalls geeft het aantal SHA-checks.
func (f *fakeSHASource) shaCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func syncTestProjects() []domain.Project {
	return []domain.Project{
		{ID: "achter", DisplayName: "Achterloper", Path: "/p/achter"},
		{ID: "gelijk", DisplayName: "Gelijk", Path: "/p/gelijk"},
	}
}

func newSyncFake() *fakeSHASource {
	return &fakeSHASource{
		def:    map[string]string{"/p/achter": "release/1.0.x", "/p/gelijk": "release/2.0.x"},
		remote: map[string]string{"/p/achter": "git@github.com:o/achter.git", "/p/gelijk": "git@github.com:o/gelijk.git"},
		locals: map[string]string{"/p/achter": "oud-sha", "/p/gelijk": "zelfde-sha"},
		shas: map[string]string{
			"o/achter@release/1.0.x": "nieuw-sha",
			"o/gelijk@release/2.0.x": "zelfde-sha",
		},
	}
}

func TestSyncFetchtAlleenAchterlopers(t *testing.T) {
	fake := newSyncFake()
	syncer := newInventorySyncer(fake)

	syncer.Sync(context.Background(), syncTestProjects())

	if got := fake.gefetcht(); len(got) != 1 || got[0] != "/p/achter" {
		t.Fatalf("gefetchte repo's = %v, want alleen /p/achter", got)
	}
}

func TestSyncCachetShaChecks(t *testing.T) {
	fake := newSyncFake()
	syncer := newInventorySyncer(fake)
	projects := syncTestProjects()

	syncer.Sync(context.Background(), projects)
	na1 := fake.shaCalls()
	syncer.Sync(context.Background(), projects)
	if got := fake.shaCalls(); got != na1 {
		t.Errorf("SHA-calls = %d na tweede Sync, want %d (cache)", got, na1)
	}

	// Na de TTL mag hij weer vragen.
	syncer.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	syncer.Sync(context.Background(), projects)
	if got := fake.shaCalls(); got <= na1 {
		t.Errorf("SHA-calls = %d na TTL, want meer dan %d", got, na1)
	}
}

func TestSyncIsBestEffortBijApiFout(t *testing.T) {
	fake := newSyncFake()
	fake.err = errors.New("401 unauthorized")
	syncer := newInventorySyncer(fake)

	// Mag niet paniekeren en niets fetchen: het overzicht valt terug op de
	// bestaande lokale stand.
	syncer.Sync(context.Background(), syncTestProjects())

	if got := fake.gefetcht(); len(got) != 0 {
		t.Errorf("er is gefetcht ondanks een API-fout: %v", got)
	}
}

func TestSyncFetchtWanneerLokaleRefOntbreekt(t *testing.T) {
	fake := newSyncFake()
	delete(fake.locals, "/p/achter") // repo nog nooit gefetcht
	syncer := newInventorySyncer(fake)

	syncer.Sync(context.Background(), syncTestProjects())

	found := false
	for _, p := range fake.gefetcht() {
		if p == "/p/achter" {
			found = true
		}
	}
	if !found {
		t.Errorf("ontbrekende lokale ref moet een fetch opleveren, gefetcht: %v", fake.fetched)
	}
}

func TestSyncSlaatProjectZonderRemoteOver(t *testing.T) {
	fake := newSyncFake()
	delete(fake.remote, "/p/achter")
	syncer := newInventorySyncer(fake)

	syncer.Sync(context.Background(), syncTestProjects())

	for _, p := range fake.gefetcht() {
		if p == "/p/achter" {
			t.Errorf("project zonder remote mag niet gefetcht worden")
		}
	}
}

// TestSyncDedupliceertGelijktijdigeAanroepen: de drie overzichten (WordPress,
// Plugins, Themes) delen één syncer. Worden ze vlak na elkaar geopend, dan mag
// dat niet leiden tot meerdere volledige sweeps over dezelfde projecten — dat
// zou de TTL-cache omzeilen en dubbele fetches op dezelfde repo veroorzaken.
func TestSyncDedupliceertGelijktijdigeAanroepen(t *testing.T) {
	fake := newSyncFake()
	syncer := newInventorySyncer(fake)
	projects := syncTestProjects()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			syncer.Sync(context.Background(), projects)
		}()
	}
	wg.Wait()

	// Twee projecten, dus precies twee SHA-checks en één fetch (de achterloper).
	if got := fake.shaCalls(); got != 2 {
		t.Errorf("SHA-calls = %d bij 3 gelijktijdige Syncs, want 2 (één per project)", got)
	}
	if got := fake.gefetcht(); len(got) != 1 {
		t.Errorf("fetches = %v, want precies één", got)
	}
}

func TestMarkAllCheckedVoorkomtDirecteHercheck(t *testing.T) {
	fake := newSyncFake()
	syncer := newInventorySyncer(fake)
	projects := syncTestProjects()

	// Na een handmatige "Fetch alles" is de lokale stand al actueel; een
	// direct volgende Sync hoeft niet opnieuw bij de API langs.
	syncer.MarkAllChecked(projects)
	syncer.Sync(context.Background(), projects)

	if got := fake.shaCalls(); got != 0 {
		t.Errorf("SHA-calls = %d na MarkAllChecked, want 0", got)
	}
}
