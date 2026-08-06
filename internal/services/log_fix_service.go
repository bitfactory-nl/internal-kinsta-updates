package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/claudecode"
	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// fixTotaalTimeout begrenst de hele run. De CLI in gebruik heeft geen
// --max-turns, dus dit is het enige dat een uitlopende agent stopt.
const fixTotaalTimeout = 20 * time.Minute

// fixAITimeout is het deel daarvan dat de agent zelf mag gebruiken; de rest is
// voor git, lint en de PR.
const fixAITimeout = 15 * time.Minute

// aiRunner voert de agent uit (test seam).
type aiRunner interface {
	Run(ctx context.Context, prompt string, o claudecode.Opties) (claudecode.Resultaat, error)
}

// echteAIRunner is de productie-implementatie.
type echteAIRunner struct{}

func (echteAIRunner) Run(ctx context.Context, prompt string, o claudecode.Opties) (claudecode.Resultaat, error) {
	return claudecode.Run(ctx, prompt, o)
}

// fixGit is het git-oppervlak dat deze service nodig heeft (test seam).
type fixGit interface {
	DefaultBranchName(ctx context.Context, repoDir string) (string, error)
	RemoteURL(ctx context.Context, repoDir string) (string, error)
	Fetch(ctx context.Context, repoDir string) error
	PrepareWorktree(ctx context.Context, repoDir, worktreePath string) error
	AddWorktree(ctx context.Context, repoDir, worktreePath, branch, fromRef string) error
	RemoveWorktree(ctx context.Context, repoDir, worktreePath string) error
	StatusPorcelain(ctx context.Context, worktreePath string) (string, error)
	DiffStat(ctx context.Context, worktreePath string) (string, error)
	StageAllIn(ctx context.Context, worktreePath string) error
	CommitIn(ctx context.Context, worktreePath, message string) error
	HeadHash(ctx context.Context, worktreePath string) (string, error)
	PushBranch(ctx context.Context, worktreePath, branch string) error
}

// fixPulls is het GitHub-PR-oppervlak (test seam).
type fixPulls interface {
	FindOpenPull(ctx context.Context, owner, repo, head string) (*github.PullRequest, error)
	CreateDraftPull(ctx context.Context, owner, repo, head, base, title, body string) (*github.PullRequest, error)
}

// LogFixService laat een AI-agent één logfout oplossen en zet het resultaat als
// draft pull request op GitHub.
//
// De keten is: verse worktree vanaf origin/<default> → agent → vangrails →
// commit → push → draft PR. De vangrails zitten er bewust vóór de commit: een
// wijziging die de controle niet haalt, komt niet in de historie en niet op
// GitHub terecht. Een geblokkeerde run laat de worktree juist wél staan, zodat
// te zien is wat de agent van plan was.
type LogFixService struct {
	projects *ProjectService
	logs     *LogService
	git      fixGit
	pulls    fixPulls
	ai       aiRunner
	cfg      *config.Global
	emitter  eventEmitter
	now      func() time.Time
	tmpBase  string

	bezigMu sync.Mutex
	bezig   map[string]bool
}

func NewLogFixService(projects *ProjectService, logs *LogService, cfg *config.Global) *LogFixService {
	return &LogFixService{
		projects: projects,
		logs:     logs,
		git:      gitFixOps{},
		ai:       echteAIRunner{},
		cfg:      cfg,
		now:      time.Now,
		bezig:    map[string]bool{},
	}
}

// SetApp injects the Wails app reference (called after app creation).
func (s *LogFixService) SetApp(app *application.App) {
	s.emitter = app.Event
}

func (s *LogFixService) claim(slot string) bool {
	s.bezigMu.Lock()
	defer s.bezigMu.Unlock()
	if s.bezig[slot] {
		return false
	}
	s.bezig[slot] = true
	return true
}

func (s *LogFixService) release(slot string) {
	s.bezigMu.Lock()
	delete(s.bezig, slot)
	s.bezigMu.Unlock()
}

func (s *LogFixService) meld(projectID, groupID string, fase domain.AIFixPhase, detail string) {
	if s.emitter == nil {
		return
	}
	s.emitter.Emit("logs:"+projectID+":fix", domain.AIFixProgress{
		GroupID: groupID, Phase: fase, Detail: detail,
	})
}

