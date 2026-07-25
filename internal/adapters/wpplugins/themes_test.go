package wpplugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadThemes(t *testing.T) {
	root := t.TempDir()
	tdir := filepath.Join(root, "public", "wp-content", "themes", "twentytwentyfour")
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		t.Fatal(err)
	}
	style := "/*\nTheme Name: Twenty Twenty-Four\nVersion: 1.1\n*/\n"
	if err := os.WriteFile(filepath.Join(tdir, "style.css"), []byte(style), 0o644); err != nil {
		t.Fatal(err)
	}

	// theme without a style.css version
	ndir := filepath.Join(root, "public", "wp-content", "themes", "custom-child")
	if err := os.MkdirAll(ndir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ReadThemes(root)
	if err != nil {
		t.Fatalf("ReadThemes: %v", err)
	}
	versions := map[string]string{}
	for _, th := range got {
		versions[th.Slug] = th.Version
	}
	if versions["twentytwentyfour"] != "1.1" {
		t.Errorf("twentytwentyfour version = %q", versions["twentytwentyfour"])
	}
	if v, ok := versions["custom-child"]; !ok || v != "" {
		t.Errorf("custom-child = %q, %v", v, ok)
	}
}

func TestReadThemesNoDir(t *testing.T) {
	got, err := ReadThemes(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Errorf("want empty, no error; got %v, %v", got, err)
	}
}

func TestReadWPVersion(t *testing.T) {
	root := t.TempDir()
	inc := filepath.Join(root, "public", "wp-includes")
	if err := os.MkdirAll(inc, 0o755); err != nil {
		t.Fatal(err)
	}
	php := "<?php\n$wp_version = '6.5.2';\n"
	if err := os.WriteFile(filepath.Join(inc, "version.php"), []byte(php), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := ReadWPVersion(root)
	if err != nil {
		t.Fatalf("ReadWPVersion: %v", err)
	}
	if v != "6.5.2" {
		t.Errorf("version = %q", v)
	}

	if _, err := ReadWPVersion(t.TempDir()); err == nil {
		t.Error("expected error for non-WP project")
	}
}
