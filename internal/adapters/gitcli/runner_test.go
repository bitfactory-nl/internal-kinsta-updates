package gitcli

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestNewGitCmdVeiligeDefaults(t *testing.T) {
	cmd := newGitCmd(context.Background(), "/tmp", "status", "--porcelain")

	want := []string{"git", "--no-optional-locks", "status", "--porcelain"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("Args = %v, want %v", cmd.Args, want)
	}
	if cmd.Dir != "/tmp" {
		t.Errorf("Dir = %q, want /tmp", cmd.Dir)
	}
	// Nette afbraak bij context-cancel: eerst SIGTERM, na de gratieperiode
	// pas SIGKILL — anders blijven index.lock-bestanden achter.
	if cmd.Cancel == nil {
		t.Error("Cancel ontbreekt: context-cancel zou SIGTERM moeten sturen i.p.v. SIGKILL")
	}
	if cmd.WaitDelay != 2*time.Second {
		t.Errorf("WaitDelay = %v, want 2s", cmd.WaitDelay)
	}
}

func TestRunWerktMetVlag(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skipf("git init niet beschikbaar: %v", err)
	}
	out, err := Run(context.Background(), dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "true" {
		t.Errorf("rev-parse = %q, want true", out)
	}
}
