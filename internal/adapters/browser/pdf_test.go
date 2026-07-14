package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakePDFSidecar writes a POSIX shell script that ignores stdin and
// prints a canned response, mirroring writeFakeSidecar in runner_test.go.
func writeFakePDFSidecar(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-pdf.sh")
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

func TestPDFRunnerSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakePDFSidecar(t, `{"ok":true,"path":"/tmp/out.pdf"}`, 0)
	r := &PDFRunner{Bin: "/bin/sh", Args: []string{script}}
	if err := r.RenderPDF(context.Background(), "<html></html>", "/tmp/out.pdf"); err != nil {
		t.Fatalf("render: %v", err)
	}
}

func TestPDFRunnerErrorJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakePDFSidecar(t, `{"error":"boom"}`, 0)
	r := &PDFRunner{Bin: "/bin/sh", Args: []string{script}}
	if err := r.RenderPDF(context.Background(), "<html></html>", "/tmp/out.pdf"); err == nil {
		t.Fatal("expected error from sidecar error field")
	}
}

func TestPDFRunnerNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakePDFSidecar(t, "", 1)
	r := &PDFRunner{Bin: "/bin/sh", Args: []string{script}}
	if err := r.RenderPDF(context.Background(), "<html></html>", "/tmp/out.pdf"); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestPDFRunnerBadJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sidecar uses /bin/sh")
	}
	script := writeFakePDFSidecar(t, "not json", 0)
	r := &PDFRunner{Bin: "/bin/sh", Args: []string{script}}
	if err := r.RenderPDF(context.Background(), "<html></html>", "/tmp/out.pdf"); err == nil {
		t.Fatal("expected error on bad JSON")
	}
}
