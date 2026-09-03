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

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/domain"
)

// fakeOrgLister is een nep-implementatie van orgRepoLister zonder netwerk.
type fakeOrgLister struct {
	mu    sync.Mutex
	repos []github.OrgRepo
	err   error
	calls int
	org   string
}

func (f *fakeOrgLister) ListOrgRepos(_ context.Context, org string) ([]github.OrgRepo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.org = org
	if f.err != nil {
		return nil, f.err
	}
	out := make([]github.OrgRepo, len(f.repos))
	copy(out, f.repos)
	return out, nil
}

// fakeOrgContents is een nep-implementatie van orgContentsGetter zonder netwerk.
type fakeOrgContents struct {
	mu    sync.Mutex
	body  map[string][]byte
	err   map[string]error
	calls int
}

func (f *fakeOrgContents) GetContentsRaw(_ context.Context, _, repo, _, _ string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if err, ok := f.err[repo]; ok {
		return nil, err
	}
	if body, ok := f.body[repo]; ok {
		return body, nil
	}
	return nil, github.ErrNotFound
}

func (f *fakeOrgContents) aantal() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeRemoteURL simuleert "git remote get-url origin" voor een vaste set
// lokale mappen; een map die er niet in staat levert een fout op (net als een
// checkout zonder origin-remote).
func fakeRemoteURL(remotes map[string]string) func(ctx context.Context, dir string) (string, error) {
	return func(_ context.Context, dir string) (string, error) {
		if r, ok := remotes[dir]; ok {
			return r, nil
		}
		return "", fmt.Errorf("geen origin-remote voor %s", dir)
	}
}

// orgSyncOpstelling bouwt een OrgSyncService met fakes en een storePath onder
// t.TempDir(), en zet de gegeven lokale projecten op de ProjectService.
func orgSyncOpstelling(t *testing.T, lister orgRepoLister, contents orgContentsGetter, remotes map[string]string, projects []domain.Project) *OrgSyncService {
	t.Helper()
	ps := NewProjectService(nil)
	ps.projects = projects
	return &OrgSyncService{
		projects:  ps,
		lister:    lister,
		contents:  contents,
		remoteURL: fakeRemoteURL(remotes),
		now:       time.Now,
		bezig:     map[string]bool{},
		storePath: filepath.Join(t.TempDir(), "org-sync.json"),
	}
}

func vijfOrgRepos() []github.OrgRepo {
	return []github.OrgRepo{
		{Name: "site-a", FullName: "acme/site-a", HTMLURL: "https://github.com/acme/site-a", PushedAt: "2026-01-01T00:00:00Z"},
		{Name: "site-b", FullName: "acme/site-b", HTMLURL: "https://github.com/acme/site-b", PushedAt: "2026-01-01T00:00:00Z"},
		{Name: "site-c", FullName: "acme/site-c", HTMLURL: "https://github.com/acme/site-c", PushedAt: "2026-01-01T00:00:00Z"},
		{Name: "site-d", FullName: "acme/site-d", HTMLURL: "https://github.com/acme/site-d", PushedAt: "2026-01-01T00:00:00Z", Archived: true},
		{Name: "site-e", FullName: "acme/site-e", HTMLURL: "https://github.com/acme/site-e", PushedAt: "2026-01-01T00:00:00Z"},
	}
}

func vijfOrgContents() *fakeOrgContents {
	return &fakeOrgContents{
		body: map[string][]byte{
			"site-a": []byte(`{"type":"wordpress_kinsta"}`),
			"site-b": []byte(`{"type":"wordpress_5_2"}`),
			"site-c": []byte(`{"type":"laravel_9"}`),
			// site-e bewust afwezig -> ErrNotFound
		},
	}
}

