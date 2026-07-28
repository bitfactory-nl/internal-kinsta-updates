package gitcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initWorktreeFixtureRepo maakt in dir een git-repo met precies één commit op
// HEAD, zodat er een geldige fromRef is om een worktree vanaf te starten.
// Patroon zie gitInit in commands_test.go; eigen naam om te voorkomen dat
// beide test-helpers in dit package botsen.
func initWorktreeFixtureRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t.nl"},
		{"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestWorktreeAddEnRemove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git niet beschikbaar")
	}

	repoDir := t.TempDir()
	initWorktreeFixtureRepo(t, repoDir)

	worktreePath := filepath.Join(t.TempDir(), "wt")
	ctx := context.Background()

	if err := WorktreeAdd(ctx, repoDir, worktreePath, "update/test", "HEAD"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	if info, err := os.Stat(worktreePath); err != nil || !info.IsDir() {
		t.Fatalf("worktree-map ontbreekt op %s: %v", worktreePath, err)
	}
	if !BranchExists(ctx, repoDir, "update/test") {
		t.Errorf("branch update/test bestaat niet na WorktreeAdd")
	}

	if err := WorktreeRemove(ctx, repoDir, worktreePath); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree-map %s bestaat nog na WorktreeRemove", worktreePath)
	}
}
