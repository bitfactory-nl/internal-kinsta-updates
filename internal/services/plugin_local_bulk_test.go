package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
)

// echteReferentiePad is de echte referentie-installatie op deze machine. De
// tests die 'm gebruiken slaan zichzelf over als hij er niet is, zodat de
// suite op een andere machine gewoon groen blijft — maar op déze machine
// bewijzen ze dat het echte parsepad (niet alleen een shell-steekproef) werkt
// op de 329 echte betaalde plugins die erin staan.
const echteReferentiePad = "/Users/jeffreyt/Projects/internal-wordpress-paid-plugins"

func referentieOfSkip(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(echteReferentiePad, "public", "wp-content", "plugins")); err != nil {
		t.Skipf("geen echte referentie-installatie op deze machine: %v", err)
	}
}

func TestListLocalPaidPluginsTegenEchteReferentieInstallatie(t *testing.T) {
	referentieOfSkip(t)
	svc := &PluginService{cfg: &config.Global{}}
	svc.cfg.PluginRepo.ReferenceProjectPath = echteReferentiePad

	lijst, err := svc.ListLocalPaidPlugins()
	if err != nil {
		t.Fatalf("ListLocalPaidPlugins: %v", err)
	}
	if len(lijst) < 300 {
		t.Fatalf("lijst = %d regels, wil minstens 300 (de referentie-installatie heeft er ~329)", len(lijst))
	}

	perSlug := map[string]LocalPaidPlugin{}
	var fouten int
	for _, r := range lijst {
		if r.Error != "" {
			fouten++
			continue
		}
		perSlug[r.Slug] = r
		if r.Source != "referentie" {
			t.Errorf("plugin %q heeft Source = %q, wil referentie", r.Slug, r.Source)
		}
	}
	// Een paar echte, bekende betaalde plugins horen er met een versie in te staan.
	for _, slug := range []string{"advanced-custom-fields-pro", "gravityforms", "facetwp"} {
		if perSlug[slug].Version == "" {
			t.Errorf("verwachtte een versie voor %q, kreeg %+v", slug, perSlug[slug])
		}
	}
	// Enkele bekende rommel (geen echte plugin, of een mu-plugins-bundel) mag
	// als fout gemeld worden, maar niet de hele lijst domineren.
	if fouten > 10 {
		t.Errorf("%d van de %d regels is een fout; dat is meer dan de bekende uitzonderingen", fouten, len(lijst))
	}
}

// setupTweeProjecten maakt twee onafhankelijke git-checkouts onder één
// gedeelde root, elk met dezelfde plugin op zijn eigen versie — nodig om
// ApplyPluginToProjects (één plugin, meerdere projecten) tegen echte,
// losstaande git-repo's te testen.
func setupTweeProjecten(t *testing.T, slug string, versies map[string]string) (ps *ProjectService, ids map[string]string, dirs map[string]string) {
	t.Helper()
	root := t.TempDir()
	ids = map[string]string{}
	dirs = map[string]string{}

	for naam, versie := range versies {
		projectDir := filepath.Join(root, naam)
		pluginDir := filepath.Join(projectDir, "public", "wp-content", "plugins", slug)
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		php := "<?php\n/**\n * Plugin Name: " + slug + "\n * Version: " + versie + "\n */\n"
		if err := os.WriteFile(filepath.Join(pluginDir, slug+".php"), []byte(php), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitApply(t, projectDir, "init")
		runGitApply(t, projectDir, "checkout", "-b", "release/1.0.x")
		runGitApply(t, projectDir, "config", "user.email", "test@example.com")
		runGitApply(t, projectDir, "config", "user.name", "Test")
		runGitApply(t, projectDir, "add", "-A")
		runGitApply(t, projectDir, "commit", "-m", "initial")
		dirs[naam] = projectDir
	}

	ps = NewProjectService([]string{root})
	if _, err := ps.Scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, p := range ps.List() {
		for naam, dir := range dirs {
			if p.Path == dir {
				ids[naam] = p.ID
			}
		}
	}
	for naam := range versies {
		if ids[naam] == "" {
			t.Fatalf("project %q niet gevonden na scan", naam)
		}
	}
	return ps, ids, dirs
}

func TestApplyPluginToProjectsMeerdereProjecten(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{
		"siteA": "6.2.0",
		"siteB": "6.1.0",
	})
	refDir := lokaleMapMet(t, nil) // t.TempDir() als basis
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps)}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"], ids["siteB"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	if res.Slug != "acf-pro" || res.Version != "6.4.1" {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Results) != 2 {
		t.Fatalf("results = %+v", res.Results)
	}
	perProject := map[string]BulkApplyProjectResult{}
	for _, r := range res.Results {
		perProject[r.ProjectID] = r
	}
	a := perProject[ids["siteA"]]
	if a.Status != "updated" || a.From != "6.2.0" || a.To != "6.4.1" {
		t.Errorf("siteA = %+v", a)
	}
	b := perProject[ids["siteB"]]
	if b.Status != "updated" || b.From != "6.1.0" || b.To != "6.4.1" {
		t.Errorf("siteB = %+v", b)
	}

	// Beide projecten hebben precies één nieuwe, gecommitte wijziging.
	for naam, dir := range dirs {
		if status := runGitApply(t, dir, "status", "--porcelain"); status != "" {
			t.Errorf("%s: werkmap niet schoon:\n%s", naam, status)
		}
		bericht := runGitApply(t, dir, "log", "-1", "--format=%s")
		if !strings.Contains(bericht, "acf-pro") || !strings.Contains(bericht, "referentie-installatie") {
			t.Errorf("%s: commitbericht = %q", naam, bericht)
		}
	}
}

