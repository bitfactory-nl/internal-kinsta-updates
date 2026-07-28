package gitread

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGitCommandGebruiktNoOptionalLocks(t *testing.T) {
	cmd := gitCommand("/tmp", "status", "--porcelain=v1")

	want := []string{"git", "--no-optional-locks", "status", "--porcelain=v1"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("Args = %v, want %v", cmd.Args, want)
	}
	if cmd.Dir != "/tmp" {
		t.Errorf("Dir = %q, want /tmp", cmd.Dir)
	}
}

// TestStatusLaatGeenIndexLockAchter draait de echte scanner-flow op een echte
// repo: na Status mag er geen .git/index.lock (meer) bestaan.
func TestStatusLaatGeenIndexLockAchter(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Skipf("git niet beschikbaar (%v)", err)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Status(dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.IsRepo {
		t.Fatalf("IsRepo = false, want true: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Errorf("index.lock bestaat na Status (err=%v) — scanner moet lock-vrij zijn", err)
	}
}
