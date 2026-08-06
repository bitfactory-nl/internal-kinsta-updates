package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/claudecode"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/domain"
)

type fakeFixGit struct {
	mu            sync.Mutex
	def           string
	remote        string
	porcelain     string
	stappen       []string
	worktreePad   string
	worktreeVanaf string
	worktreeTak   string
	verwijderd    bool
	commitBericht string
	pushTak       string
	fout          map[string]error
}

func (f *fakeFixGit) log(stap string) {
	f.mu.Lock()
	f.stappen = append(f.stappen, stap)
	f.mu.Unlock()
}

func (f *fakeFixGit) err(stap string) error {
	if f.fout == nil {
		return nil
	}
	return f.fout[stap]
}

func (f *fakeFixGit) DefaultBranchName(context.Context, string) (string, error) {
	f.log("default")
	return f.def, f.err("default")
}
func (f *fakeFixGit) RemoteURL(context.Context, string) (string, error) {
	return f.remote, f.err("remote")
}
func (f *fakeFixGit) Fetch(context.Context, string) error { f.log("fetch"); return f.err("fetch") }
func (f *fakeFixGit) PrepareWorktree(_ context.Context, _, pad string) error {
	f.log("prepare")
	return f.err("prepare")
}
func (f *fakeFixGit) AddWorktree(_ context.Context, _, pad, branch, vanaf string) error {
	f.log("addworktree")
	f.worktreePad, f.worktreeTak, f.worktreeVanaf = pad, branch, vanaf
	if err := f.err("addworktree"); err != nil {
		return err
	}
	return os.MkdirAll(pad, 0o755)
}
func (f *fakeFixGit) RemoveWorktree(_ context.Context, _, pad string) error {
	f.log("removeworktree")
	f.verwijderd = true
	return nil
}
func (f *fakeFixGit) StatusPorcelain(context.Context, string) (string, error) {
	f.log("status")
	return f.porcelain, f.err("status")
}
func (f *fakeFixGit) DiffStat(context.Context, string) (string, error) { return "1 file changed", nil }
func (f *fakeFixGit) StageAllIn(context.Context, string) error         { f.log("stage"); return f.err("stage") }
func (f *fakeFixGit) CommitIn(_ context.Context, _, bericht string) error {
	f.log("commit")
	f.commitBericht = bericht
	return f.err("commit")
}
func (f *fakeFixGit) HeadHash(context.Context, string) (string, error) { return "abc1234", nil }
func (f *fakeFixGit) PushBranch(_ context.Context, _, branch string) error {
	f.log("push")
	f.pushTak = branch
	return f.err("push")
}

type fakeFixPulls struct {
	open         *github.PullRequest
	zoekFout     error
	maakFout     error
	draftOpen    int
	gewoonOpen   int
	head, base   string
	titel, tekst string
}

func (f *fakeFixPulls) FindOpenPull(_ context.Context, _, _, head string) (*github.PullRequest, error) {
	return f.open, f.zoekFout
}

func (f *fakeFixPulls) CreateDraftPull(_ context.Context, _, _, head, base, titel, tekst string) (*github.PullRequest, error) {
	f.draftOpen++
	f.head, f.base, f.titel, f.tekst = head, base, titel, tekst
	if f.maakFout != nil {
		return nil, f.maakFout
	}
	return &github.PullRequest{Number: 42, HTMLURL: "https://github.com/acme/repo/pull/42"}, nil
}

type fakeAI struct {
	prompt  string
	dir     string
	res     claudecode.Resultaat
	fout    error
	aanroep int
	// schrijf wordt aangeroepen om te doen alsof de agent bestanden aanpast.
	schrijf func(dir string)
}

func (f *fakeAI) Run(_ context.Context, prompt string, o claudecode.Opties) (claudecode.Resultaat, error) {
	f.aanroep++
	f.prompt, f.dir = prompt, o.Dir
	if f.schrijf != nil {
		f.schrijf(o.Dir)
	}
	return f.res, f.fout
}