// TestApplyPluginToProjectsIsoleertFoutPerProject bewijst het hele punt van de
// bulk-methode: een project dat niet lukt mag de andere projecten niet
// tegenhouden. Het faalgeval is hier een project zonder pluginmap — precies de
// fout die in de praktijk voorbijkwam ("no such file or directory").
func TestApplyPluginToProjectsIsoleertFoutPerProject(t *testing.T) {
	ps, ids, _ := setupTweeProjecten(t, "acf-pro", map[string]string{
		"siteA": "6.2.0",
		"siteB": "6.1.0",
	})
	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps)}

	// Een project-ID dat niet bestaat kan onmogelijk lukken; siteB moet dat
	// overleven.
	res, err := svc.ApplyPluginToProjects("acf-pro", []string{"bestaat-niet-als-project", ids["siteB"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	perProject := map[string]BulkApplyProjectResult{}
	for _, r := range res.Results {
		perProject[r.ProjectID] = r
	}
	if perProject["bestaat-niet-als-project"].Status != "error" {
		t.Errorf("onbekend project had een fout moeten geven: %+v", perProject["bestaat-niet-als-project"])
	}
	if perProject[ids["siteB"]].Status != "updated" {
		t.Errorf("siteB had gewoon door moeten gaan: %+v", perProject[ids["siteB"]])
	}
}

// TestApplyPluginToProjectsStashtOpenstaandWerk dekt eis 1 uit de praktijkrun:
// openstaand werk hoort niet te blokkeren maar automatisch gestasht te worden,
// mét een melding welke stash dat is — anders lijkt dat werk verdwenen.
func TestApplyPluginToProjectsStashtOpenstaandWerk(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	// siteA heeft iets staged én een los, nieuw bestand: beide moeten mee de
	// stash in, anders is de werkmap niet schoon voor de branchwissel.
	if err := os.WriteFile(filepath.Join(dirs["siteA"], "los.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitApply(t, dirs["siteA"], "add", "los.txt")
	if err := os.WriteFile(filepath.Join(dirs["siteA"], "nieuw.txt"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps)}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	r := res.Results[0]
	if r.Status != "updated" {
		t.Fatalf("openstaand werk hoort niet te blokkeren: %+v", r)
	}
	if r.Stash == "" {
		t.Error("er is werk gestasht, maar dat wordt niet gemeld — dan lijkt het verdwenen")
	}
	if !strings.Contains(r.Stash, "stash@{0}") {
		t.Errorf("Stash = %q, wil een herkenbare stash-aanduiding", r.Stash)
	}
	// Het werk staat echt in de stash en is terug te halen.
	lijst := runGitApply(t, dirs["siteA"], "stash", "list")
	if !strings.Contains(lijst, "auto-stash") {
		t.Errorf("stash list = %q, wil de auto-stash van de tool", lijst)
	}
	inhoud := runGitApply(t, dirs["siteA"], "stash", "show", "--include-untracked", "--name-only", "stash@{0}")
	for _, wil := range []string{"los.txt", "nieuw.txt"} {
		if !strings.Contains(inhoud, wil) {
			t.Errorf("stash bevat %q niet:\n%s", wil, inhoud)
		}
	}
	// En de plugin-commit is er niet door vervuild.
	gecommit := runGitApply(t, dirs["siteA"], "show", "--stat", "--format=", "HEAD")
	for _, nietIn := range []string{"los.txt", "nieuw.txt"} {
		if strings.Contains(gecommit, nietIn) {
			t.Errorf("%q is meegelift in de plugin-commit:\n%s", nietIn, gecommit)
		}
	}
}

func TestApplyPluginToProjectsOnbekendeSlug(t *testing.T) {
	ps, ids, _ := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "1.0.0"})
	svc := &PluginService{cfg: &config.Global{}, projects: ps, git: NewGitService(ps)}

	if _, err := svc.ApplyPluginToProjects("bestaat-niet", []string{ids["siteA"]}); err == nil {
		t.Error("een onbekende slug hoort een fout te geven (er is geen bron om uit te lezen)")
	}
}

func TestApplyPluginToProjectsLegeInvoer(t *testing.T) {
	svc := &PluginService{cfg: &config.Global{}}
	if _, err := svc.ApplyPluginToProjects("", []string{"p1"}); err == nil {
		t.Error("lege slug had een fout moeten geven")
	}
	if _, err := svc.ApplyPluginToProjects("acf-pro", nil); err == nil {
		t.Error("geen projecten had een fout moeten geven")
	}
}

func TestListLocalPaidPluginsReferentieOverschrijftMap(t *testing.T) {
	mapDir := lokaleMapMet(t, map[string][]byte{
		"acf-pro.zip": bouwPluginZip(t, "acf-pro", "6.0.0", nil),
	})
	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1") // nieuwer, moet winnen
	lokalePluginMap(t, refPluginsDir, "alleen-referentie", "1.0.0")

	svc := &PluginService{cfg: &config.Global{}}
	svc.cfg.PluginRepo.LocalDir = mapDir
	svc.cfg.PluginRepo.ReferenceProjectPath = refDir

	lijst, err := svc.ListLocalPaidPlugins()
	if err != nil {
		t.Fatalf("ListLocalPaidPlugins: %v", err)
	}
	perSlug := map[string]LocalPaidPlugin{}
	for _, r := range lijst {
		if r.Slug != "" {
			perSlug[r.Slug] = r
		}
	}
	// Precies één rij voor acf-pro, en die komt uit de referentie.
	acf := perSlug["acf-pro"]
	if acf.Version != "6.4.1" || acf.Source != "referentie" {
		t.Errorf("acf-pro = %+v, wil versie 6.4.1 uit de referentie", acf)
	}
	var acfCount int
	for _, r := range lijst {
		if r.Slug == "acf-pro" {
			acfCount++
		}
	}
	if acfCount != 1 {
		t.Errorf("acf-pro komt %d keer voor, wil precies 1 (dedupliceren)", acfCount)
	}
	// Een plugin die alleen in de referentie zit, moet ook meekomen.
	if perSlug["alleen-referentie"].Version != "1.0.0" || perSlug["alleen-referentie"].Source != "referentie" {
		t.Errorf("alleen-referentie = %+v", perSlug["alleen-referentie"])
	}
}

func TestListLocalPaidPluginsSourceTagOpAlleenMap(t *testing.T) {
	mapDir := lokaleMapMet(t, map[string][]byte{
		"acf-pro.zip": bouwPluginZip(t, "acf-pro", "6.0.0", nil),
	})
	svc := &PluginService{cfg: &config.Global{}}
	svc.cfg.PluginRepo.LocalDir = mapDir

	lijst, err := svc.ListLocalPaidPlugins()
	if err != nil {
		t.Fatalf("ListLocalPaidPlugins: %v", err)
	}
	if len(lijst) != 1 || lijst[0].Source != "map" {
		t.Errorf("lijst = %+v, wil 1 rij met Source=map", lijst)
	}
}

func TestLocalDirConfiguredMetAlleenReferentie(t *testing.T) {
	svc := &PluginService{cfg: &config.Global{}}
	if svc.LocalDirConfigured() {
		t.Error("zonder enige bron hoort dit false te zijn")
	}
	svc.cfg.PluginRepo.ReferenceProjectPath = "/ergens"
	if !svc.LocalDirConfigured() {
		t.Error("met alleen een referentiepad hoort dit true te zijn")
	}
}

// TestApplyLocalPluginsVanuitReferentieKrijgtEigenBranch bewijst het punt dat
// de gebruiker maakte: een update vanuit de referentie-installatie mag nooit
// rechtstreeks op de huidige checkout landen (dat kan een productie-branch
// zijn), maar hoort op zijn eigen branch te komen. De release/1.0.x-branch
// waar het project op begon blijft ongewijzigd op zijn originele commit.
func TestApplyLocalPluginsVanuitReferentieKrijgtEigenBranch(t *testing.T) {
	ps, projectID, projectDir := setupApplyTestProject(t, "acf-pro", "6.3.0")
	origHead := runGitApply(t, projectDir, "rev-parse", "release/1.0.x")

	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps)}

	res, err := svc.ApplyLocalPlugins(projectID, []string{"acf-pro"})
	if err != nil {
		t.Fatalf("ApplyLocalPlugins: %v", err)
	}
	// De branchnaam moet de plugin én de versie bevatten (eis 2 uit de
	// praktijkrun): dat is wat een PR-overzicht leesbaar maakt.
	if res.Branch != "chore/plugin-acf-pro-6.4.1" {
		t.Errorf("Branch = %q, wil chore/plugin-acf-pro-6.4.1", res.Branch)
	}
	if len(res.Plugins) != 1 || res.Plugins[0].Status != "updated" {
		t.Fatalf("resultaat = %+v", res.Plugins)
	}

	huidig := runGitApply(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	if huidig != res.Branch {
		t.Errorf("checkout na afloop = %q, wil %q", huidig, res.Branch)
	}
	if head := runGitApply(t, projectDir, "rev-parse", "release/1.0.x"); head != origHead {
		t.Errorf("release/1.0.x is verschoven van %q naar %q; die branch hoort onaangeraakt te blijven", origHead, head)
	}
}

// TestApplyPluginToProjectsReferentieBranchPerProject bewijst dat de
// bulk-variant (main-menu, meerdere projecten tegelijk) dezelfde
// branch-isolatie krijgt, en dat elk project zijn eigen branch-veld
// terugkrijgt in het resultaat.
func TestApplyPluginToProjectsReferentieBranchPerProject(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{
		"siteA": "6.2.0",
		"siteB": "6.1.0",
	})
	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps)}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"], ids["siteB"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	for _, r := range res.Results {
		if r.Status != "updated" {
			t.Fatalf("verwachtte updated: %+v", r)
		}
		if r.Branch != "chore/plugin-acf-pro-6.4.1" {
			t.Errorf("%s: Branch = %q, wil chore/plugin-acf-pro-6.4.1", r.ProjectName, r.Branch)
		}
	}
	for naam, dir := range dirs {
		huidig := runGitApply(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
		if huidig == "release/1.0.x" {
			t.Errorf("%s: staat nog op release/1.0.x; had naar een eigen branch moeten wisselen", naam)
		}
	}
}

