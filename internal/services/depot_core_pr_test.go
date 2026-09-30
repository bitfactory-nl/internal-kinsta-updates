package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/github"
)

type fakeDepotGit struct {
	remote      string
	defaultBr   string
	worktreeDir string
	addedBranch string
	addedFrom   string
	removed     bool
	stagedPaths []string
	committed   string
	pushed      string
}

func (f *fakeDepotGit) DefaultBranchName(context.Context, string) (string, error) {
	return f.defaultBr, nil
}
func (f *fakeDepotGit) RemoteURL(context.Context, string) (string, error)     { return f.remote, nil }
func (f *fakeDepotGit) Fetch(context.Context, string) error                   { return nil }
func (f *fakeDepotGit) PrepareWorktree(context.Context, string, string) error { return nil }
func (f *fakeDepotGit) AddWorktree(_ context.Context, _, wt, branch, from string) error {
	f.worktreeDir, f.addedBranch, f.addedFrom = wt, branch, from
	return os.MkdirAll(wt, 0o755)
}
func (f *fakeDepotGit) RemoveWorktree(context.Context, string, string) error {
	f.removed = true
	return nil
}
func (f *fakeDepotGit) StageAllIn(context.Context, string) error {
	return errors.New("StageAllIn mag niet gebruikt worden")
}
func (f *fakeDepotGit) StagePathsIn(_ context.Context, _ string, paths ...string) error {
	f.stagedPaths = paths
	return nil
}
func (f *fakeDepotGit) CommitIn(_ context.Context, _, msg string) error {
	f.committed = msg
	return nil
}
func (f *fakeDepotGit) PushBranch(_ context.Context, _, br string) error { f.pushed = br; return nil }

type fakeComposer struct {
	calls [][]string
	err   error
	// lockVersion is de roots/wordpress-versie die de nep-update in
	// composer.lock zet; leeg = geen lock schrijven.
	lockVersion string
}

func (f *fakeComposer) Run(_ context.Context, dir string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return "", f.err
	}
	if len(args) > 0 && args[0] == "update" && f.lockVersion != "" {
		lock := `{"packages":[{"name":"composer/installers","version":"v2.3.0"},{"name":"roots/wordpress","version":"` + f.lockVersion + `"}],"packages-dev":[]}`
		if err := os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(lock), 0o644); err != nil {
			return "", err
		}
	}
	return "", nil
}

func depotFixture(t *testing.T) (*depotCorePRMaker, *fakeDepotGit, *fakeCorePulls, *fakeComposer) {
	t.Helper()
	g := &fakeDepotGit{remote: "git@github.com:bitfactory-nl/internal-satispressnobearsdev.git", defaultBr: "main"}
	p := &fakeCorePulls{created: &github.PullRequest{Number: 46, HTMLURL: "https://github.com/bitfactory-nl/internal-satispressnobearsdev/pull/46"}}
	c := &fakeComposer{lockVersion: "7.2.1"}
	return newDepotCorePRForTest(g, p, c, t.TempDir()), g, p, c
}

func TestOpenCorePRHappyPath(t *testing.T) {
	m, g, p, c := depotFixture(t)
	url, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/bitfactory-nl/internal-satispressnobearsdev/pull/46" {
		t.Fatalf("url %q", url)
	}
	if g.addedBranch != "update/wordpress-7.2.1" || g.addedFrom != "origin/main" {
		t.Fatalf("worktree %q van %q", g.addedBranch, g.addedFrom)
	}
	want := [][]string{
		{"require", "roots/wordpress:^7.2", "--no-update", "--no-scripts", "--no-plugins"},
		{"update", "roots/wordpress:7.2.1", "--with-dependencies", "--no-install", "--no-scripts", "--no-plugins"},
	}
	if len(c.calls) != 2 || strings.Join(c.calls[0], " ") != strings.Join(want[0], " ") || strings.Join(c.calls[1], " ") != strings.Join(want[1], " ") {
		t.Fatalf("composer %v", c.calls)
	}
	if strings.Join(g.stagedPaths, ",") != "composer.json,composer.lock" {
		t.Fatalf("staged %v", g.stagedPaths)
	}
	if g.committed != "chore: WordPress naar 7.2.1" || g.pushed != "update/wordpress-7.2.1" || !g.removed {
		t.Fatalf("commit %q push %q removed %v", g.committed, g.pushed, g.removed)
	}
	if p.lastHead != "update/wordpress-7.2.1" || p.lastBase != "main" || p.lastTitle != "WordPress 7.2.1" {
		t.Fatalf("pr %q→%q %q", p.lastHead, p.lastBase, p.lastTitle)
	}
	if _, err := os.Stat(g.worktreeDir); !os.IsNotExist(err) {
		t.Fatal("worktree-map niet opgeruimd")
	}
}

func TestOpenCorePRLockAfwijkendeVersie(t *testing.T) {
	m, g, p, c := depotFixture(t)
	c.lockVersion = "7.2"
	_, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1")
	if err == nil || err.Error() != "composer.lock bevat roots/wordpress 7.2, verwacht 7.2.1" {
		t.Fatalf("fout %v", err)
	}
	if g.committed != "" || g.pushed != "" || p.lastBase != "" || !g.removed {
		t.Fatalf("commit %q push %q pr %q removed %v", g.committed, g.pushed, p.lastBase, g.removed)
	}
}

