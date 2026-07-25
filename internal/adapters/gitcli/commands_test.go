package gitcli

import (
	"context"
	"os/exec"
	"path/filepath"
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