// TestApplyLocalPluginsIdentiekeVersieIsUnchanged dekt het geval waar de
// gebruiker op wees: staat de referentie-versie er al byte-identiek in, dan is
// er niets bij te werken. Dat moet als "unchanged" terugkomen en niet als een
// kale "git commit: exit status 1" — die fout vertelt niet dat er simpelweg
// niets te doen was.
func TestApplyLocalPluginsIdentiekeVersieIsUnchanged(t *testing.T) {
	ps, projectID, projectDir := setupApplyTestProject(t, "acf-pro", "6.3.0")
	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.3.0")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps)}

	// Eerste keer is een echte wijziging (de referentie-map heeft andere inhoud
	// bij hetzelfde versienummer): die hoort gewoon te committen.
	res1, err := svc.ApplyLocalPlugins(projectID, []string{"acf-pro"})
	if err != nil {
		t.Fatalf("eerste ApplyLocalPlugins: %v", err)
	}
	if len(res1.Plugins) != 1 || res1.Plugins[0].Status != "updated" {
		t.Fatalf("eerste keer = %+v, wil updated", res1.Plugins)
	}
	logNa1 := runGitApply(t, projectDir, "log", "--oneline")

	// Tweede keer is byte-identiek: een echte no-op.
	res2, err := svc.ApplyLocalPlugins(projectID, []string{"acf-pro"})
	if err != nil {
		t.Fatalf("tweede ApplyLocalPlugins: %v", err)
	}
	if len(res2.Plugins) != 1 {
		t.Fatalf("tweede keer = %+v", res2.Plugins)
	}
	p := res2.Plugins[0]
	if p.Status != "unchanged" {
		t.Errorf("Status = %q (error=%q), wil unchanged", p.Status, p.Error)
	}
	if p.Error != "" {
		t.Errorf("een no-op hoort geen foutmelding te geven, kreeg %q", p.Error)
	}
	// Geen tweede, lege commit en een schone werkmap.
	if logNa2 := runGitApply(t, projectDir, "log", "--oneline"); logNa2 != logNa1 {
		t.Errorf("er is een commit bijgekomen voor een no-op:\nvoor:\n%s\nna:\n%s", logNa1, logNa2)
	}
	if status := runGitApply(t, projectDir, "status", "--porcelain"); status != "" {
		t.Errorf("werkmap niet schoon na een no-op:\n%s", status)
	}
}

