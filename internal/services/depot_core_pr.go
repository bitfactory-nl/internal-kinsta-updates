package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	// LockedCoreVersion geeft de roots/wordpress-versie in composer.lock op
	// de default-branch van origin.
	LockedCoreVersion(ctx context.Context, projectPath string) (string, error)
}

type composerRunner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

type depotGit interface {
	coreGit
	StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error
	HasStagedChanges(ctx context.Context, worktreePath string) (bool, error)
	ForcePushBranch(ctx context.Context, worktreePath, branch string) error
	ShowFile(ctx context.Context, repoDir, ref, path string) (string, error)
}

// ShowFile geeft de inhoud van path op ref, zonder iets uit te checken.
func (gitCoreOps) ShowFile(ctx context.Context, repoDir, ref, path string) (string, error) {
	return gitcli.Run(ctx, repoDir, "show", ref+":"+path)
}

func (gitCoreOps) StagePathsIn(ctx context.Context, worktreePath string, paths ...string) error {
	_, err := gitcli.Run(ctx, worktreePath, append([]string{"add", "--"}, paths...)...)
	return err
}

// HasStagedChanges meldt of er iets gestaged is. Via --name-only in plaats
// van --quiet: gitcli.Run geeft de exitcode niet door, de uitvoer wel.
func (gitCoreOps) HasStagedChanges(ctx context.Context, worktreePath string) (bool, error) {
	out, err := gitcli.Run(ctx, worktreePath, "diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// ForcePushBranch pusht een branch die van de tool is (update/wordpress-X):
// een restant van een eerdere run, waarvan de PR dicht is, mag overschreven
// worden, maar alleen als niemand er intussen iets op heeft gezet.
func (gitCoreOps) ForcePushBranch(ctx context.Context, worktreePath, branch string) error {
	_, err := gitcli.Run(ctx, worktreePath, "push", "--force-with-lease", "-u", "origin", branch)
	return err
}

type depotCorePRMaker struct {
	git      depotGit
	pulls    func() (corePulls, error)
	composer composerRunner
	tmpBase  string

	mu       sync.Mutex
	inFlight map[string]bool
}

// claim reserveert een run voor project+versie; false als er al een loopt.
func (m *depotCorePRMaker) claim(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight == nil {
		m.inFlight = map[string]bool{}
	}
	if m.inFlight[key] {
		return false
	}
	m.inFlight[key] = true
	return true
}

func (m *depotCorePRMaker) release(key string) {
	m.mu.Lock()
	delete(m.inFlight, key)
	m.mu.Unlock()
}

// kortHash geeft de eerste 8 hex-tekens van de sha256 van s.
func kortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:8]
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

func (m *depotCorePRMaker) LockedCoreVersion(ctx context.Context, projectPath string) (string, error) {
	if err := m.git.Fetch(ctx, projectPath); err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	def, err := m.git.DefaultBranchName(ctx, projectPath)
	if err != nil {
		return "", fmt.Errorf("default-branch bepalen: %w", err)
	}
	data, err := m.git.ShowFile(ctx, projectPath, "origin/"+def, "composer.lock")
	if err != nil {
		return "", fmt.Errorf("composer.lock lezen: %w", err)
	}
	return wordpressVersieUitLock([]byte(data))
}

func (m *depotCorePRMaker) OpenCorePR(ctx context.Context, projectPath, version string) (string, error) {
	if !reWPVersie.MatchString(version) {
		return "", fmt.Errorf("ongeldige WordPress-versie %q", version)
	}
	slot := projectPath + "@" + version
	if !m.claim(slot) {
		return "", errors.New("er loopt al een PR-run voor deze versie")
	}
	defer m.release(slot)
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
	open, err := pulls.FindOpenPull(ctx, owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("bestaande PR zoeken: %w", err)
	}
	if open != nil {
		return open.HTMLURL, nil
	}
	if err := m.git.Fetch(ctx, projectPath); err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	def, err := m.git.DefaultBranchName(ctx, projectPath)
	if err != nil {
		return "", fmt.Errorf("default-branch bepalen: %w", err)
	}
	wt := filepath.Join(m.tmpBase, fmt.Sprintf("%s-%s-wp-%s", filepath.Base(projectPath), kortHash(projectPath), version))
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
	// Inline tijdelijke constraint (Composer >= 2.2): de lock moet exact de
	// live versie krijgen, niet de hoogste die ^X.Y toestaat.
	if _, err := m.composer.Run(ctx, wt, "update", "roots/wordpress:"+version, "--with-dependencies", "--no-install", "--no-scripts", "--no-plugins"); err != nil {
		return "", fmt.Errorf("composer update: %w", err)
	}
	if err := controleerLockVersie(wt, version); err != nil {
		return "", err
	}
	if err := m.git.StagePathsIn(ctx, wt, "composer.json", "composer.lock"); err != nil {
		return "", fmt.Errorf("stagen: %w", err)
	}
	gewijzigd, err := m.git.HasStagedChanges(ctx, wt)
	if err != nil {
		return "", fmt.Errorf("wijzigingen bepalen: %w", err)
	}
	if !gewijzigd {
		return "", fmt.Errorf("composer-bestanden staan al op %s", version)
	}
	if err := m.git.CommitIn(ctx, wt, "chore: WordPress naar "+version); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	if err := m.git.ForcePushBranch(ctx, wt, branch); err != nil {
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

// controleerLockVersie eist dat composer.lock in dir roots/wordpress exact
// op version heeft staan. roots taggt X.Y.0 als X.Y en version.php zegt dan
// ook X.Y, dus we vergelijken letterlijk na het weghalen van een leidende v.
func controleerLockVersie(dir, version string) error {
	data, err := os.ReadFile(filepath.Join(dir, "composer.lock"))
	if err != nil {
		return errors.New("composer.lock niet leesbaar na composer update")
	}
	gevonden, err := wordpressVersieUitLock(data)
	if err != nil {
		return err
	}
	if gevonden != strings.TrimPrefix(version, "v") {
		return fmt.Errorf("composer.lock bevat roots/wordpress %s, verwacht %s", gevonden, version)
	}
	return nil
}

// wordpressVersieUitLock haalt de roots/wordpress-versie (zonder leidende v)
// uit de inhoud van een composer.lock, in packages of packages-dev.
func wordpressVersieUitLock(data []byte) (string, error) {
	var lock struct {
		Packages    []struct{ Name, Version string } `json:"packages"`
		PackagesDev []struct{ Name, Version string } `json:"packages-dev"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return "", errors.New("composer.lock is geen geldige JSON")
	}
	for _, p := range append(lock.Packages, lock.PackagesDev...) {
		if p.Name == "roots/wordpress" {
			return strings.TrimPrefix(p.Version, "v"), nil
		}
	}
	return "", errors.New("composer.lock bevat geen roots/wordpress")
}

// execComposer draait de echte composer-binary.
type execComposer struct{}

func (execComposer) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, ComposerBin(), args...)
	cmd.Dir = dir
	cmd.Env = composerEnv(os.Environ(), ComposerBin())
	out, err := cmd.CombinedOutput()
	if err != nil {
		regels := strings.Split(strings.TrimSpace(string(out)), "\n")
		return string(out), fmt.Errorf("%w: %s", err, regels[len(regels)-1])
	}
	return string(out), nil
}

// composerEnv zet PATH zo dat composer (een PHP-script) php kan vinden, ook
// als de app vanuit Finder start zonder shell-PATH, en zet niet-interactief.
func composerEnv(base []string, composerBin string) []string {
	path := filepath.Dir(composerBin) + ":/opt/homebrew/bin:/usr/local/bin"
	env := make([]string, 0, len(base)+2)
	for _, e := range base {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			if v != "" {
				path += ":" + v
			}
			continue
		}
		env = append(env, e)
	}
	return append(env, "PATH="+path, "COMPOSER_NO_INTERACTION=1")
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
