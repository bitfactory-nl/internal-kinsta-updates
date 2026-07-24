package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// writeFakeSidecar writes a POSIX shell script that ignores stdin and prints a
// canned RunResponse, so the exec+stdin/stdout+JSON plumbing is tested without
// Node/Playwright.
func writeFakeSidecar(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	body := "#!/bin/sh\ncat >/dev/null\n"
	if stdout != "" {
		body += "printf '%s' '" + stdout + "'\n"
	}
	if exitCode != 0 {
		body += "exit 1\n"
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestRunnerParsesResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	canned := `{"steps":[{"index":0,"action":"navigate","baseline":{"screenshot":"/t/b.png","consoleErrors":[],"statusCodes":{"/":200}},"update":{"screenshot":"/t/u.png","consoleErrors":[],"statusCodes":{"/":200}}}]}`
	script := writeFakeSidecar(t, canned, 0)

	r := &Runner{Bin: "/bin/sh", Args: []string{script}}
	resp, err := r.Run(context.Background(), RunRequest{
		Flow: domain.Flow{Name: "F", Steps: []domain.Step{{Action: domain.StepNavigate, Target: "/"}}},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(resp.Steps) != 1 || resp.Steps[0].Baseline.StatusCodes["/"] != 200 {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

func TestRunnerNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakeSidecar(t, "", 1)
	r := &Runner{Bin: "/bin/sh", Args: []string{script}}
	if _, err := r.Run(context.Background(), RunRequest{}); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestRunnerBadJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakeSidecar(t, "not json", 0)
	r := &Runner{Bin: "/bin/sh", Args: []string{script}}
	if _, err := r.Run(context.Background(), RunRequest{}); err == nil {
		t.Fatal("expected error on bad JSON")
	}
}