// fakePluginPulls is een pluginPulls-dubbel: legt vast waarmee de PR is
// aangemaakt, zonder netwerk.
type fakePluginPulls struct {
	bestaand    *github.PullRequest
	head, base  string
	titel, body string
	aangemaakt  int

	// toegang bepaalt wat GetRepoAccess teruggeeft; nil = push mag, alles mag.
	toegang    *github.RepoAccess
	toegangErr error
	// gemerged legt vast met welk nummer en welke methode er gemerged is.
	gemergedNr     int
	gemergdMethode string
	mergeErr       error
}

func (f *fakePluginPulls) FindOpenPull(_ context.Context, _, _, _ string) (*github.PullRequest, error) {
	return f.bestaand, nil
}

func (f *fakePluginPulls) CreatePull(_ context.Context, _, _, head, base, title, body string) (*github.PullRequest, error) {
	f.aangemaakt++
	f.head, f.base, f.titel, f.body = head, base, title, body
	return &github.PullRequest{Number: 7, HTMLURL: "https://github.com/acme/web-test/pull/7"}, nil
}

func (f *fakePluginPulls) GetRepoAccess(_ context.Context, _, _ string) (*github.RepoAccess, error) {
	if f.toegangErr != nil {
		return nil, f.toegangErr
	}
	if f.toegang != nil {
		return f.toegang, nil
	}
	return &github.RepoAccess{CanPush: true, AllowMergeCommit: true, AllowSquashMerge: true, AllowRebaseMerge: true}, nil
}