// (a) volledige sync met 3 org-repos + 1 archived + 1 zonder deploy_conf.
func TestSyncVolledigeRunClassificeertEnMatcht(t *testing.T) {
	localDir := t.TempDir()
	lister := &fakeOrgLister{repos: vijfOrgRepos()}
	contents := vijfOrgContents()
	remotes := map[string]string{localDir: "git@github.com:acme/site-a.git"}
	svc := orgSyncOpstelling(t, lister, contents, remotes, []domain.Project{
		{ID: "p1", DisplayName: "Site A", Path: localDir},
	})

	res, err := svc.Sync(false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if res.Org != "acme" {
		t.Fatalf("Org = %q, wil acme", res.Org)
	}
	if res.Totals.Repos != 5 {
		t.Errorf("Totals.Repos = %d, wil 5", res.Totals.Repos)
	}
	if res.Totals.Archived != 1 {
		t.Errorf("Totals.Archived = %d, wil 1", res.Totals.Archived)
	}
	if res.Totals.WordPress != 2 {
		t.Errorf("Totals.WordPress = %d, wil 2", res.Totals.WordPress)
	}
	if res.Totals.WordPressLocal != 1 {
		t.Errorf("Totals.WordPressLocal = %d, wil 1", res.Totals.WordPressLocal)
	}
	if res.Totals.WordPressMissing != 1 {
		t.Errorf("Totals.WordPressMissing = %d, wil 1", res.Totals.WordPressMissing)
	}
	if res.Scanned != 4 {
		t.Errorf("Scanned = %d, wil 4 (alle niet-archived repos)", res.Scanned)
	}

	byName := map[string]domain.OrgSyncRepo{}
	for _, r := range res.Repos {
		byName[r.Name] = r
	}
	if !byName["site-a"].IsWordPress || byName["site-a"].LocalPath != localDir {
		t.Errorf("site-a niet correct gematcht: %+v", byName["site-a"])
	}
	if !byName["site-b"].IsWordPress || byName["site-b"].LocalPath != "" {
		t.Errorf("site-b onverwacht: %+v", byName["site-b"])
	}
	if byName["site-c"].IsWordPress {
		t.Errorf("site-c (laravel) is onterecht als WordPress geclassificeerd")
	}
	if byName["site-e"].HasDeployConf {
		t.Errorf("site-e (404) heeft onterecht HasDeployConf=true")
	}

	// Sortering: de missende WordPress-repo (site-b) hoort vooraan te staan.
	if res.Repos[0].Name != "site-b" {
		t.Errorf("Repos[0] = %q, wil site-b (missende WordPress eerst)", res.Repos[0].Name)
	}
}

// (b)+(c) incrementeel gedrag: ongewijzigde PushedAt slaat de fetch over,
// een gewijzigde PushedAt fetcht precies die ene repo opnieuw, en force=true
// fetcht alles (behalve archived) opnieuw.
func TestSyncIncrementeelEnForce(t *testing.T) {
	lister := &fakeOrgLister{repos: vijfOrgRepos()}
	contents := vijfOrgContents()
	svc := orgSyncOpstelling(t, lister, contents, nil, []domain.Project{
		{ID: "p1", DisplayName: "Acme", Path: "/nergens"},
	})
	// Zorg dat org bepaald kan worden zonder een echte lokale match nodig te
	// hebben voor deze test: geef een remote voor het project.
	svc.remoteURL = fakeRemoteURL(map[string]string{"/nergens": "git@github.com:acme/iets.git"})

	if _, err := svc.Sync(false); err != nil {
		t.Fatalf("eerste Sync: %v", err)
	}
	if contents.aantal() != 4 {
		t.Fatalf("na eerste sync: contents-aanroepen = %d, wil 4", contents.aantal())
	}

	res2, err := svc.Sync(false)
	if err != nil {
		t.Fatalf("tweede Sync: %v", err)
	}
	if contents.aantal() != 4 {
		t.Errorf("tweede sync (ongewijzigd) deed extra fetches: contents-aanroepen = %d, wil nog steeds 4", contents.aantal())
	}
	if res2.FromCache != 4 {
		t.Errorf("FromCache = %d, wil 4", res2.FromCache)
	}
	if res2.Scanned != 0 {
		t.Errorf("Scanned = %d, wil 0", res2.Scanned)
	}

	// Wijzig de PushedAt van precies één repo.
	repos := vijfOrgRepos()
	repos[0].PushedAt = "2026-02-01T00:00:00Z" // site-a
	lister.repos = repos

	res3, err := svc.Sync(false)
	if err != nil {
		t.Fatalf("derde Sync: %v", err)
	}
	if contents.aantal() != 5 {
		t.Errorf("derde sync: contents-aanroepen = %d, wil 5 (4 + 1 nieuwe fetch)", contents.aantal())
	}
	if res3.Scanned != 1 {
		t.Errorf("Scanned = %d, wil 1", res3.Scanned)
	}
	if res3.FromCache != 3 {
		t.Errorf("FromCache = %d, wil 3", res3.FromCache)
	}

	// force=true fetcht alle niet-archived repos opnieuw.
	if _, err := svc.Sync(true); err != nil {
		t.Fatalf("force Sync: %v", err)
	}
	if contents.aantal() != 9 {
		t.Errorf("na force sync: contents-aanroepen = %d, wil 9 (5 + 4 opnieuw)", contents.aantal())
	}
}

// (d) kapotte JSON in deploy_conf.json geeft een warning en HasDeployConf=true.
func TestSyncKapotteDeployConfGeeftWarning(t *testing.T) {
	lister := &fakeOrgLister{repos: []github.OrgRepo{
		{Name: "site-x", FullName: "acme/site-x", PushedAt: "2026-01-01T00:00:00Z"},
	}}
	contents := &fakeOrgContents{body: map[string][]byte{"site-x": []byte("{niet geldige json")}}
	svc := orgSyncOpstelling(t, lister, contents, map[string]string{"/p": "git@github.com:acme/site-x.git"}, []domain.Project{
		{ID: "p1", DisplayName: "Site X", Path: "/p"},
	})

	res, err := svc.Sync(false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.Repos) != 1 || !res.Repos[0].HasDeployConf || res.Repos[0].DeployType != "" {
		t.Fatalf("repo-rij onverwacht: %+v", res.Repos)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("Warnings = %v, wil precies 1", res.Warnings)
	}
}

// (e) een fetch-fout op één repo laat de sync slagen met een warning, en de
// eerder bekende cache-waarde blijft behouden.
func TestSyncFetchFoutBehoudtCacheWaarde(t *testing.T) {
	repos := []github.OrgRepo{
		{Name: "site-y", FullName: "acme/site-y", PushedAt: "2026-01-01T00:00:00Z"},
	}
	lister := &fakeOrgLister{repos: repos}
	contents := &fakeOrgContents{body: map[string][]byte{"site-y": []byte(`{"type":"wordpress_kinsta"}`)}}
	remotes := map[string]string{"/p": "git@github.com:acme/site-y.git"}
	svc := orgSyncOpstelling(t, lister, contents, remotes, []domain.Project{
		{ID: "p1", DisplayName: "Site Y", Path: "/p"},
	})

	if _, err := svc.Sync(false); err != nil {
		t.Fatalf("eerste Sync: %v", err)
	}

	// Volgende sync: PushedAt wijzigt zodat een fetch nodig is, maar die fetch
	// mislukt nu.
	repos2 := []github.OrgRepo{
		{Name: "site-y", FullName: "acme/site-y", PushedAt: "2026-02-01T00:00:00Z"},
	}
	lister.repos = repos2
	contents.err = map[string]error{"site-y": fmt.Errorf("500 server error")}

	res, err := svc.Sync(false)
	if err != nil {
		t.Fatalf("tweede Sync mag niet falen op één kapotte repo: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("verwachtte een warning over de mislukte fetch")
	}
	if !res.Repos[0].HasDeployConf || res.Repos[0].DeployType != "wordpress_kinsta" {
		t.Errorf("cache-waarde niet behouden: %+v", res.Repos[0])
	}
}

// (f) org-bepaling: de meerderheid wint; zonder remotes is er geen org te
// bepalen.
func TestOrgBepalingMeerderheidEnFoutZonderRemotes(t *testing.T) {
	remotes := map[string]string{
		"/a": "git@github.com:acme/a.git",
		"/b": "git@github.com:acme/b.git",
		"/c": "git@github.com:other/c.git",
	}
	svc := orgSyncOpstelling(t, nil, nil, remotes, []domain.Project{
		{ID: "p1", DisplayName: "A", Path: "/a"},
		{ID: "p2", DisplayName: "B", Path: "/b"},
		{ID: "p3", DisplayName: "C", Path: "/c"},
	})

	org, err := svc.Org(context.Background())
	if err != nil {
		t.Fatalf("Org: %v", err)
	}
	if org != "acme" {
		t.Fatalf("Org = %q, wil acme (meerderheid)", org)
	}

	svc2 := orgSyncOpstelling(t, nil, nil, nil, []domain.Project{
		{ID: "p1", DisplayName: "Zonder remote", Path: "/nergens"},
	})
	if _, err := svc2.Org(context.Background()); err == nil {
		t.Fatal("verwachtte een fout zonder enige remote")
	}
}

// (g) Last() zonder eerdere sync geeft een fout; Last() na een sync hermatcht
// tegen de actuele lokale projecten.
func TestLastZonderBestandEnHermatching(t *testing.T) {
	lister := &fakeOrgLister{repos: []github.OrgRepo{
		{Name: "site-a", FullName: "acme/site-a", PushedAt: "2026-01-01T00:00:00Z"},
	}}
	contents := &fakeOrgContents{body: map[string][]byte{"site-a": []byte(`{"type":"wordpress_kinsta"}`)}}
	svc := orgSyncOpstelling(t, lister, contents, map[string]string{"/p": "git@github.com:acme/site-a.git"}, []domain.Project{
		{ID: "p1", DisplayName: "Site A", Path: "/p"},
	})

	if _, err := svc.Last(); err == nil {
		t.Fatal("verwachtte een fout: nog nooit gesynchroniseerd")
	}

	if _, err := svc.Sync(false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	last, err := svc.Last()
	if err != nil {
		t.Fatalf("Last: %v", err)
	}
	if last.Repos[0].LocalPath != "/p" {
		t.Fatalf("Last() matchte niet tegen /p: %+v", last.Repos[0])
	}

	// Verplaats de lokale checkout naar een ander pad.
	svc.projects.projects = []domain.Project{
		{ID: "p1", DisplayName: "Site A", Path: "/anders"},
	}
	svc.remoteURL = fakeRemoteURL(map[string]string{"/anders": "git@github.com:acme/site-a.git"})

	last2, err := svc.Last()
	if err != nil {
		t.Fatalf("Last na verplaatsing: %v", err)
	}
	if last2.Repos[0].LocalPath != "/anders" {
		t.Fatalf("Last() hermatchte niet naar /anders: %+v", last2.Repos[0])
	}
}

// (h) twee gelijktijdige syncs: de tweede krijgt "er loopt al ...".
func TestSyncLaatGeenTweeRunsTegelijk(t *testing.T) {
	lister := &fakeOrgLister{repos: vijfOrgRepos()}
	contents := vijfOrgContents()
	svc := orgSyncOpstelling(t, lister, contents, map[string]string{"/p": "git@github.com:acme/site-a.git"}, []domain.Project{
		{ID: "p1", DisplayName: "Site A", Path: "/p"},
	})

	if !svc.claim(orgSyncSlot) {
		t.Fatal("claim mislukte")
	}
	if _, err := svc.Sync(false); err == nil {
		t.Fatal("een tweede run werd toegestaan")
	}
	svc.release(orgSyncSlot)
}

// (i) een afgekapt (corrupt) cachebestand mag Sync niet laten falen: Sync
// herstelt zelf met een volledige herfetch en een warning, en schrijft er een
// weer geldig bestand overheen. Last() vóór die sync moet wel een duidelijke
// fout geven die zegt dat een nieuwe synchronisatie dit herstelt.
func TestSyncHersteltVanAfgekaptCachebestand(t *testing.T) {
	lister := &fakeOrgLister{repos: []github.OrgRepo{
		{Name: "site-a", FullName: "acme/site-a", PushedAt: "2026-01-01T00:00:00Z"},
	}}
	contents := &fakeOrgContents{body: map[string][]byte{"site-a": []byte(`{"type":"wordpress_kinsta"}`)}}
	svc := orgSyncOpstelling(t, lister, contents, map[string]string{"/p": "git@github.com:acme/site-a.git"}, []domain.Project{
		{ID: "p1", DisplayName: "Site A", Path: "/p"},
	})

	// Simuleer een crash/volle schijf halverwege een eerdere schrijfactie: een
	// afgekapt (niet-parseerbaar) JSON-bestand op het storePath.
	afgekapt := []byte(`{"result":{"org":"acme","repos":[{"name":"site-a","fullN`)
	if err := os.WriteFile(svc.storePath, afgekapt, 0o644); err != nil {
		t.Fatalf("afgekapte cache wegschrijven: %v", err)
	}

	if _, err := svc.Last(); err == nil {
		t.Fatal("Last() had een fout moeten geven op een corrupt cachebestand")
	} else if !strings.Contains(err.Error(), "synchronisatie") {
		t.Errorf("foutmelding van Last() noemt geen nieuwe synchronisatie als herstel: %v", err)
	}

	res, err := svc.Sync(false)
	if err != nil {
		t.Fatalf("Sync mag niet falen op een corrupt cachebestand: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("verwachtte een warning over het corrupte cachebestand")
	}
	foundWarning := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "corrupt") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("geen warning over 'corrupt' cache gevonden: %v", res.Warnings)
	}

	// Het bestand op storePath moet na de sync weer geldige JSON zijn.
	data, err := os.ReadFile(svc.storePath)
	if err != nil {
		t.Fatalf("cachebestand na sync lezen: %v", err)
	}
	var cache orgSyncCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("cachebestand na sync is nog steeds geen geldige JSON: %v", err)
	}

	// En Last() moet nu weer gewoon werken.
	if _, err := svc.Last(); err != nil {
		t.Fatalf("Last() na herstel: %v", err)
	}
}

// (j) DefaultOrgSyncPath valt terug op de systeem-temp-map als
// os.UserHomeDir() een fout geeft, in plaats van een relatief pad te bouwen.
func TestDefaultOrgSyncPathFallbackBijHomeFout(t *testing.T) {
	got := defaultOrgSyncPathFrom("", fmt.Errorf("$HOME niet gezet"))
	wil := filepath.Join(os.TempDir(), "rdm", "org-sync.json")
	if got != wil {
		t.Fatalf("defaultOrgSyncPathFrom bij fout = %q, wil %q", got, wil)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("defaultOrgSyncPathFrom bij fout gaf een relatief pad: %q", got)
	}
}

// (k) DefaultOrgSyncPath gebruikt gewoon ~/.config/rdm/org-sync.json als
// os.UserHomeDir() slaagt.
func TestDefaultOrgSyncPathMetGeldigeHome(t *testing.T) {
	got := defaultOrgSyncPathFrom("/Users/iemand", nil)
	wil := filepath.Join("/Users/iemand", ".config", "rdm", "org-sync.json")
	if got != wil {
		t.Fatalf("defaultOrgSyncPathFrom = %q, wil %q", got, wil)
	}
}

// (l) resolveOrgFromMatches is deterministisch bij een gelijke stand: de
// alfabetisch kleinste owner wint, ongeacht map-iteratievolgorde. Herhaald
// uitgevoerd omdat map-iteratie in Go bewust gerandomiseerd is.
func TestResolveOrgFromMatchesTiebreakIsDeterministisch(t *testing.T) {
	matches := []localMatch{
		{owner: "zulu", repo: "r1"},
		{owner: "alfa", repo: "r2"},
	}
	for i := 0; i < 20; i++ {
		org, err := resolveOrgFromMatches(matches)
		if err != nil {
			t.Fatalf("resolveOrgFromMatches (poging %d): %v", i, err)
		}
		if org != "alfa" {
			t.Fatalf("resolveOrgFromMatches (poging %d) = %q, wil altijd alfa (alfabetisch kleinste bij gelijke stand)", i, org)
		}
	}
}

// (m) een niet-parseerbare origin-remote levert een warning op met de
// projectnaam erin, en laat de sync niet falen.
func TestSyncOnparseerbareRemoteGeeftWarningMetProjectnaam(t *testing.T) {
	lister := &fakeOrgLister{repos: []github.OrgRepo{
		{Name: "site-a", FullName: "acme/site-a", PushedAt: "2026-01-01T00:00:00Z"},
	}}
	contents := &fakeOrgContents{body: map[string][]byte{"site-a": []byte(`{"type":"wordpress_kinsta"}`)}}
	remotes := map[string]string{
		"/goed":  "git@github.com:acme/site-a.git",
		"/kapot": "geen-url",
	}
	svc := orgSyncOpstelling(t, lister, contents, remotes, []domain.Project{
		{ID: "p1", DisplayName: "Site A", Path: "/goed"},
		{ID: "p2", DisplayName: "Kapotte Remote", Path: "/kapot"},
	})

	res, err := svc.Sync(false)
	if err != nil {
		t.Fatalf("Sync mag niet falen op een onparseerbare remote: %v", err)
	}

	foundWarning := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "Kapotte Remote") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("geen warning met projectnaam %q gevonden: %v", "Kapotte Remote", res.Warnings)
	}
}

