package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitCoreFixture maakt een echte repo met één commit en geeft het pad terug.
func gitCoreFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git niet beschikbaar")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "release/1.0.x")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "leesmij.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir
}

// TestWorktreeRetryNaAfgebrokenRun dekt het scenario waarin de app tijdens een
// core-update hard afgebroken werd: de worktree-map is weg, maar git heeft hem
// nog geregistreerd. Een volgende poging voor dezelfde doelversie moet gewoon
// werken in plaats van te blijven hangen op "missing but already registered".
func TestWorktreeRetryNaAfgebrokenRun(t *testing.T) {
	repo := gitCoreFixture(t)
	ops := gitCoreOps{}
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "wt")
	const branch = "update/wordpress-7.0.2"

	if err := ops.AddWorktree(ctx, repo, wt, branch, "HEAD"); err != nil {
		t.Fatalf("eerste AddWorktree: %v", err)
	}
	// Simuleer een crash: map weg, git-registratie blijft achter.
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	if err := ops.PrepareWorktree(ctx, repo, wt); err != nil {
		t.Fatalf("PrepareWorktree: %v", err)
	}
	if err := ops.AddWorktree(ctx, repo, wt, branch, "HEAD"); err != nil {
		t.Fatalf("tweede AddWorktree na afgebroken run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "leesmij.txt")); err != nil {
		t.Errorf("worktree niet opnieuw aangemaakt: %v", err)
	}
}

// TestWorktreeRetryBijBestaandeBranch dekt het scenario waarin een eerdere run
// de branch al aanmaakte (bijv. push gelukt maar PR mislukt, of PR gesloten
// zonder merge). De branch moet hergebruikt/gereset worden.
func TestWorktreeRetryBijBestaandeBranch(t *testing.T) {
	repo := gitCoreFixture(t)
	ops := gitCoreOps{}
	ctx := context.Background()
	const branch = "update/wordpress-7.0.2"

	wt1 := filepath.Join(t.TempDir(), "wt1")
	if err := ops.AddWorktree(ctx, repo, wt1, branch, "HEAD"); err != nil {
		t.Fatalf("eerste AddWorktree: %v", err)
	}
	if err := ops.RemoveWorktree(ctx, repo, wt1); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	// Branch bestaat nu lokaal; een nieuwe poging mag daar niet op stuklopen.
	wt2 := filepath.Join(t.TempDir(), "wt2")
	if err := ops.PrepareWorktree(ctx, repo, wt2); err != nil {
		t.Fatalf("PrepareWorktree: %v", err)
	}
	if err := ops.AddWorktree(ctx, repo, wt2, branch, "HEAD"); err != nil {
		t.Fatalf("tweede AddWorktree bij bestaande branch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt2, "leesmij.txt")); err != nil {
		t.Errorf("worktree niet aangemaakt: %v", err)
	}
}
