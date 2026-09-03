package gitcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t.nl"},
		{"config", "user.name", "t"},
		{"checkout", "-q", "-b", "release/1.0.x"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestDefaultBranchFallsBackToRelease(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	got, err := DefaultBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "release/1.0.x" {
		t.Errorf("got %q want release/1.0.x", got)
	}
	_ = filepath.Base(dir)
}

func TestCloneMaaktCheckoutMetOriginRemote(t *testing.T) {
	bron := t.TempDir()
	gitInit(t, bron)

	doelParent := t.TempDir()
	if err := Clone(context.Background(), doelParent, bron, "kopie"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	dest := filepath.Join(doelParent, "kopie")
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatalf("clone landde niet op %s: %v", dest, err)
	}

	// De naam "origin" is geen detail: de org-sync-matching en de rest van de
	// tool lezen juist die remote om een checkout aan een GitHub-repo te koppelen.
	got, err := Run(context.Background(), dest, "remote", "get-url", "origin")
	if err != nil {
		t.Fatalf("origin-remote lezen: %v", err)
	}
	if got != bron {
		t.Errorf("origin = %q, wil %q", got, bron)
	}
}

func TestCloneWeigertBestaandeDoelmap(t *testing.T) {
	bron := t.TempDir()
	gitInit(t, bron)

	doelParent := t.TempDir()
	bestaand := filepath.Join(doelParent, "kopie")
	if err := os.MkdirAll(bestaand, 0o755); err != nil {
		t.Fatalf("map aanmaken: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bestaand, "bestand.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("bestand schrijven: %v", err)
	}

	if err := Clone(context.Background(), doelParent, bron, "kopie"); err == nil {
		t.Fatal("Clone slaagde in een niet-lege bestaande map")
	}
	if _, err := os.Stat(filepath.Join(bestaand, "bestand.txt")); err != nil {
		t.Errorf("bestaande inhoud is aangetast: %v", err)
	}
}

// TestRunEnvGeeftExtraEnvDoor bewijst het plumbing waar Clone op leunt: zonder
// doorgegeven omgeving zouden GIT_TERMINAL_PROMPT/GIT_SSH_COMMAND geen effect
// hebben en kan een clone alsnog op een prompt blijven hangen.
func TestRunEnvGeeftExtraEnvDoor(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	extra := []string{"GIT_AUTHOR_NAME=envtest", "GIT_AUTHOR_EMAIL=env@test.nl"}
	got, err := RunEnv(context.Background(), dir, extra, "var", "GIT_AUTHOR_IDENT")
	if err != nil {
		t.Fatalf("RunEnv: %v", err)
	}
	if !strings.Contains(got, "envtest") {
		t.Errorf("GIT_AUTHOR_IDENT = %q, wil dat de meegegeven GIT_AUTHOR_NAME erin staat", got)
	}
}

func TestNonInteractiveEnvZetGeenPrompts(t *testing.T) {
	env := strings.Join(nonInteractiveEnv(), " ")
	for _, wil := range []string{"GIT_TERMINAL_PROMPT=0", "BatchMode=yes"} {
		if !strings.Contains(env, wil) {
			t.Errorf("nonInteractiveEnv() = %v, wil %q erin", nonInteractiveEnv(), wil)
		}
	}
}