// fixOpstelling bouwt een LogFixService met een gevulde log-cache.
func fixOpstelling(t *testing.T, ruwLog string, repoBestanden []string) (*LogFixService, *fakeFixGit, *fakeFixPulls, *fakeAI, domain.LogGroup) {
	t.Helper()
	bron := &fakeLogSource{logs: map[string]string{"error": ruwLog}}
	logs, projectID, _ := logServiceMetProject(t, bron, repoBestanden...)
	res, err := logs.Fetch(projectID, "env", "error", 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	var kandidaat domain.LogGroup
	for _, g := range res.Groups {
		if g.AIEligible {
			kandidaat = g
			break
		}
	}

	git := &fakeFixGit{def: "release/1.0.x", remote: "git@github.com:acme/repo.git"}
	pulls := &fakeFixPulls{}
	ai := &fakeAI{res: claudecode.Resultaat{Samenvatting: "Null-check toegevoegd."}}

	svc := &LogFixService{
		projects: logs.projects,
		logs:     logs,
		git:      git,
		pulls:    pulls,
		ai:       ai,
		now:      logs.now,
		tmpBase:  t.TempDir(),
		bezig:    map[string]bool{},
	}
	return svc, git, pulls, ai, kandidaat
}

const themaFoutLog = `2026/08/03 09:14:02 [error] 99731#99731: *2801 FastCGI sent in stderr: "PHP message: PHP Warning:  Undefined array key "listing_price" in /www/voorbeeld_706/public/wp-content/themes/voorbeeld/inc/listing-card.php on line 88" while reading response header from upstream, client: 88.159.12.4, server: voorbeeld.nl, request: "GET /listings/ HTTP/2.0", host: "www.voorbeeld.nl:26426"`

const themaBestand = "public/wp-content/themes/voorbeeld/inc/listing-card.php"

// nepPHPLint laat php -l slagen of falen zonder echte php.
func nepPHPLint(t *testing.T, slaagt bool) {
	t.Helper()
	origineelExec, origineelLook := phpExecCommand, phpLookPath
	t.Cleanup(func() {
		phpExecCommand, phpLookPath = origineelExec, origineelLook
		phpEenmalig = sync.Once{}
		phpGevonden = ""
	})
	phpEenmalig = sync.Once{}
	phpGevonden = ""
	t.Setenv("RDM_PHP", "/nep/php")
	phpExecCommand = func(ctx context.Context, naam string, args ...string) *exec.Cmd {
		if slaagt {
			return exec.CommandContext(ctx, "printf", "No syntax errors detected")
		}
		return exec.CommandContext(ctx, "sh", "-c", "echo 'PHP Parse error: syntax error'; exit 255")
	}
}

func TestFixHappyPathLooptTotDraftPR(t *testing.T) {
	nepPHPLint(t, true)
	svc, git, pulls, ai, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	if groep.ID == "" {
		t.Fatal("geen kandidaat")
	}
	git.porcelain = " M " + themaBestand

	res, err := svc.Fix("p1", groep.ID)
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if res.Blocked {
		t.Fatalf("onterecht geblokkeerd: %s", res.BlockReason)
	}
	if !res.Committed || !res.Pushed {
		t.Errorf("committed=%v pushed=%v", res.Committed, res.Pushed)
	}
	if res.PullRequestURL != "https://github.com/acme/repo/pull/42" {
		t.Errorf("PR-url = %q", res.PullRequestURL)
	}
	if pulls.draftOpen != 1 {
		t.Errorf("draft-PR's aangemaakt: %d, wil 1", pulls.draftOpen)
	}
	if pulls.base != "release/1.0.x" {
		t.Errorf("PR-base = %q, wil de default branch", pulls.base)
	}
	if pulls.head != "fix/log-"+groep.ID {
		t.Errorf("PR-head = %q", pulls.head)
	}
	// De worktree komt van origin/<default> zodat de checkout onaangeroerd blijft.
	if git.worktreeVanaf != "origin/release/1.0.x" {
		t.Errorf("worktree vanaf %q", git.worktreeVanaf)
	}
	if ai.dir != git.worktreePad {
		t.Errorf("de AI draaide in %q, niet in de worktree %q", ai.dir, git.worktreePad)
	}
	// Bij succes wordt de worktree opgeruimd.
	if !git.verwijderd {
		t.Error("de worktree is bij succes niet opgeruimd")
	}
	if res.CommitHash != "abc1234" {
		t.Errorf("commithash = %q", res.CommitHash)
	}
	if !strings.Contains(git.commitBericht, "Null-check toegevoegd.") {
		t.Errorf("de samenvatting van de AI hoort in het commitbericht: %q", git.commitBericht)
	}
	if !strings.Contains(pulls.tekst, "draft") {
		t.Errorf("de PR-tekst hoort te vermelden dat dit AI-werk is: %q", pulls.tekst)
	}
}

// TestFixBlokkeertCoreWijziging is de belangrijkste vangrail-test: een AI die
// WordPress core aanpast, mag niet committen en niet pushen.
func TestFixBlokkeertCoreWijziging(t *testing.T) {
	nepPHPLint(t, true)
	svc, git, pulls, _, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	git.porcelain = " M " + themaBestand + "\n M public/wp-includes/class-wp-hook.php"

	res, err := svc.Fix("p1", groep.ID)
	if err != nil {
		t.Fatalf("Fix gaf een fout in plaats van een blokkade: %v", err)
	}
	if !res.Blocked {
		t.Fatal("een core-wijziging werd niet geblokkeerd")
	}
	if res.Committed || res.Pushed {
		t.Errorf("er is toch gecommit/gepusht: committed=%v pushed=%v", res.Committed, res.Pushed)
	}
	if pulls.draftOpen != 0 {
		t.Error("er is een PR aangemaakt terwijl de run geblokkeerd was")
	}
	for _, stap := range git.stappen {
		if stap == "commit" || stap == "push" {
			t.Errorf("git-stap %q had niet mogen gebeuren: %v", stap, git.stappen)
		}
	}
	if !strings.Contains(res.BlockReason, "core") {
		t.Errorf("reden legt het niet uit: %q", res.BlockReason)
	}
	// De worktree blijft staan zodat te zien is wat de agent deed.
	if git.verwijderd {
		t.Error("een geblokkeerde run hoort de worktree te laten staan")
	}
}

func TestFixBlokkeertMislukteLint(t *testing.T) {
	nepPHPLint(t, false)
	svc, git, pulls, _, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	git.porcelain = " M " + themaBestand

	res, err := svc.Fix("p1", groep.ID)
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if !res.Blocked {
		t.Fatal("een falende php -l werd niet geblokkeerd")
	}
	if res.Pushed || pulls.draftOpen != 0 {
		t.Error("er is gepusht terwijl de syntaxcontrole faalde")
	}
	if !strings.Contains(res.LintOutput, "Parse error") {
		t.Errorf("lintuitvoer = %q", res.LintOutput)
	}
}

func TestFixBlokkeertZonderWijzigingen(t *testing.T) {
	nepPHPLint(t, true)
	svc, git, pulls, _, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	git.porcelain = ""

	res, err := svc.Fix("p1", groep.ID)
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if !res.Blocked {
		t.Fatal("een run zonder wijzigingen hoort te blokkeren")
	}
	if pulls.draftOpen != 0 {
		t.Error("er is een PR aangemaakt zonder wijzigingen")
	}
}

func TestFixBlokkeertPadBuitenDeRepoEnDependencies(t *testing.T) {
	tests := []struct {
		naam      string
		porcelain string
		verwacht  string
	}{
		{"pad buiten de repo", " M ../elders/x.php", "buiten de checkout"},
		{"composer.json", " M composer.json", "afhankelijkheden"},
		{"package-lock.json", " M package-lock.json", "afhankelijkheden"},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			nepPHPLint(t, true)
			svc, git, pulls, _, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
			git.porcelain = tt.porcelain

			res, err := svc.Fix("p1", groep.ID)
			if err != nil {
				t.Fatalf("Fix: %v", err)
			}
			if !res.Blocked {
				t.Fatalf("%s werd niet geblokkeerd", tt.naam)
			}
			if !strings.Contains(res.BlockReason, tt.verwacht) {
				t.Errorf("reden = %q, wil iets over %q", res.BlockReason, tt.verwacht)
			}
			if pulls.draftOpen != 0 {
				t.Error("er is een PR aangemaakt")
			}
		})
	}
}