// Fix laat de AI de melding groupID oplossen. De hele keten loopt door tot en
// met een draft pull request; blokkeert een vangrail, dan stopt het daar.
func (s *LogFixService) Fix(projectID, groupID string) (domain.AIFixResult, error) {
	groep, _, err := s.logs.GroupByID(projectID, groupID)
	if err != nil {
		return domain.AIFixResult{}, err
	}
	// De poort wordt hier opnieuw getoetst en niet vertrouwd op wat de frontend
	// meestuurt: een losse aanroep mag geen AI op botruis afsturen.
	if !groep.AIEligible {
		return domain.AIFixResult{}, fmt.Errorf("deze melding komt niet in aanmerking voor een AI-fix: %s", groep.AIReason)
	}
	p, ok := s.projects.Get(projectID)
	if !ok {
		return domain.AIFixResult{}, fmt.Errorf("project %q niet gevonden", projectID)
	}

	slot := projectID + "|" + groupID
	if !s.claim(slot) {
		return domain.AIFixResult{}, fmt.Errorf("er loopt al een AI-fix voor deze melding")
	}
	defer s.release(slot)

	res := domain.AIFixResult{
		GroupID:   groupID,
		Branch:    fixBranchNaam(groep),
		StartedAt: s.now(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), fixTotaalTimeout)
	defer cancel()

	uit, err := s.voerFixUit(ctx, p, groep, res)
	uit.FinishedAt = s.now()
	return uit, err
}