func TestOpenCorePRLockXYVersie(t *testing.T) {
	m, g, _, c := depotFixture(t)
	c.lockVersion = "v7.1"
	if _, err := m.OpenCorePR(context.Background(), "/repo", "7.1"); err != nil {
		t.Fatal(err)
	}
	if g.committed != "chore: WordPress naar 7.1" || g.pushed != "update/wordpress-7.1" {
		t.Fatalf("commit %q push %q", g.committed, g.pushed)
	}
}

func TestOpenCorePRLockOntbreekt(t *testing.T) {
	m, g, _, c := depotFixture(t)
	c.lockVersion = ""
	_, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1")
	if err == nil || !strings.Contains(err.Error(), "composer.lock") {
		t.Fatalf("fout %v", err)
	}
	if g.committed != "" || g.pushed != "" {
		t.Fatalf("commit %q push %q", g.committed, g.pushed)
	}
}

func TestOpenCorePRBestaandePR(t *testing.T) {
	m, g, p, c := depotFixture(t)
	p.open = &github.PullRequest{HTMLURL: "https://github.com/x/y/pull/9"}
	url, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1")
	if err != nil || url != "https://github.com/x/y/pull/9" {
		t.Fatalf("url %q err %v", url, err)
	}
	if len(c.calls) != 0 || g.addedBranch != "" {
		t.Fatal("bij een bestaande PR mag er niets gebouwd worden")
	}
}

func TestOpenCorePRComposerFoutRuimtOp(t *testing.T) {
	m, g, _, c := depotFixture(t)
	c.err = errors.New("Your requirements could not be resolved")
	if _, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1"); err == nil || !strings.Contains(err.Error(), "composer") {
		t.Fatalf("fout %v", err)
	}
	if !g.removed || g.pushed != "" {
		t.Fatalf("removed %v pushed %q", g.removed, g.pushed)
	}
}

func TestOpenCorePRWeigertRareVersie(t *testing.T) {
	m, _, _, _ := depotFixture(t)
	if _, err := m.OpenCorePR(context.Background(), "/repo", "7.2; rm -rf /"); err == nil {
		t.Fatal("verwacht fout")
	}
}

func TestPluginWorkflowURL(t *testing.T) {
	m, _, _, _ := depotFixture(t)
	url, err := m.PluginWorkflowURL(context.Background(), "/repo")
	if err != nil || url != "https://github.com/bitfactory-nl/internal-satispressnobearsdev/actions/workflows/plugin-update.yml" {
		t.Fatalf("url %q err %v", url, err)
	}
}

func TestComposerBinViaEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "composer")
	os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("RDM_COMPOSER", p)
	resetComposerBin()
	t.Cleanup(resetComposerBin)
	if got := ComposerBin(); got != p {
		t.Fatalf("got %q", got)
	}
}

func TestOpenCorePRUniekeWorktreePerProject(t *testing.T) {
	m, g, _, _ := depotFixture(t)
	if _, err := m.OpenCorePR(context.Background(), "/a/depot", "7.2.1"); err != nil {
		t.Fatal(err)
	}
	eerste := g.worktreeDir
	if _, err := m.OpenCorePR(context.Background(), "/b/depot", "7.2.1"); err != nil {
		t.Fatal(err)
	}
	if eerste == g.worktreeDir {
		t.Fatalf("zelfde worktree-map %q voor verschillende projecten", eerste)
	}
}

func TestOpenCorePRRunGuard(t *testing.T) {
	m, _, _, c := depotFixture(t)
	m.claim("/repo@7.2.1")
	_, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1")
	if err == nil || !strings.Contains(err.Error(), "loopt al een PR-run") {
		t.Fatalf("fout %v", err)
	}
	if len(c.calls) != 0 {
		t.Fatal("composer mag niet draaien")
	}
}

func TestOpenCorePRFindFoutStopt(t *testing.T) {
	m, g, p, c := depotFixture(t)
	p.findErr = errors.New("boem")
	if _, err := m.OpenCorePR(context.Background(), "/repo", "7.2.1"); err == nil || !strings.Contains(err.Error(), "bestaande PR zoeken") {
		t.Fatalf("fout %v", err)
	}
	if len(c.calls) != 0 || g.addedBranch != "" {
		t.Fatal("er mag niets gebouwd worden")
	}
}

func TestComposerEnv(t *testing.T) {
	env := composerEnv([]string{"HOME=/h", "PATH=/usr/bin"}, "/opt/x/composer")
	var paths []string
	nonInt := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			paths = append(paths, e)
		}
		if e == "COMPOSER_NO_INTERACTION=1" {
			nonInt = true
		}
	}
	if len(paths) != 1 || paths[0] != "PATH=/opt/x:/opt/homebrew/bin:/usr/local/bin:/usr/bin" || !nonInt {
		t.Fatalf("env %v", env)
	}
}
