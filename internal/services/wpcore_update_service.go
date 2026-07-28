package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// CoreUpdateResult is de uitkomst van één core-update.
type CoreUpdateResult struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	// Status: pr_created | exists | skipped_no_release | error
	Status         string `json:"status"`
	From           string `json:"from"`
	To             string `json:"to"`
	Branch         string `json:"branch"`
	PullRequestURL string `json:"pullRequestUrl"`
	Error          string `json:"error"`
}

// coreGit is het git-oppervlak dat deze service nodig heeft (test seam). Alle
// mutaties gebeuren in een tijdelijke worktree, dus de methodes werken op
// paden en niet op project-ID's.
type coreGit interface {
	DefaultBranchName(ctx context.Context, repoDir string) (string, error)
	RemoteURL(ctx context.Context, repoDir string) (string, error)
	Fetch(ctx context.Context, repoDir string) error
	AddWorktree(ctx context.Context, repoDir, worktreePath, branch, fromRef string) error
	RemoveWorktree(ctx context.Context, repoDir, worktreePath string) error
	StageAllIn(ctx context.Context, worktreePath string) error
	CommitIn(ctx context.Context, worktreePath, message string) error
	PushBranch(ctx context.Context, worktreePath, branch string) error
}

// coreDownloader haalt de core-zip op (test seam).
type coreDownloader interface {
	Download(ctx context.Context, url string) ([]byte, error)
}

// corePulls is het GitHub-PR-oppervlak (test seam).
type corePulls interface {
	FindOpenPull(ctx context.Context, owner, repo, head string) (*github.PullRequest, error)
	CreatePull(ctx context.Context, owner, repo, head, base, title, body string) (*github.PullRequest, error)
}

// coreProjects is de subset van *ProjectService die hier nodig is (test seam).
type coreProjects interface {
	Get(id string) (domain.Project, bool)
}

// WPCoreUpdateService zet een WordPress core-update om in een branch met pull
// request op de klantrepo. Er wordt nooit naar de release-branch gepusht en
// nooit iets op een live-omgeving aangepast: het eindpunt is altijd een PR die
// een mens reviewt en merget.
type WPCoreUpdateService struct {
	projects coreProjects
	git      coreGit
	download coreDownloader
	pulls    corePulls
	cfg      *config.Global
	// tmpBase is de map waaronder tijdelijke worktrees komen (test seam).
	tmpBase string
}

// NewWPCoreUpdateService wires the service. De GitHub-client wordt lui
// opgebouwd zodra er een PR nodig is, zodat een ontbrekend token pas een fout
// geeft bij gebruik en niet bij het opstarten van de app.
func NewWPCoreUpdateService(projects *ProjectService, cfg *config.Global) *WPCoreUpdateService {
	return &WPCoreUpdateService{
		projects: projects,
		git:      gitCoreOps{},
		download: wporg.NewClient(),
		cfg:      cfg,
	}
}

// newWPCoreUpdateServiceForTest bouwt de service met fakes.
func newWPCoreUpdateServiceForTest(projects coreProjects, git coreGit, dl coreDownloader, pulls corePulls, tmpBase string) *WPCoreUpdateService {
	return &WPCoreUpdateService{projects: projects, git: git, download: dl, pulls: pulls, tmpBase: tmpBase}
}

