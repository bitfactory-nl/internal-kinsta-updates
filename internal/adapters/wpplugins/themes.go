package wpplugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// InstalledTheme is one theme directory under public/wp-content/themes.
type InstalledTheme struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
	Dir     string `json:"dir"`
}

// ThemesDir is the fixed location of themes within a project checkout.
func ThemesDir(projectPath string) string {
	return filepath.Join(projectPath, "public", "wp-content", "themes")
}

// ReadThemes scans public/wp-content/themes/*/ and returns each theme's slug
// and version (from the Version: header in style.css). A missing themes
// directory yields an empty slice.
func ReadThemes(projectPath string) ([]InstalledTheme, error) {
	base := ThemesDir(projectPath)
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []InstalledTheme
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		ver := ""
		if data, err := os.ReadFile(filepath.Join(dir, "style.css")); err == nil {
			if m := headerVersionRe.FindSubmatch(data); m != nil {
				ver = strings.TrimSpace(string(m[1]))
			}
		}
		out = append(out, InstalledTheme{Slug: e.Name(), Version: ver, Dir: dir})
	}
	return out, nil
}

var wpVersionRe = regexp.MustCompile(`\$wp_version\s*=\s*'([^']+)'`)

// ReadWPVersion reads the WordPress core version from
// public/wp-includes/version.php. A project without that file is not a
// WordPress checkout; the error lets callers skip it.
func ReadWPVersion(projectPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectPath, "public", "wp-includes", "version.php"))
	if err != nil {
		return "", err
	}
	if m := wpVersionRe.FindSubmatch(data); m != nil {
		return string(m[1]), nil
	}
	return "", fmt.Errorf("wp_version niet gevonden in version.php")
}