func (f *fakePluginPulls) MergePull(_ context.Context, _, _ string, number int, method string) (*github.MergeResult, error) {
	if f.mergeErr != nil {
		return nil, f.mergeErr
	}
	f.gemergedNr, f.gemergdMethode = number, method
	return &github.MergeResult{Merged: true, SHA: "deadbeef", Message: "Pull Request successfully merged"}, nil
}

// metPushDoelwit geeft het project een GitHub-origin die bij het pushen naar een
// lokale bare repo wordt omgeleid (git pushInsteadOf). Zo draait het echte
// push-pad — inclusief branchnaam op de remote — zonder netwerk, terwijl
// `git remote get-url origin` nog steeds de GitHub-URL teruggeeft die de tool
// in owner/repo moet kunnen splitsen.
func metPushDoelwit(t *testing.T, projectDir string) (bareDir string) {
	t.Helper()
	bareDir = filepath.Join(t.TempDir(), "origin.git")
	runGitApply(t, t.TempDir(), "init", "--bare", "-q", bareDir)
	const ghURL = "https://github.com/acme/web-test.git"
	runGitApply(t, projectDir, "remote", "add", "origin", ghURL)
	// insteadOf (niet pushInsteadOf): zo werken fetch én push naar de lokale
	// bare repo. GitService.RemoteURL leest de geconfigureerde URL via
	// `git config --get`, dus die geeft nog steeds de GitHub-URL terug en
	// owner/repo blijft te bepalen.
	runGitApply(t, projectDir, "config", "url."+bareDir+".insteadOf", ghURL)
	// Eerste push zet de default branch op de remote, zodat origin/<branch>
	// bestaat om van af te takken.
	huidig := runGitApply(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	runGitApply(t, projectDir, "push", "-q", "--set-upstream", "origin", huidig)
	return bareDir
}

// TestApplyPluginToProjectsPusthEnOpentPR dekt eis 2 uit de praktijkrun: na de
// commit moet de branch gepusht worden en er meteen een PR op staan.
func TestApplyPluginToProjectsPusthEnOpentPR(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	bare := metPushDoelwit(t, dirs["siteA"])

	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	nep := &fakePluginPulls{}
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps), pulls: nep}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	r := res.Results[0]
	if r.Status != "updated" || r.PullRequestError != "" {
		t.Fatalf("resultaat = %+v", r)
	}
	if r.PullRequestURL != "https://github.com/acme/web-test/pull/7" {
		t.Errorf("PullRequestURL = %q", r.PullRequestURL)
	}

	// De branch staat echt op de remote, onder de naam met plugin en versie.
	remoteBranches := runGitApply(t, dirs["siteA"], "--git-dir="+bare, "branch", "--list")
	if !strings.Contains(remoteBranches, "chore/plugin-acf-pro-6.4.1") {
		t.Errorf("remote branches = %q, wil chore/plugin-acf-pro-6.4.1", remoteBranches)
	}

	// En de PR is met de juiste head/base en een informatieve titel aangemaakt.
	if nep.aangemaakt != 1 {
		t.Errorf("CreatePull %d keer aangeroepen, wil 1", nep.aangemaakt)
	}
	if nep.head != "chore/plugin-acf-pro-6.4.1" || nep.base != "release/1.0.x" {
		t.Errorf("head = %q, base = %q", nep.head, nep.base)
	}
	if !strings.Contains(nep.titel, "acf-pro") || !strings.Contains(nep.titel, "6.4.1") {
		t.Errorf("PR-titel = %q, wil plugin en versie erin", nep.titel)
	}
	if !strings.Contains(nep.body, "6.2.0 → 6.4.1") {
		t.Errorf("PR-body = %q, wil de versiesprong erin", nep.body)
	}
}