func (s *LogFixService) voerFixUit(ctx context.Context, p domain.Project, groep domain.LogGroup, res domain.AIFixResult) (domain.AIFixResult, error) {
	projectID := p.ID

	// 1. Waar landt de PR? Nooit op de default branch committen: we werken altijd
	// op een eigen branch die daarvan af komt.
	def, err := s.git.DefaultBranchName(ctx, p.Path)
	if err != nil {
		return res, fmt.Errorf("default branch bepalen: %w", err)
	}
	res.BaseRef = def
	if res.Branch == def {
		return res, fmt.Errorf("de fix-branch zou gelijk zijn aan de default branch %q; dat weiger ik", def)
	}

	owner, repo, err := s.repoVanRemote(ctx, p.Path)
	if err != nil {
		return res, err
	}
	pulls, err := s.pullClient()
	if err != nil {
		return res, err
	}

	// 2. Staat er al een PR voor deze melding? Dan is nog een run zinloos.
	if open, err := pulls.FindOpenPull(ctx, owner, repo, res.Branch); err != nil {
		return res, fmt.Errorf("bestaande pull request zoeken: %w", err)
	} else if open != nil {
		res.PullRequestURL = open.HTMLURL
		res.Warnings = append(res.Warnings, "er stond al een open pull request voor deze melding; er is geen nieuwe run gestart")
		s.meld(projectID, groep.ID, domain.FixPhaseDone, "bestaande pull request gevonden")
		return res, nil
	}

	// 3. Verse worktree, zodat de checkout van de gebruiker onaangeroerd blijft.
	s.meld(projectID, groep.ID, domain.FixPhaseBranch, "worktree aanmaken op "+res.Branch)
	if err := s.git.Fetch(ctx, p.Path); err != nil {
		return res, fmt.Errorf("fetch: %w", err)
	}
	worktree := filepath.Join(s.worktreeBase(), fmt.Sprintf("%s-fix-%s", p.ID, groep.ID))
	if err := s.git.PrepareWorktree(ctx, p.Path, worktree); err != nil {
		return res, fmt.Errorf("oude worktree opruimen: %w", err)
	}
	if err := s.git.AddWorktree(ctx, p.Path, worktree, res.Branch, "origin/"+def); err != nil {
		return res, fmt.Errorf("worktree aanmaken: %w", err)
	}
	// Alleen bij een geslaagde run ruimen we op. Blokkeert een vangrail, dan
	// blijft de worktree staan om te kunnen kijken wat de agent deed; de
	// volgende poging ruimt hem op via PrepareWorktree.
	gelukt := false
	defer func() {
		if !gelukt {
			return
		}
		_ = s.git.RemoveWorktree(ctx, p.Path, worktree)
		_ = os.RemoveAll(worktree)
	}()

	// 4. De agent aan het werk.
	prompt, gemaskeerd := bouwFixPrompt(groep)
	if len(gemaskeerd) > 0 {
		res.Warnings = append(res.Warnings,
			"in de logregels die naar de AI gingen is gemaskeerd: "+strings.Join(gemaskeerd, ", "))
	}
	s.meld(projectID, groep.ID, domain.FixPhaseAI, "de AI zoekt de oorzaak")

	aiCtx, aiCancel := context.WithTimeout(ctx, fixAITimeout)
	defer aiCancel()
	aiRes, err := s.ai.Run(aiCtx, prompt, claudecode.Opties{
		Dir:    worktree,
		Model:  "sonnet",
		APIKey: s.aiAPIKey(),
		// Zonder dit vindt de agent in de geïnstalleerde app geen php, composer
		// of npm om zijn eigen wijziging te controleren.
		PATH: GereedschapPATH(),
		OnProgress: func(regel string) {
			s.meld(projectID, groep.ID, domain.FixPhaseAI, regel)
		},
	})
	res.AISummary = aiRes.Samenvatting
	if err != nil {
		return s.geblokkeerd(projectID, res, worktree, fmt.Sprintf("de AI-run is mislukt: %v", err)), nil
	}

	// 5. Vangrails.
	s.meld(projectID, groep.ID, domain.FixPhaseGuard, "controleren wat er gewijzigd is")
	porcelain, err := s.git.StatusPorcelain(ctx, worktree)
	if err != nil {
		return res, fmt.Errorf("wijzigingen opvragen: %w", err)
	}
	res.ChangedFiles = gewijzigdeBestanden(porcelain)
	if err := controleerGewijzigdePaden(res.ChangedFiles); err != nil {
		return s.geblokkeerd(projectID, res, worktree, err.Error()), nil
	}

	if php := phpBestanden(res.ChangedFiles); len(php) > 0 {
		s.meld(projectID, groep.ID, domain.FixPhaseLint, fmt.Sprintf("php -l op %d bestand(en)", len(php)))
		uitvoer, lintErr := lintPHP(ctx, worktree, php)
		res.LintOutput = uitvoer
		if lintErr != nil {
			return s.geblokkeerd(projectID, res, worktree, fmt.Sprintf("de syntaxcontrole faalt, dus dit gaat niet naar GitHub: %v", lintErr)), nil
		}
		if PhpBin() == "" {
			res.Warnings = append(res.Warnings, "php ontbreekt op deze machine: de syntaxcontrole is niet gedraaid")
		}
	}

	// 6. Committen.
	s.meld(projectID, groep.ID, domain.FixPhaseCommit, "committen")
	if err := s.git.StageAllIn(ctx, worktree); err != nil {
		return res, fmt.Errorf("wijzigingen stagen: %w", err)
	}
	titel := commitTitel(groep)
	if err := s.git.CommitIn(ctx, worktree, commitBericht(groep, titel, res)); err != nil {
		return res, fmt.Errorf("committen: %w", err)
	}
	res.Committed = true
	if hash, err := s.git.HeadHash(ctx, worktree); err == nil {
		res.CommitHash = hash
	}
	if stat, err := s.git.DiffStat(ctx, worktree); err == nil {
		res.DiffStat = stat
	}

	// 7. Pushen en de draft-PR openen.
	s.meld(projectID, groep.ID, domain.FixPhasePush, "pushen naar origin/"+res.Branch)
	if err := s.git.PushBranch(ctx, worktree, res.Branch); err != nil {
		return res, fmt.Errorf("pushen: %w", err)
	}
	res.Pushed = true

	s.meld(projectID, groep.ID, domain.FixPhasePR, "draft pull request openen")
	pr, err := pulls.CreateDraftPull(ctx, owner, repo, res.Branch, def, titel, prBody(groep, res))
	if err != nil {
		return res, fmt.Errorf("pull request aanmaken: %w", err)
	}
	if pr != nil {
		res.PullRequestURL = pr.HTMLURL
	}
	gelukt = true
	s.meld(projectID, groep.ID, domain.FixPhaseDone, "klaar: "+res.PullRequestURL)
	return res, nil
}

