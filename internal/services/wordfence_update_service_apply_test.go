package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fakeApplyDownloader is a pluginDownloader test double for ApplyProject tests.
type fakeApplyDownloader struct {
	version string
	url     string
	zipData []byte
}

func (f fakeApplyDownloader) LatestVersion(ctx context.Context, slug string) (string, string, error) {
	return f.version, f.url, nil
}

func (f fakeApplyDownloader) Download(ctx context.Context, url string) ([]byte, error) {
	return f.zipData, nil
}

// runGitApply runs a git command in dir, failing the test on error.
func runGitApply(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupApplyTestProject creates a real git repo under a temp root with a
// single plugin at the given slug/version, checked out on a release/* branch
// (so GitService.DefaultBranch resolves to it), and returns a real
// ProjectService (scanned) plus the discovered project's ID and its path.
func setupApplyTestProject(t *testing.T, slug, version string) (ps *ProjectService, projectID, projectDir string) {
	t.Helper()
	root := t.TempDir()
	projectDir = filepath.Join(root, "myproject")
	pluginDir := filepath.Join(projectDir, "public", "wp-content", "plugins", slug)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	phpContent := "<?php\n/**\n * Plugin Name: " + slug + "\n * Version: " + version + "\n */\n"
	if err := os.WriteFile(filepath.Join(pluginDir, slug+".php"), []byte(phpContent), 0o644); err != nil {
		t.Fatal(err)
	}

	runGitApply(t, projectDir, "init")
	runGitApply(t, projectDir, "checkout", "-b", "release/1.0.x")
	runGitApply(t, projectDir, "config", "user.email", "test@example.com")
	runGitApply(t, projectDir, "config", "user.name", "Test")
	runGitApply(t, projectDir, "add", "-A")
	runGitApply(t, projectDir, "commit", "-m", "initial")

	ps = NewProjectService([]string{root})
	if _, err := ps.Scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, p := range ps.List() {
		if p.Path == projectDir {
			projectID = p.ID
		}
	}
	if projectID == "" {
		t.Fatalf("project at %q not found after scan", projectDir)
	}
	return ps, projectID, projectDir
}

func TestApplyProjectCleanWorktreeStagesOnlyPluginPath(t *testing.T) {
	slug := "wordfence"
	ps, projectID, projectDir := setupApplyTestProject(t, slug, "1.0.0")
	gs := NewGitService(ps)
	svc := NewWordfenceUpdateService(gs, ps)
	svc.wporg = fakeApplyDownloader{
		version: "2.0.0",
		url:     "https://example.invalid/wordfence.zip",
		zipData: makeZip(t, slug, slug+".php", "<?php\n/**\n * Plugin Name: "+slug+"\n * Version: 2.0.0\n */\n"),
	}

	// A stray untracked file must never be swept into the security commit.
	// Note: an untracked file makes GetStatus report the worktree as dirty
	// (isDirty counts Untracked too), so autoStash=true is required here —
	// `git stash push` without -u is a no-op for untracked-only changes
	// (exits cleanly, leaves stray.txt untracked), which is exactly what we
	// want to exercise together with Fix 1's scoped staging.
	strayPath := filepath.Join(projectDir, "stray.txt")
	if err := os.WriteFile(strayPath, []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := svc.ApplyProject(UpdateSelection{ProjectID: projectID, Slugs: []string{slug}}, true)

	if res.Status != "updated" {
		t.Fatalf("status = %q, want updated (error=%q)", res.Status, res.Error)
	}
	if !res.Stashed {
		t.Errorf("Stashed should be true: ApplyProject called StashSave for the dirty (untracked) worktree")
	}
	if !strings.HasPrefix(res.Branch, "security/wordfence-") {
		t.Errorf("branch = %q, want prefix security/wordfence-", res.Branch)
	}

	// Plugin file content changed.
	got, err := os.ReadFile(filepath.Join(projectDir, "public", "wp-content", "plugins", slug, slug+".php"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "2.0.0") {
		t.Errorf("plugin file not updated, got: %q", got)
	}

	// Exactly one commit matches the fix(security) message pattern.
	log := runGitApply(t, projectDir, "log", "--pretty=%s")
	subjects := strings.Split(log, "\n")
	pattern := regexp.MustCompile(`^fix\(security\): update ` + regexp.QuoteMeta(slug) + ` .*\(Wordfence\)$`)
	matches := 0
	for _, s := range subjects {
		if pattern.MatchString(s) {
			matches++
		}
	}
	if matches != 1 {
		t.Errorf("expected exactly 1 matching commit, got %d in log: %v", matches, subjects)
	}

	// The commit must not contain the stray file — only the plugin path.
	headFiles := runGitApply(t, projectDir, "show", "--name-only", "--pretty=format:", "HEAD")
	for _, f := range strings.Split(headFiles, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.HasPrefix(f, "public/wp-content/plugins/"+slug+"/") {
			t.Errorf("commit contains unexpected file: %q", f)
		}
		if f == "stray.txt" {
			t.Errorf("commit must not contain stray.txt")
		}
	}

	// stray.txt must still be untracked afterwards.
	statusOut := runGitApply(t, projectDir, "status", "--porcelain")
	if !strings.Contains(statusOut, "?? stray.txt") {
		t.Errorf("expected stray.txt to remain untracked, status: %q", statusOut)
	}
}

func TestApplyProjectDirtyWorktreeNeedsStashNoAutoStash(t *testing.T) {
	slug := "wordfence"
	ps, projectID, projectDir := setupApplyTestProject(t, slug, "1.0.0")
	gs := NewGitService(ps)
	svc := NewWordfenceUpdateService(gs, ps)
	svc.wporg = fakeApplyDownloader{
		version: "2.0.0",
		url:     "https://example.invalid/wordfence.zip",
		zipData: makeZip(t, slug, slug+".php", "<?php\n/**\n * Plugin Name: "+slug+"\n * Version: 2.0.0\n */\n"),
	}

	// Dirty the worktree: unstaged modification of a tracked file.
	pluginFile := filepath.Join(projectDir, "public", "wp-content", "plugins", slug, slug+".php")
	if err := os.WriteFile(pluginFile, []byte("<?php\n// local WIP change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	beforeBranch := runGitApply(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	beforeLog := runGitApply(t, projectDir, "log", "--oneline")

	res := svc.ApplyProject(UpdateSelection{ProjectID: projectID, Slugs: []string{slug}}, false)

	if res.Status != "needs_stash" {
		t.Fatalf("status = %q, want needs_stash (error=%q)", res.Status, res.Error)
	}
	if res.Branch != "" {
		t.Errorf("branch should be empty, got %q", res.Branch)
	}
	if res.Stashed {
		t.Errorf("Stashed should be false when needs_stash is returned without autoStash")
	}

	// No branch or commit must have been created.
	afterBranch := runGitApply(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	if afterBranch != beforeBranch {
		t.Errorf("branch changed: before=%q after=%q", beforeBranch, afterBranch)
	}
	afterLog := runGitApply(t, projectDir, "log", "--oneline")
	if afterLog != beforeLog {
		t.Errorf("commit log changed:\nbefore=%q\nafter=%q", beforeLog, afterLog)
	}
	branches := runGitApply(t, projectDir, "branch", "--list", "security/wordfence-*")
	if branches != "" {
		t.Errorf("no security/wordfence-* branch should exist, found: %q", branches)
	}
}

// TestApplyProjectSameDayRerunReusesBranch covers Fix 2: a second same-day
// run must reuse the existing security/wordfence-<date> branch instead of
// failing with "branch aanmaken mislukt" (git checkout -b errors when the
// branch already exists).
func TestApplyProjectSameDayRerunReusesBranch(t *testing.T) {
	slug := "wordfence"
	ps, projectID, projectDir := setupApplyTestProject(t, slug, "1.0.0")
	gs := NewGitService(ps)
	svc := NewWordfenceUpdateService(gs, ps)
	svc.wporg = fakeApplyDownloader{
		version: "2.0.0",
		url:     "https://example.invalid/wordfence.zip",
		zipData: makeZip(t, slug, slug+".php", "<?php\n/**\n * Plugin Name: "+slug+"\n * Version: 2.0.0\n */\n"),
	}

	first := svc.ApplyProject(UpdateSelection{ProjectID: projectID, Slugs: []string{slug}}, false)
	if first.Status != "updated" {
		t.Fatalf("first run: status = %q, want updated (error=%q)", first.Status, first.Error)
	}

	// Simulate a second, same-day re-run against a newer release (otherwise
	// the second download is byte-identical and there is nothing new to
	// commit, which would mask the branch-reuse behaviour under test).
	svc.wporg = fakeApplyDownloader{
		version: "3.0.0",
		url:     "https://example.invalid/wordfence.zip",
		zipData: makeZip(t, slug, slug+".php", "<?php\n/**\n * Plugin Name: "+slug+"\n * Version: 3.0.0\n */\n"),
	}
	second := svc.ApplyProject(UpdateSelection{ProjectID: projectID, Slugs: []string{slug}}, false)
	if second.Status != "updated" {
		t.Fatalf("second run: status = %q, want updated (error=%q)", second.Status, second.Error)
	}
	if second.Branch != first.Branch {
		t.Errorf("second run branch = %q, want same as first run %q", second.Branch, first.Branch)
	}

	// Exactly one branch with that name must exist (no duplicate/-2 suffix).
	branches := runGitApply(t, projectDir, "branch", "--list", first.Branch)
	count := 0
	for _, l := range strings.Split(branches, "\n") {
		if strings.TrimSpace(l) != "" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one branch named %q, found %d: %q", first.Branch, count, branches)
	}

	// Two update commits should now exist on that branch.
	log := runGitApply(t, projectDir, "log", "--pretty=%s")
	pattern := regexp.MustCompile(`^fix\(security\): update ` + regexp.QuoteMeta(slug) + ` .*\(Wordfence\)$`)
	matches := 0
	for _, s := range strings.Split(log, "\n") {
		if pattern.MatchString(s) {
			matches++
		}
	}
	if matches != 2 {
		t.Errorf("expected exactly 2 matching commits after two runs, got %d", matches)
	}
}