// TestApplyPluginToProjectsHergebruiktOpenPR: staat er al een open PR op deze
// branch, dan hoort die hergebruikt te worden in plaats van een tweede te
// openen (GitHub zou dat weigeren).
func TestApplyPluginToProjectsHergebruiktOpenPR(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	metPushDoelwit(t, dirs["siteA"])

	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	nep := &fakePluginPulls{bestaand: &github.PullRequest{HTMLURL: "https://github.com/acme/web-test/pull/3"}}
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps), pulls: nep}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	r := res.Results[0]
	if r.PullRequestURL != "https://github.com/acme/web-test/pull/3" {
		t.Errorf("PullRequestURL = %q, wil de bestaande PR", r.PullRequestURL)
	}
	if nep.aangemaakt != 0 {
		t.Error("er is een tweede PR aangemaakt terwijl er al een open stond")
	}
}

// TestApplyPluginToProjectsPRFoutIsGeenMislukteUpdate: zonder GitHub-remote kan
// er geen PR komen, maar de commit is dan wél gelukt. Dat moet als aparte
// melding terugkomen en niet de update als mislukt bestempelen.
func TestApplyPluginToProjectsPRFoutIsGeenMislukteUpdate(t *testing.T) {
	ps, ids, _ := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps), pulls: &fakePluginPulls{}}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	r := res.Results[0]
	if r.Status != "updated" {
		t.Errorf("Status = %q, wil updated: de commit is gelukt", r.Status)
	}
	if r.PullRequestError == "" {
		t.Error("zonder remote hoort er een PR-melding te zijn, geen stilte")
	}
	if r.PullRequestURL != "" {
		t.Errorf("PullRequestURL = %q, wil leeg", r.PullRequestURL)
	}
}