// geblokkeerd markeert een run als gestopt door een vangrail. Er is dan niets
// gecommit en niets gepusht; de worktree blijft staan om te inspecteren.
func (s *LogFixService) geblokkeerd(projectID string, res domain.AIFixResult, worktree, reden string) domain.AIFixResult {
	res.Blocked = true
	res.BlockReason = reden
	res.Committed = false
	res.Pushed = false
	res.Warnings = append(res.Warnings,
		"er is niets gecommit of gepusht. De wijzigingen staan nog in "+worktree)
	s.meld(projectID, res.GroupID, domain.FixPhaseBlocked, reden)
	return res
}

func (s *LogFixService) worktreeBase() string {
	if s.tmpBase != "" {
		return s.tmpBase
	}
	return filepath.Join(os.TempDir(), "rdm-log-fix")
}

func (s *LogFixService) repoVanRemote(ctx context.Context, repoDir string) (string, string, error) {
	remote, err := s.git.RemoteURL(ctx, repoDir)
	if err != nil {
		return "", "", fmt.Errorf("origin-URL lezen: %w", err)
	}
	owner, repo, err := github.ParseRepoFromRemote(remote)
	if err != nil {
		return "", "", fmt.Errorf("origin-URL %q: %w", remote, err)
	}
	return owner, repo, nil
}

func (s *LogFixService) pullClient() (fixPulls, error) {
	if s.pulls != nil {
		return s.pulls, nil
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("configuratie niet beschikbaar")
	}
	token, err := config.ResolveSecret(s.cfg.PluginRepo.GithubToken)
	if err != nil {
		return nil, fmt.Errorf("github token: %w", err)
	}
	if token == "" {
		return nil, fmt.Errorf("github token niet geconfigureerd (zie Instellingen)")
	}
	return github.NewPullClient(token), nil
}

// aiAPIKey is de terugval voor het geval de Claude CLI niet is ingelogd.
func (s *LogFixService) aiAPIKey() string {
	if s.cfg == nil {
		return ""
	}
	key, err := config.ResolveSecret(s.cfg.AI.APIKey)
	if err != nil {
		return ""
	}
	return key
}

func commitTitel(g domain.LogGroup) string {
	kort := g.Title
	if len(kort) > 60 {
		kort = kap(kort, 60)
	}
	kort = strings.ReplaceAll(kort, "\n", " ")
	return "fix(logs): " + kort
}

func commitBericht(g domain.LogGroup, titel string, res domain.AIFixResult) string {
	var b strings.Builder
	b.WriteString(titel)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Productiefout uit het Kinsta error-log, %d keer voorgekomen.\n", g.Count)
	if g.RepoPath != "" {
		fmt.Fprintf(&b, "Bestand: %s", g.RepoPath)
		if g.Line > 0 {
			fmt.Fprintf(&b, ":%d", g.Line)
		}
		b.WriteString("\n")
	}
	if res.AISummary != "" {
		b.WriteString("\n")
		b.WriteString(res.AISummary)
		b.WriteString("\n")
	}
	b.WriteString("\nGemaakt door een AI-agent via de RDM Sites Tool; nog niet door een mens beoordeeld.\n")
	return b.String()
}

