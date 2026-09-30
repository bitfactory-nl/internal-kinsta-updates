package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
)

// DepotCorePR maakt na een live core-update op de SatisPress-depotsite de
// bijbehorende Composer-PR, en weet waar de plugin-workflow draait.
type DepotCorePR interface {
	OpenCorePR(ctx context.Context, projectPath, version string) (string, error)
	PluginWorkflowURL(ctx context.Context, projectPath string) (string, error)
}

type composerRunner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

type depotGit interface {
	coreGit
	StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error
}

func (gitCoreOps) StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error {
	_, err := gitcli.Run(ctx, worktreePath, append([]string{"add", "--"}, paths...)...)
	return err
}

type depotCorePRMaker struct {
	git      depotGit
	pulls    func() (corePulls, error)
	composer composerRunner
	tmpBase  string
}

// NewDepotCorePR bouwt de echte implementatie; het GitHub-token wordt pas bij
// gebruik opgehaald (zelfde token als de plugin-repo, zoals WPCoreUpdateService).
func NewDepotCorePR(cfg *config.Global) DepotCorePR {
	return &depotCorePRMaker{
		git: gitCoreOps{},
		pulls: func() (corePulls, error) {
			token, err := config.ResolveSecret(cfg.PluginRepo.GithubToken)
			if err != nil || token == "" {
				return nil, errors.New("github token niet geconfigureerd (zie Instellingen)")
			}
			return github.NewPullClient(token), nil
		},
		composer: execComposer{},
		tmpBase:  filepath.Join(os.TempDir(), "rdm-depot-core"),
	}
}

func newDepotCorePRForTest(git depotGit, pulls corePulls, composer composerRunner, tmpBase string) *depotCorePRMaker {
	return &depotCorePRMaker{git: git, pulls: func() (corePulls, error) { return pulls, nil }, composer: composer, tmpBase: tmpBase}
}

func (m *depotCorePRMaker) ownerRepo(ctx context.Context, repoDir string) (string, string, error) {
	remote, err := m.git.RemoteURL(ctx, repoDir)
	if err != nil {
		return "", "", fmt.Errorf("origin-URL lezen: %w", err)
	}
	return github.ParseRepoFromRemote(remote)
}

func (m *depotCorePRMaker) PluginWorkflowURL(ctx context.Context, projectPath string) (string, error) {
	owner, repo, err := m.ownerRepo(ctx, projectPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/%s/%s/actions/workflows/plugin-update.yml", owner, repo), nil
}

func (m *depotCorePRMaker) OpenCorePR(ctx context.Context, projectPath, version string) (string, error) {
	if !reWPVersie.MatchString(version) {
		return "", fmt.Errorf("ongeldige WordPress-versie %q", version)
	}
	parts := strings.Split(version, ".")
	constraint := "^" + parts[0] + "." + parts[1]
	branch := "update/wordpress-" + version
	title := "WordPress " + version

	owner, repo, err := m.ownerRepo(ctx, projectPath)
	if err != nil {
		return "", err
	}
	pulls, err := m.pulls()
	if err != nil {
		return "", err
	}
	if open, err := pulls.FindOpenPull(ctx, owner, repo, branch); err == nil && open != nil {
		return open.HTMLURL, nil
	}
	if err := m.git.Fetch(ctx, projectPath); err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	def, err := m.git.DefaultBranchName(ctx, projectPath)
	if err != nil {
		return "", fmt.Errorf("default-branch bepalen: %w", err)
	}
	wt := filepath.Join(m.tmpBase, fmt.Sprintf("%s-wp-%s", filepath.Base(projectPath), version))
	if err := m.git.PrepareWorktree(ctx, projectPath, wt); err != nil {
		return "", fmt.Errorf("oude worktree opruimen: %w", err)
	}
	if err := m.git.AddWorktree(ctx, projectPath, wt, branch, "origin/"+def); err != nil {
		return "", fmt.Errorf("worktree aanmaken: %w", err)
	}
	defer func() {
		_ = m.git.RemoveWorktree(ctx, projectPath, wt)
		_ = os.RemoveAll(wt)
	}()
	if _, err := m.composer.Run(ctx, wt, "require", "roots/wordpress:"+constraint, "--no-update", "--no-scripts", "--no-plugins"); err != nil {
		return "", fmt.Errorf("composer require: %w", err)
	}
	if _, err := m.composer.Run(ctx, wt, "update", "roots/wordpress", "--with-dependencies", "--no-install", "--no-scripts", "--no-plugins"); err != nil {
		return "", fmt.Errorf("composer update: %w", err)
	}
	if err := m.git.StagePathsIn(ctx, wt, "composer.json", "composer.lock"); err != nil {
		return "", fmt.Errorf("stagen: %w", err)
	}
	if err := m.git.CommitIn(ctx, wt, "chore: WordPress naar "+version); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	if err := m.git.PushBranch(ctx, wt, branch); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}
	body := fmt.Sprintf("De live depotsite is via de RDM Sites Tool al bijgewerkt naar WordPress %s.\n\n"+
		"Deze PR zet `roots/wordpress` in composer.json/composer.lock gelijk, zodat de volgende deploy dezelfde versie installeert.", version)
	pr, err := pulls.CreatePull(ctx, owner, repo, branch, def, title, body)
	if err != nil {
		return "", fmt.Errorf("PR aanmaken: %w", err)
	}
	return pr.HTMLURL, nil
}

// execComposer draait de echte composer-binary.
type execComposer struct{}

func (execComposer) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, ComposerBin(), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSER_NO_INTERACTION=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		regels := strings.Split(strings.TrimSpace(string(out)), "\n")
		return string(out), fmt.Errorf("%w: %s", err, regels[len(regels)-1])
	}
	return string(out), nil
}

var (
	composerEenmalig sync.Once
	composerGevonden string
)

// ComposerBin zoekt de composer-binary; zelfde reden als docker.DockerBin: een
// vanuit Finder gestarte app erft de shell-PATH niet.
func ComposerBin() string {
	composerEenmalig.Do(func() { composerGevonden = zoekComposer() })
	return composerGevonden
}

func resetComposerBin() { composerEenmalig = sync.Once{} }

func zoekComposer() string {
	if p := strings.TrimSpace(os.Getenv("RDM_COMPOSER")); p != "" {
		return p
	}
	if p, err := exec.LookPath("composer"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{"/opt/homebrew/bin/composer", "/usr/local/bin/composer",
		filepath.Join(home, "Library", "Application Support", "Herd", "bin", "composer")} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, shell, "-ilc", "command -v composer").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			return p
		}
	}
	return "composer"
}