// TestApplyLocalPluginsCommitOndanksFalendePreCommitHook dekt eis 3 uit de
// praktijkrun: een project met een pre-commit-hook die faalt (husky + ESLint
// die op bestaande projectcode struikelt) hield de plugin-update tegen. De
// commit gaat daarom met --no-verify.
func TestApplyLocalPluginsCommitOndanksFalendePreCommitHook(t *testing.T) {
	ps, projectID, projectDir := setupApplyTestProject(t, "acf-pro", "6.3.0")

	// Een pre-commit-hook die altijd faalt, zoals een kapotte lint-config doet.
	hookDir := filepath.Join(projectDir, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := "#!/bin/sh\necho 'Running ESLint...' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(hookDir, "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	// Bewijs dat de hook echt bijt: een gewone commit hoort te falen.
	if err := os.WriteFile(filepath.Join(projectDir, "controle.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitApply(t, projectDir, "add", "controle.txt")
	if out, err := gewoneCommitPoging(projectDir); err == nil {
		t.Fatalf("de test-hook blokkeert niets; opzet is fout:\n%s", out)
	}
	runGitApply(t, projectDir, "reset", "-q", "HEAD", "--", "controle.txt")
	if err := os.Remove(filepath.Join(projectDir, "controle.txt")); err != nil {
		t.Fatal(err)
	}

	refDir := lokaleMapMet(t, nil)
	refPluginsDir := filepath.Join(refDir, "public", "wp-content", "plugins")
	lokalePluginMap(t, refPluginsDir, "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps), pulls: &fakePluginPulls{}}

	res, err := svc.ApplyLocalPlugins(projectID, []string{"acf-pro"})
	if err != nil {
		t.Fatalf("ApplyLocalPlugins: %v", err)
	}
	if len(res.Plugins) != 1 || res.Plugins[0].Status != "updated" {
		t.Fatalf("een falende pre-commit-hook mag de update niet blokkeren: %+v", res.Plugins)
	}
	bericht := runGitApply(t, projectDir, "log", "-1", "--format=%s")
	if !strings.Contains(bericht, "acf-pro") {
		t.Errorf("commit ontbreekt; log = %q", bericht)
	}
}

// gewoneCommitPoging doet een commit mét hooks en geeft de uitkomst terug —
// gebruikt om te bewijzen dat een test-hook daadwerkelijk blokkeert.
func gewoneCommitPoging(dir string) (string, error) {
	cmd := exec.Command("git", "commit", "-m", "controle")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestReferenceUpdateBranchNaam dekt de branchnaam-opbouw, inclusief de rommel
// die echte betaalde plugins in hun Version-header zetten: die mag geen
// ongeldige git-ref opleveren.
func TestReferenceUpdateBranchNaam(t *testing.T) {
	vandaag := time.Now().Format("2006-01-02")
	tests := []struct {
		naam    string
		plugins []LocalPaidPlugin
		wil     string
	}{
		{
			naam:    "een plugin: naam en versie in de branch",
			plugins: []LocalPaidPlugin{{Slug: "gravityforms", Version: "2.9.1"}},
			wil:     "chore/plugin-gravityforms-2.9.1",
		},
		{
			naam:    "versie met spaties en haakjes wordt opgeschoond",
			plugins: []LocalPaidPlugin{{Slug: "acf-pro", Version: "6.0 (RC1)"}},
			wil:     "chore/plugin-acf-pro-6.0-rc1",
		},
		{
			naam:    "hoofdletters gaan naar kleine letters",
			plugins: []LocalPaidPlugin{{Slug: "WP-Rocket", Version: "3.15"}},
			wil:     "chore/plugin-wp-rocket-3.15",
		},
		{
			naam:    "dubbele punt is een verboden ref en verdwijnt",
			plugins: []LocalPaidPlugin{{Slug: "x", Version: "1..2"}},
			wil:     "chore/plugin-x-1.2",
		},
		{
			naam:    "zonder versie valt het terug op de datum",
			plugins: []LocalPaidPlugin{{Slug: "facetwp", Version: ""}},
			wil:     "chore/plugin-facetwp-" + vandaag,
		},
		{
			naam: "meerdere plugins: datum, want alle slugs erin is onleesbaar",
			plugins: []LocalPaidPlugin{
				{Slug: "acf-pro", Version: "6.4.1"},
				{Slug: "gravityforms", Version: "2.9.1"},
			},
			wil: "chore/plugin-updates-" + vandaag,
		},
		{
			naam:    "geen plugins: datum",
			plugins: nil,
			wil:     "chore/plugin-updates-" + vandaag,
		},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			if got := referenceUpdateBranch(tt.plugins); got != tt.wil {
				t.Errorf("referenceUpdateBranch = %q, wil %q", got, tt.wil)
			}
		})
	}
}

// TestVeiligBranchDeelWeigertGeenGeldigeRef controleert dat wat de sanitizer
// oplevert, door git zelf als branchnaam wordt geaccepteerd.
func TestVeiligBranchDeelWeigertGeenGeldigeRef(t *testing.T) {
	for _, rommel := range []string{"6.0 (RC1)", "1..2", "~^:?*[", "beta/2", "  1.0  ", "v1.0.lock"} {
		naam := "chore/plugin-x-" + veiligBranchDeel(rommel)
		cmd := exec.Command("git", "check-ref-format", "--branch", naam)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("git keurt %q af (uit %q): %v\n%s", naam, rommel, err, out)
		}
	}
}

// TestPRResultaatBevatMergeGegevens: de UI heeft het PR-nummer en een
// rechten-oordeel nodig om een merge-knop te kunnen tonen.
func TestPRResultaatBevatMergeGegevens(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	metPushDoelwit(t, dirs["siteA"])
	refDir := lokaleMapMet(t, nil)
	lokalePluginMap(t, filepath.Join(refDir, "public", "wp-content", "plugins"), "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps), pulls: &fakePluginPulls{}}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	r := res.Results[0]
	if r.PullRequestNumber != 7 {
		t.Errorf("PullRequestNumber = %d, wil 7", r.PullRequestNumber)
	}
	if !r.CanMerge {
		t.Error("met push-recht hoort CanMerge true te zijn")
	}
}

// TestPRResultaatZonderPushRechtGeenMergeKnop dekt de eis "als de git-gebruiker
// deze rechten heeft": een leesrecht-token krijgt geen merge-knop.
func TestPRResultaatZonderPushRechtGeenMergeKnop(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	metPushDoelwit(t, dirs["siteA"])
	refDir := lokaleMapMet(t, nil)
	lokalePluginMap(t, filepath.Join(refDir, "public", "wp-content", "plugins"), "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	nep := &fakePluginPulls{toegang: &github.RepoAccess{CanPush: false, AllowMergeCommit: true}}
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps), pulls: nep}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	r := res.Results[0]
	if r.CanMerge {
		t.Error("zonder push-recht hoort er geen merge-knop te komen")
	}
	// De PR zelf is er wel: alleen mergen kan niet.
	if r.PullRequestURL == "" || r.Status != "updated" {
		t.Errorf("resultaat = %+v", r)
	}
}

// TestPRResultaatRepoZonderMergeMethodes: een repo die alle merge-methodes heeft
// uitgezet levert ook geen knop op.
func TestPRResultaatRepoZonderMergeMethodes(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	metPushDoelwit(t, dirs["siteA"])
	refDir := lokaleMapMet(t, nil)
	lokalePluginMap(t, filepath.Join(refDir, "public", "wp-content", "plugins"), "acf-pro", "6.4.1")

	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	nep := &fakePluginPulls{toegang: &github.RepoAccess{CanPush: true}}
	svc := &PluginService{cfg: cfg, projects: ps, git: NewGitService(ps), pulls: nep}

	res, err := svc.ApplyPluginToProjects("acf-pro", []string{ids["siteA"]})
	if err != nil {
		t.Fatalf("ApplyPluginToProjects: %v", err)
	}
	if res.Results[0].CanMerge {
		t.Error("zonder toegestane merge-methode hoort er geen knop te komen")
	}
}

// TestMergePluginPullRequest merget via de bound method en controleert dat de
// methode wordt gekozen die de repo toestaat.
func TestMergePluginPullRequest(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	metPushDoelwit(t, dirs["siteA"])

	nep := &fakePluginPulls{toegang: &github.RepoAccess{CanPush: true, AllowSquashMerge: true}}
	svc := &PluginService{cfg: &config.Global{}, projects: ps, git: NewGitService(ps), pulls: nep}

	res, err := svc.MergePluginPullRequest(ids["siteA"], 7)
	if err != nil {
		t.Fatalf("MergePluginPullRequest: %v", err)
	}
	if !res.Merged || res.SHA != "deadbeef" {
		t.Errorf("resultaat = %+v", res)
	}
	if nep.gemergedNr != 7 {
		t.Errorf("gemerged nummer = %d, wil 7", nep.gemergedNr)
	}
	if nep.gemergdMethode != "squash" {
		t.Errorf("methode = %q, wil squash (het enige wat deze repo toestaat)", nep.gemergdMethode)
	}
}

func TestMergePluginPullRequestWeigertZonderRechten(t *testing.T) {
	ps, ids, dirs := setupTweeProjecten(t, "acf-pro", map[string]string{"siteA": "6.2.0"})
	metPushDoelwit(t, dirs["siteA"])

	nep := &fakePluginPulls{toegang: &github.RepoAccess{CanPush: false, AllowMergeCommit: true}}
	svc := &PluginService{cfg: &config.Global{}, projects: ps, git: NewGitService(ps), pulls: nep}

	_, err := svc.MergePluginPullRequest(ids["siteA"], 7)
	if err == nil {
		t.Fatal("zonder push-recht hoort mergen te weigeren")
	}
	if !strings.Contains(err.Error(), "geen push-recht") {
		t.Errorf("fout = %q", err)
	}
	if nep.gemergedNr != 0 {
		t.Error("er is toch gemerged")
	}
}

func TestMergePluginPullRequestOngeldigNummer(t *testing.T) {
	svc := &PluginService{cfg: &config.Global{}}
	if _, err := svc.MergePluginPullRequest("p1", 0); err == nil {
		t.Error("nummer 0 had geweigerd moeten worden")
	}
}