// TestFixWeigertNietKandidaat bewijst dat de poort ook geldt bij een directe
// aanroep: de frontend kan niet afdwingen dat er AI op botruis wordt gezet.
func TestFixWeigertNietKandidaat(t *testing.T) {
	botLog := `2026/08/04 10:08:29 [error] 5348#5348: *11792 directory index of "/www/voorbeeld_706/public/wp-includes/css/" is forbidden, client: 51.107.184.196, server: voorbeeld.nl, request: "GET /wp-includes/css/ HTTP/2.0", host: "voorbeeld.nl"`
	bron := &fakeLogSource{logs: map[string]string{"error": botLog}}
	logs, projectID, _ := logServiceMetProject(t, bron)
	res, err := logs.Fetch(projectID, "env", "error", 10)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].AIEligible {
		t.Fatalf("opstelling verkeerd: %+v", res.Groups)
	}

	ai := &fakeAI{}
	svc := &LogFixService{
		projects: logs.projects, logs: logs,
		git: &fakeFixGit{def: "main"}, pulls: &fakeFixPulls{}, ai: ai,
		now: logs.now, tmpBase: t.TempDir(), bezig: map[string]bool{},
	}
	if _, err := svc.Fix(projectID, res.Groups[0].ID); err == nil {
		t.Fatal("een niet-kandidaat werd geaccepteerd")
	}
	if ai.aanroep != 0 {
		t.Error("de AI is aangeroepen voor botruis")
	}
}

