package wpplugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadInstalled(t *testing.T) {
	root := t.TempDir()
	pdir := filepath.Join(root, "public", "wp-content", "plugins", "contact-form-7")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	main := "<?php\n/*\nPlugin Name: Contact Form 7\nVersion: 5.9.2\n*/\n"
	if err := os.WriteFile(filepath.Join(pdir, "wp-contact-form-7.php"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	// readme-only plugin (Stable tag fallback)
	rdir := filepath.Join(root, "public", "wp-content", "plugins", "akismet")
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "akismet.php"), []byte("<?php\n// no header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "readme.txt"), []byte("=== Akismet ===\nStable tag: 5.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadInstalled(root)
	if err != nil {
		t.Fatalf("ReadInstalled: %v", err)
	}
	versions := map[string]string{}
	for _, p := range got {
		versions[p.Slug] = p.Version
	}
	if versions["contact-form-7"] != "5.9.2" {
		t.Errorf("cf7 version = %q", versions["contact-form-7"])
	}
	if versions["akismet"] != "5.3" {
		t.Errorf("akismet version = %q", versions["akismet"])
	}
}

func TestReadInstalledNoDir(t *testing.T) {
	got, err := ReadInstalled(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}