// TestComputeTotalsArchivedRandgevallen legt de definitie van WordPressMissing
// vast. De frontend-filter voor "WordPress zonder lokale checkout" gebruikt
// dezelfde conditie (isWordPress && !localPath && !archived); zodra die hier
// uiteen gaan lopen, tonen de samenvattingskaart en de tabel andere getallen.
func TestComputeTotalsArchivedRandgevallen(t *testing.T) {
	repos := []domain.OrgSyncRepo{
		// Levend, WordPress, lokaal aanwezig -> Local.
		{FullName: "o/a", IsWordPress: true, LocalPath: "/p/a"},
		// Levend, WordPress, niet lokaal -> Missing.
		{FullName: "o/b", IsWordPress: true},
		// Gearchiveerd, WordPress, lokaal aanwezig -> Local (archived doet niet mee
		// zodra er een checkout is: de LocalPath-tak komt eerst).
		{FullName: "o/c", IsWordPress: true, LocalPath: "/p/c", Archived: true},
		// Gearchiveerd, WordPress, niet lokaal -> in geen van beide tellers.
		{FullName: "o/d", IsWordPress: true, Archived: true},
		// Geen WordPress -> nergens in de WordPress-tellers.
		{FullName: "o/e", DeployType: "laravel_9", Archived: true},
	}

	got := computeTotals(repos)

	if got.Repos != 5 {
		t.Errorf("Repos = %d, wil 5", got.Repos)
	}
	if got.WordPress != 4 {
		t.Errorf("WordPress = %d, wil 4", got.WordPress)
	}
	if got.WordPressLocal != 2 {
		t.Errorf("WordPressLocal = %d, wil 2 (o/a en o/c)", got.WordPressLocal)
	}
	if got.WordPressMissing != 1 {
		t.Errorf("WordPressMissing = %d, wil 1 (alleen o/b; o/d is gearchiveerd)", got.WordPressMissing)
	}
	if got.Archived != 3 {
		t.Errorf("Archived = %d, wil 3", got.Archived)
	}
}