func TestFixGeeftBestaandePRTerugZonderAIRun(t *testing.T) {
	svc, _, pulls, ai, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	pulls.open = &github.PullRequest{Number: 7, HTMLURL: "https://github.com/acme/repo/pull/7"}

	res, err := svc.Fix("p1", groep.ID)
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if res.PullRequestURL != "https://github.com/acme/repo/pull/7" {
		t.Errorf("PR-url = %q", res.PullRequestURL)
	}
	if ai.aanroep != 0 {
		t.Error("de AI is gedraaid terwijl er al een open PR was")
	}
	if pulls.draftOpen != 0 {
		t.Error("er is een tweede PR aangemaakt")
	}
}

func TestFixWeigertBranchGelijkAanDefault(t *testing.T) {
	svc, git, _, ai, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	// Doe alsof de default branch precies de fix-branch is.
	git.def = "fix/log-" + groep.ID

	if _, err := svc.Fix("p1", groep.ID); err == nil {
		t.Fatal("committen op de default branch werd niet geweigerd")
	}
	if ai.aanroep != 0 {
		t.Error("de AI is gestart ondanks de weigering")
	}
}

func TestFixLaatGeenTweeRunsTegelijk(t *testing.T) {
	nepPHPLint(t, true)
	svc, git, _, _, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	git.porcelain = " M " + themaBestand

	// Bezet het slot alsof er al een run loopt.
	if !svc.claim("p1|" + groep.ID) {
		t.Fatal("claim mislukte")
	}
	if _, err := svc.Fix("p1", groep.ID); err == nil {
		t.Fatal("een tweede run werd toegestaan")
	}
	svc.release("p1|" + groep.ID)
}