func prBody(g domain.LogGroup, res domain.AIFixResult) string {
	var b strings.Builder
	b.WriteString("> Deze pull request is door een AI-agent gemaakt en staat daarom als **draft**. Beoordeel de wijziging voordat je hem merget.\n\n")
	b.WriteString("## De fout in productie\n\n")
	fmt.Fprintf(&b, "- **Soort:** %s\n", soortLabel(g.Kind))
	fmt.Fprintf(&b, "- **Melding:** `%s`\n", scrubVoorAI(g.Title).Tekst)
	if g.RepoPath != "" {
		fmt.Fprintf(&b, "- **Bestand:** `%s`", g.RepoPath)
		if g.Line > 0 {
			fmt.Fprintf(&b, " (regel %d)", g.Line)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "- **Aantal keer:** %d\n", g.Count)
	if !g.First.IsZero() {
		fmt.Fprintf(&b, "- **Periode:** %s – %s (UTC)\n",
			g.First.Format("2006-01-02 15:04"), g.Last.Format("2006-01-02 15:04"))
	}

	if res.AISummary != "" {
		b.WriteString("\n## Wat de agent zegt\n\n")
		b.WriteString(res.AISummary)
		b.WriteString("\n")
	}
	if len(res.ChangedFiles) > 0 {
		b.WriteString("\n## Gewijzigde bestanden\n\n")
		for _, f := range res.ChangedFiles {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
	}
	if res.LintOutput != "" {
		b.WriteString("\n## Syntaxcontrole\n\n```\n")
		b.WriteString(strings.TrimSpace(res.LintOutput))
		b.WriteString("\n```\n")
	}
	b.WriteString("\n## Wat nog niet gecontroleerd is\n\n")
	// Dit blok moet kloppen met wat er werkelijk gedraaid heeft. "php -l is
	// gedraaid" schrijven terwijl php ontbrak, zou de reviewer een zekerheid
	// geven die er niet is.
	if res.LintOutput == "" {
		b.WriteString("- Er is **geen** syntaxcontrole gedraaid (geen PHP-bestanden gewijzigd, of php ontbrak op de machine).\n")
	} else if strings.Contains(res.LintOutput, "php niet gevonden") {
		b.WriteString("- Er is **geen** syntaxcontrole gedraaid: php was niet beschikbaar op de machine die deze fix maakte.\n")
	} else {
		b.WriteString("- Er is alleen `php -l` gedraaid: syntaxcontrole, geen functionele test.\n")
	}
	b.WriteString("- Of de fout hiermee echt weg is, blijkt pas na deploy uit het error-log.\n")
	b.WriteString("- Persoonsgegevens in de logregels zijn gemaskeerd voordat ze naar de AI gingen; de agent had dus niet de volledige logtekst.\n")
	b.WriteString("- De logtekst is deels door bezoekers en bots bepaald. Die is als onvertrouwde data aan de agent gegeven, maar let bij het reviewen op wijzigingen die niets met de fout te maken hebben.\n")
	return b.String()
}

// gitFixOps is de productie-implementatie van fixGit, bovenop gitcli.
type gitFixOps struct{}

func (gitFixOps) DefaultBranchName(ctx context.Context, repoDir string) (string, error) {
	return gitcli.DefaultBranch(ctx, repoDir)
}

func (gitFixOps) RemoteURL(ctx context.Context, repoDir string) (string, error) {
	return gitcli.Run(ctx, repoDir, "remote", "get-url", "origin")
}

func (gitFixOps) Fetch(ctx context.Context, repoDir string) error {
	return gitcli.Fetch(ctx, repoDir)
}

func (gitFixOps) PrepareWorktree(ctx context.Context, repoDir, worktreePath string) error {
	if _, err := os.Stat(worktreePath); err == nil {
		_ = gitcli.WorktreeRemove(ctx, repoDir, worktreePath)
		_ = os.RemoveAll(worktreePath)
	}
	return gitcli.WorktreePrune(ctx, repoDir)
}

func (gitFixOps) AddWorktree(ctx context.Context, repoDir, worktreePath, branch, fromRef string) error {
	return gitcli.WorktreeAdd(ctx, repoDir, worktreePath, branch, fromRef)
}

func (gitFixOps) RemoveWorktree(ctx context.Context, repoDir, worktreePath string) error {
	return gitcli.WorktreeRemove(ctx, repoDir, worktreePath)
}

func (gitFixOps) StatusPorcelain(ctx context.Context, worktreePath string) (string, error) {
	return gitcli.Run(ctx, worktreePath, "status", "--porcelain")
}

func (gitFixOps) DiffStat(ctx context.Context, worktreePath string) (string, error) {
	return gitcli.Run(ctx, worktreePath, "show", "--stat", "--oneline", "HEAD")
}

func (gitFixOps) StageAllIn(ctx context.Context, worktreePath string) error {
	_, err := gitcli.Run(ctx, worktreePath, "add", "-A")
	return err
}

func (gitFixOps) CommitIn(ctx context.Context, worktreePath, message string) error {
	_, err := gitcli.Run(ctx, worktreePath, "commit", "-m", message)
	return err
}

func (gitFixOps) HeadHash(ctx context.Context, worktreePath string) (string, error) {
	return gitcli.Run(ctx, worktreePath, "rev-parse", "--short", "HEAD")
}

func (gitFixOps) PushBranch(ctx context.Context, worktreePath, branch string) error {
	_, err := gitcli.Run(ctx, worktreePath, "push", "--set-upstream", "origin", branch)
	return err
}
