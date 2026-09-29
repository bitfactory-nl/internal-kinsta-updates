package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMCPLaunchOngeldigBestand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(home, ".config", "rdm", "wp-sites.yml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("sites:\n  - id: X\n"), 0o600)

	var stdout, stderr bytes.Buffer
	code := RunMCPLaunch(context.Background(), strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exitcode %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout moet leeg blijven (MCP-kanaal): %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "wp-sites.yml") {
		t.Fatalf("stderr noemt het bestand niet: %q", stderr.String())
	}
}