// pullClient geeft de PR-client: de geïnjecteerde fake in tests, anders een
// verse client met het token uit de instellingen.
func (s *WPCoreUpdateService) pullClient() (corePulls, error) {
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

// UpdateProject werkt WordPress core in één project bij naar toVersion en
// opent daarvoor een pull request. De frontend roept dit per project aan, ook
// voor de bulk-actie, zodat elke rij zijn eigen status krijgt.
func (s *WPCoreUpdateService) UpdateProject(projectID, toVersion string) CoreUpdateResult {
	res := CoreUpdateResult{ProjectID: projectID, To: strings.TrimSpace(toVersion)}

	p, ok := s.projects.Get(projectID)
	if !ok {
		return s.fout(res, fmt.Errorf("project %q niet gevonden", projectID))
	}
	res.ProjectName = p.DisplayName
	if res.To == "" {
		return s.fout(res, fmt.Errorf("geen doelversie opgegeven"))
	}
	if p.Path == "" {
		return s.fout(res, fmt.Errorf("project heeft geen pad"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1. Default branch moet een release-branch zijn (zelfde regel als de
	// plugin-updates): alleen dan weten we waar de PR op moet landen.
	def, err := s.git.DefaultBranchName(ctx, p.Path)
	if err != nil {
		return s.fout(res, fmt.Errorf("default branch bepalen: %w", err))
	}
	if !strings.HasPrefix(def, "release/") {
		res.Status = "skipped_no_release"
		res.Error = fmt.Sprintf("default branch %q voldoet niet aan release/*", def)
		return res
	}

	owner, repo, err := s.repoVanRemote(ctx, p.Path)
	if err != nil {
		return s.fout(res, err)
	}
	pulls, err := s.pullClient()
	if err != nil {
		return s.fout(res, err)
	}

	branch := "update/wordpress-" + res.To
	res.Branch = branch

	// 2. Bestaat er al een open PR voor deze doelversie? Dan is er niets te
	// doen — dubbel klikken (of de bulk-actie na een eerdere run) is veilig.
	if open, err := pulls.FindOpenPull(ctx, owner, repo, branch); err == nil && open != nil {
		res.Status = "exists"
		res.PullRequestURL = open.HTMLURL
		return res
	} else if err != nil {
		return s.fout(res, fmt.Errorf("bestaande PR zoeken: %w", err))
	}

	// 3. Verse worktree vanaf origin/<release-branch>, zodat de checkout van de
	// gebruiker onaangeroerd blijft.
	if err := s.git.Fetch(ctx, p.Path); err != nil {
		return s.fout(res, fmt.Errorf("fetch: %w", err))
	}
	worktree := filepath.Join(s.worktreeBase(), fmt.Sprintf("%s-wp-%s", p.ID, res.To))
	_ = os.RemoveAll(worktree) // restant van een eerdere, afgebroken run
	if err := s.git.AddWorktree(ctx, p.Path, worktree, branch, "origin/"+def); err != nil {
		return s.fout(res, fmt.Errorf("worktree aanmaken: %w", err))
	}
	defer func() {
		_ = s.git.RemoveWorktree(ctx, p.Path, worktree)
		_ = os.RemoveAll(worktree)
	}()

	// 4. Core downloaden en vervangen.
	url := wporg.CoreDownloadURL(res.To)
	zipData, err := s.download.Download(ctx, url)
	if err != nil {
		return s.fout(res, fmt.Errorf("core downloaden: %w", err))
	}
	wpRoot := wpRootDir(worktree)
	res.From = leesWPVersie(wpRoot)
	if err := replaceCore(zipData, wpRoot); err != nil {
		return s.fout(res, fmt.Errorf("core vervangen: %w", err))
	}

	// 5. Committen en pushen.
	if err := s.git.StageAllIn(ctx, worktree); err != nil {
		return s.fout(res, fmt.Errorf("wijzigingen stagen: %w", err))
	}
	titel := fmt.Sprintf("fix(wordpress): update WordPress core %s→%s", weergaveVersie(res.From), res.To)
	if err := s.git.CommitIn(ctx, worktree, titel); err != nil {
		return s.fout(res, fmt.Errorf("committen: %w", err))
	}
	if err := s.git.PushBranch(ctx, worktree, branch); err != nil {
		return s.fout(res, fmt.Errorf("pushen: %w", err))
	}

	// 6. PR openen.
	body := fmt.Sprintf(
		"Automatische WordPress core-update van %s naar %s, aangemaakt met de RDM Sites Tool.\n\n"+
			"- Alleen core is vervangen: `wp-admin/`, `wp-includes/` en de core-bestanden in de webroot.\n"+
			"- `wp-config.php`, `wp-content/` (thema's, plugins, uploads) en project-eigen bestanden zijn ongewijzigd.\n"+
			"- Een eventuele database-upgrade voert WordPress zelf uit na deploy.\n",
		weergaveVersie(res.From), res.To)
	pr, err := pulls.CreatePull(ctx, owner, repo, branch, def, titel, body)
	if err != nil {
		return s.fout(res, fmt.Errorf("pull request aanmaken: %w", err))
	}
	res.Status = "pr_created"
	if pr != nil {
		res.PullRequestURL = pr.HTMLURL
	}
	return res
}

// worktreeBase is de map waaronder tijdelijke worktrees komen.
func (s *WPCoreUpdateService) worktreeBase() string {
	if s.tmpBase != "" {
		return s.tmpBase
	}
	return filepath.Join(os.TempDir(), "rdm-wp-core")
}

// repoVanRemote leest de origin-URL en splitst die in owner en repo.
func (s *WPCoreUpdateService) repoVanRemote(ctx context.Context, repoDir string) (string, string, error) {
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

func (s *WPCoreUpdateService) fout(res CoreUpdateResult, err error) CoreUpdateResult {
	res.Status = "error"
	res.Error = err.Error()
	return res
}

// leesWPVersie leest de huidige core-versie uit de webroot; "" als dat niet
// lukt (dan is alleen het van→naar-label in de commit minder specifiek).
func leesWPVersie(wpRoot string) string {
	data, err := os.ReadFile(filepath.Join(wpRoot, "wp-includes", "version.php"))
	if err != nil {
		return ""
	}
	return wpplugins.ParseWPVersion(data)
}

// weergaveVersie geeft een leesbaar label voor een mogelijk lege versie.
func weergaveVersie(v string) string {
	if v == "" {
		return "onbekend"
	}
	return v
}

// gitCoreOps is de productie-implementatie van coreGit, bovenop gitcli.
type gitCoreOps struct{}

func (gitCoreOps) DefaultBranchName(ctx context.Context, repoDir string) (string, error) {
	return gitcli.DefaultBranch(ctx, repoDir)
}

func (gitCoreOps) RemoteURL(ctx context.Context, repoDir string) (string, error) {
	return gitcli.Run(ctx, repoDir, "remote", "get-url", "origin")
}

func (gitCoreOps) Fetch(ctx context.Context, repoDir string) error {
	return gitcli.Fetch(ctx, repoDir)
}

func (gitCoreOps) AddWorktree(ctx context.Context, repoDir, worktreePath, branch, fromRef string) error {
	return gitcli.WorktreeAdd(ctx, repoDir, worktreePath, branch, fromRef)
}

func (gitCoreOps) RemoveWorktree(ctx context.Context, repoDir, worktreePath string) error {
	return gitcli.WorktreeRemove(ctx, repoDir, worktreePath)
}

func (gitCoreOps) StageAllIn(ctx context.Context, worktreePath string) error {
	_, err := gitcli.Run(ctx, worktreePath, "add", "-A")
	return err
}

func (gitCoreOps) CommitIn(ctx context.Context, worktreePath, message string) error {
	_, err := gitcli.Run(ctx, worktreePath, "commit", "-m", message)
	return err
}

func (gitCoreOps) PushBranch(ctx context.Context, worktreePath, branch string) error {
	_, err := gitcli.Run(ctx, worktreePath, "push", "--set-upstream", "origin", branch)
	return err
}