// De prompt die naar de AI gaat is de enige plek waar logtekst de machine
// verlaat, dus die mag geen persoonsgegevens bevatten.
func TestFixStuurtGescrubdePromptNaarDeAI(t *testing.T) {
	nepPHPLint(t, true)
	ruw := `2026/08/05 12:44:51 [error] 42251#42251: *37774 FastCGI sent in stderr: "PHP message: PHP Warning:  mail() naar test.persoon@example.com faalde in /www/voorbeeld_706/public/wp-content/themes/voorbeeld/inc/listing-card.php on line 88" while reading response header from upstream, client: 80.56.116.54, server: voorbeeld.nl, request: "POST /form HTTP/2.0", host: "voorbeeld.nl"`
	svc, git, _, ai, groep := fixOpstelling(t, ruw, []string{themaBestand})
	if groep.ID == "" {
		t.Fatal("geen kandidaat")
	}
	git.porcelain = " M " + themaBestand

	res, err := svc.Fix("p1", groep.ID)
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if ai.aanroep != 1 {
		t.Fatalf("AI-aanroepen = %d", ai.aanroep)
	}
	if strings.Contains(ai.prompt, "test.persoon@example.com") {
		t.Errorf("de prompt bevat een e-mailadres:\n%s", ai.prompt)
	}
	if strings.Contains(ai.prompt, "80.56.116.54") {
		t.Errorf("de prompt bevat een ip-adres:\n%s", ai.prompt)
	}
	var gemeld bool
	for _, w := range res.Warnings {
		if strings.Contains(w, "gemaskeerd") {
			gemeld = true
		}
	}
	if !gemeld {
		t.Errorf("het resultaat meldt niet dat er gemaskeerd is: %v", res.Warnings)
	}
}

func TestFixMisluktteAIRunBlokkeert(t *testing.T) {
	svc, git, pulls, ai, groep := fixOpstelling(t, themaFoutLog, []string{themaBestand})
	ai.fout = fmt.Errorf("de Claude CLI is niet ingelogd")
	git.porcelain = " M " + themaBestand

	res, err := svc.Fix("p1", groep.ID)
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if !res.Blocked || !strings.Contains(res.BlockReason, "niet ingelogd") {
		t.Errorf("res = %+v", res)
	}
	if pulls.draftOpen != 0 {
		t.Error("er is een PR aangemaakt na een mislukte AI-run")
	}
}

func TestGewijzigdeBestandenLeestPorcelain(t *testing.T) {
	porcelain := " M public/a.php\n?? public/b.php\nA  public/c.php\nR  oud.php -> nieuw.php\n M \"pad met spaties.php\"\n"
	got := gewijzigdeBestanden(porcelain)
	wil := []string{"public/a.php", "public/b.php", "public/c.php", "nieuw.php", "pad met spaties.php"}
	if len(got) != len(wil) {
		t.Fatalf("got = %v, wil %v", got, wil)
	}
	for i := range wil {
		if got[i] != wil[i] {
			t.Errorf("[%d] = %q, wil %q", i, got[i], wil[i])
		}
	}
}

func TestRepoPadIsCore(t *testing.T) {
	core := []string{
		"public/wp-includes/x.php",
		"public/wp-admin/y.php",
		"public/wp-settings.php",
		"web/public/wp-includes/x.php",
		"web/wp-admin/y.php",
		"wp-includes/x.php",
	}
	for _, p := range core {
		if !repoPadIsCore(p) {
			t.Errorf("%q hoort core te zijn", p)
		}
	}
	eigen := []string{
		"public/wp-content/themes/t/f.php",
		"public/wp-content/plugins/p/p.php",
		"assets/js/app.js",
		"web/public/wp-content/themes/t/f.php",
	}
	for _, p := range eigen {
		if repoPadIsCore(p) {
			t.Errorf("%q hoort geen core te zijn", p)
		}
	}
}

func TestPhpBestanden(t *testing.T) {
	got := phpBestanden([]string{"a.php", "b.js", "c.PHP", "d.scss"})
	if len(got) != 2 || got[0] != "a.php" || got[1] != "c.PHP" {
		t.Errorf("got = %v", got)
	}
}

func TestControleerGewijzigdePadenLaatEigenCodeDoor(t *testing.T) {
	if err := controleerGewijzigdePaden([]string{
		"public/wp-content/themes/t/f.php",
		"public/wp-content/plugins/p/src/Widget.php",
	}); err != nil {
		t.Errorf("eigen code werd geblokkeerd: %v", err)
	}
}
