package services

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func makeZip(t *testing.T, slug, filename, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(slug + "/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractZipReplace(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "public", "wp-content", "plugins")
	old := filepath.Join(pluginsDir, "cf7")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	// stale file that must be gone after replace
	if err := os.WriteFile(filepath.Join(old, "stale.php"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipData := makeZip(t, "cf7", "wp-cf7.php", "<?php // v5.9.2")
	if err := extractZipReplace(zipData, pluginsDir, "cf7"); err != nil {
		t.Fatalf("extractZipReplace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old, "stale.php")); !os.IsNotExist(err) {
		t.Error("stale file should be removed")
	}
	got, err := os.ReadFile(filepath.Join(old, "wp-cf7.php"))
	if err != nil || string(got) != "<?php // v5.9.2" {
		t.Errorf("new file missing/wrong: %v %q", err, got)
	}
}

func TestExtractZipReplaceRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing plugin dir that must survive a rejected (unsafe) zip.
	existing := filepath.Join(pluginsDir, "evil")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	keepFile := filepath.Join(existing, "keep.php")
	if err := os.WriteFile(keepFile, []byte("<?php // keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("../evil.php")
	_, _ = f.Write([]byte("x"))
	_ = zw.Close()
	if err := extractZipReplace(buf.Bytes(), pluginsDir, "evil"); err == nil {
		t.Error("expected error on path traversal")
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Errorf("pre-existing plugin file should NOT be deleted on rejected traversal: %v", err)
	}
}
