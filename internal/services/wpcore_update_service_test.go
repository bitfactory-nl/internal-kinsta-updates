package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/domain"
)

// --- fakes ---

type fakeCoreGit struct {
	defaultBranch string
	defaultErr    error
	remote        string
	remoteErr     error

	worktreePath  string // door WorktreeAdd aangemaakt (fixture-inhoud)
	worktreeAdded bool
	removed       bool
	fetched       bool
	staged        bool
	committed     string
	pushedBranch  string
	pushErr       error
	addErr        error
	prepared      bool
	// versieInWorktree bepaalt de version.php die AddWorktree neerzet.
	versieInWorktree string
}

func (f *fakeCoreGit) DefaultBranchName(_ context.Context, _ string) (string, error) {
	return f.defaultBranch, f.defaultErr
}

func (f *fakeCoreGit) RemoteURL(_ context.Context, _ string) (string, error) {
	return f.remote, f.remoteErr
}

func (f *fakeCoreGit) Fetch(_ context.Context, _ string) error {
	f.fetched = true
	return nil
}

func (f *fakeCoreGit) AddWorktree(_ context.Context, _, worktreePath, _, _ string) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.worktreeAdded = true
	f.worktreePath = worktreePath
	// Bouw een minimale webroot zodat replaceCore iets te doen heeft.
	root := filepath.Join(worktreePath, "public")
	versie := f.versieInWorktree
	if versie == "" {
		versie = "6.7.2"
	}
	for rel, inhoud := range map[string]string{
		"wp-includes/version.php": "$wp_version = '" + versie + "';",
		"wp-config.php":           "geheim",
	} {
		pad := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(pad), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(pad, []byte(inhoud), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeCoreGit) RemoveWorktree(_ context.Context, _, _ string) error {
	f.removed = true
	return nil
}

func (f *fakeCoreGit) StageAllIn(_ context.Context, _ string) error {
	f.staged = true
	return nil
}

func (f *fakeCoreGit) CommitIn(_ context.Context, _, message string) error {
	f.committed = message
	return nil
}

func (f *fakeCoreGit) PushBranch(_ context.Context, _, branch string) error {
	f.pushedBranch = branch
	return f.pushErr
}

type fakeCoreDownloader struct {
	data []byte
	err  error
	urls []string
}

func (f *fakeCoreDownloader) Download(_ context.Context, url string) ([]byte, error) {
	f.urls = append(f.urls, url)
	return f.data, f.err
}

type fakeCorePulls struct {
	open      *github.PullRequest
	findErr   error
	created   *github.PullRequest
	createErr error
	lastHead  string
	lastBase  string
	lastTitle string
}

func (f *fakeCorePulls) FindOpenPull(_ context.Context, _, _, head string) (*github.PullRequest, error) {
	f.lastHead = head
	return f.open, f.findErr
}

func (f *fakeCorePulls) CreatePull(_ context.Context, _, _, head, base, title, _ string) (*github.PullRequest, error) {
	f.lastHead, f.lastBase, f.lastTitle = head, base, title
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.created != nil {
		return f.created, nil
	}
	return &github.PullRequest{Number: 42, HTMLURL: "https://github.com/o/r/pull/42", State: "open"}, nil
}

// coreTestService bouwt een service met fakes en één project.
func coreTestService(t *testing.T, git *fakeCoreGit, dl *fakeCoreDownloader, pulls *fakeCorePulls) *WPCoreUpdateService {
	t.Helper()
	projects := &fakeReportProjects{projects: map[string]domain.Project{
		"p1": {ID: "p1", DisplayName: "AFC", Path: t.TempDir()},
	}}
	return newWPCoreUpdateServiceForTest(projects, git, dl, pulls, t.TempDir())
}

func geldigeCoreZip(t *testing.T) []byte {
	t.Helper()
	return bouwCoreZip(t, map[string]string{
		"wp-admin/admin.php":      "nieuw",
		"wp-includes/version.php": "$wp_version = '7.0.2';",
		"wp-login.php":            "nieuw",
	})
}

// --- tests ---

func TestCoreUpdateHappyPath(t *testing.T) {
	git := &fakeCoreGit{defaultBranch: "release/1.0.x", remote: "git@github.com:bitfactory-nl/web-afcnl.git"}
	dl := &fakeCoreDownloader{data: geldigeCoreZip(t)}
	pulls := &fakeCorePulls{}
	svc := coreTestService(t, git, dl, pulls)

	res := svc.UpdateProject("p1", "7.0.2")

	if res.Status != "pr_created" {
		t.Fatalf("Status = %q (%s), want pr_created", res.Status, res.Error)
	}
	if res.Branch != "update/wordpress-7.0.2" {
		t.Errorf("Branch = %q", res.Branch)
	}
	if res.PullRequestURL != "https://github.com/o/r/pull/42" {
		t.Errorf("PullRequestURL = %q", res.PullRequestURL)
	}
	if !git.fetched || !git.worktreeAdded || !git.staged || git.committed == "" {
		t.Errorf("git-stappen niet allemaal doorlopen: %+v", git)
	}
	if git.pushedBranch != "update/wordpress-7.0.2" {
		t.Errorf("gepushte branch = %q", git.pushedBranch)
	}
	if pulls.lastBase != "release/1.0.x" {
		t.Errorf("PR-base = %q, want de default release-branch", pulls.lastBase)
	}
	if !git.removed {
		t.Error("worktree is niet opgeruimd")
	}
	if len(dl.urls) != 1 || dl.urls[0] != "https://wordpress.org/wordpress-7.0.2-no-content.zip" {
		t.Errorf("download-URL = %v", dl.urls)
	}
}

func TestCoreUpdateSkiptZonderReleaseBranch(t *testing.T) {
	git := &fakeCoreGit{defaultBranch: "main", remote: "git@github.com:o/r.git"}
	svc := coreTestService(t, git, &fakeCoreDownloader{data: geldigeCoreZip(t)}, &fakeCorePulls{})

	res := svc.UpdateProject("p1", "7.0.2")

	if res.Status != "skipped_no_release" {
		t.Fatalf("Status = %q, want skipped_no_release", res.Status)
	}
	if git.worktreeAdded {
		t.Error("er is een worktree aangemaakt ondanks de skip")
	}
}

func TestCoreUpdateIdempotentBijBestaandePR(t *testing.T) {
	git := &fakeCoreGit{defaultBranch: "release/1.0.x", remote: "git@github.com:o/r.git"}
	pulls := &fakeCorePulls{open: &github.PullRequest{Number: 7, HTMLURL: "https://github.com/o/r/pull/7", State: "open"}}
	svc := coreTestService(t, git, &fakeCoreDownloader{data: geldigeCoreZip(t)}, pulls)

	res := svc.UpdateProject("p1", "7.0.2")

	if res.Status != "exists" {
		t.Fatalf("Status = %q, want exists", res.Status)
	}
	if res.PullRequestURL != "https://github.com/o/r/pull/7" {
		t.Errorf("PullRequestURL = %q, want de bestaande PR", res.PullRequestURL)
	}
	if git.worktreeAdded {
		t.Error("bestaande PR moet zonder git-werk afgehandeld worden")
	}
}

func TestCoreUpdateRuimtWorktreeOpBijFout(t *testing.T) {
	git := &fakeCoreGit{defaultBranch: "release/1.0.x", remote: "git@github.com:o/r.git", pushErr: errors.New("geen toegang")}
	svc := coreTestService(t, git, &fakeCoreDownloader{data: geldigeCoreZip(t)}, &fakeCorePulls{})

	res := svc.UpdateProject("p1", "7.0.2")

	if res.Status != "error" {
		t.Fatalf("Status = %q, want error", res.Status)
	}
	if res.Error == "" {
		t.Error("Error-veld is leeg bij een mislukte push")
	}
	if !git.removed {
		t.Error("worktree is niet opgeruimd na een fout")
	}
}

func TestCoreUpdateFoutBijSlechteDownload(t *testing.T) {
	git := &fakeCoreGit{defaultBranch: "release/1.0.x", remote: "git@github.com:o/r.git"}
	dl := &fakeCoreDownloader{data: []byte("<html>404</html>")}
	svc := coreTestService(t, git, dl, &fakeCorePulls{})

	res := svc.UpdateProject("p1", "7.0.2")

	if res.Status != "error" {
		t.Fatalf("Status = %q, want error", res.Status)
	}
	if git.pushedBranch != "" {
		t.Error("er is gepusht ondanks een ongeldige download")
	}
	if !git.removed {
		t.Error("worktree is niet opgeruimd na een ongeldige download")
	}
}

func TestCoreUpdateOnbekendProject(t *testing.T) {
	svc := coreTestService(t, &fakeCoreGit{}, &fakeCoreDownloader{}, &fakeCorePulls{})
	res := svc.UpdateProject("bestaat-niet", "7.0.2")
	if res.Status != "error" || res.Error == "" {
		t.Fatalf("onbekend project moet een fout geven: %+v", res)
	}
}

func TestCoreUpdateLegeVersie(t *testing.T) {
	svc := coreTestService(t, &fakeCoreGit{defaultBranch: "release/1.0.x"}, &fakeCoreDownloader{}, &fakeCorePulls{})
	res := svc.UpdateProject("p1", "")
	if res.Status != "error" || res.Error == "" {
		t.Fatalf("lege doelversie moet een fout geven: %+v", res)
	}
}

func (f *fakeCoreGit) PrepareWorktree(_ context.Context, _, _ string) error {
	f.prepared = true
	return nil
}

func TestCoreUpdateRuimtOpVoorNieuwePoging(t *testing.T) {
	git := &fakeCoreGit{defaultBranch: "release/1.0.x", remote: "git@github.com:o/r.git"}
	svc := coreTestService(t, git, &fakeCoreDownloader{data: geldigeCoreZip(t)}, &fakeCorePulls{})

	if res := svc.UpdateProject("p1", "7.0.2"); res.Status != "pr_created" {
		t.Fatalf("Status = %q (%s)", res.Status, res.Error)
	}
	if !git.prepared {
		t.Error("PrepareWorktree is niet aangeroepen — restanten van een afgebroken run blijven dan liggen")
	}
}

func TestCoreUpdateAlActueel(t *testing.T) {
	git := &fakeCoreGit{defaultBranch: "release/1.0.x", remote: "git@github.com:o/r.git", versieInWorktree: "7.0.2"}
	svc := coreTestService(t, git, &fakeCoreDownloader{data: geldigeCoreZip(t)}, &fakeCorePulls{})

	res := svc.UpdateProject("p1", "7.0.2")

	if res.Status != "up_to_date" {
		t.Fatalf("Status = %q, want up_to_date", res.Status)
	}
	if git.pushedBranch != "" || git.committed != "" {
		t.Error("er is gecommit/gepusht terwijl de branch al op de doelversie stond")
	}
	if !git.removed {
		t.Error("worktree is niet opgeruimd")
	}
}

func TestCoreUpdateWeigertOngeldigeVersie(t *testing.T) {
	svc := coreTestService(t, &fakeCoreGit{defaultBranch: "release/1.0.x"}, &fakeCoreDownloader{}, &fakeCorePulls{})
	for _, v := range []string{"", "latest", "7", "../../etc", "7.0.2; rm -rf /"} {
		res := svc.UpdateProject("p1", v)
		if res.Status != "error" {
			t.Errorf("versie %q gaf status %q, want error", v, res.Status)
		}
	}
}
