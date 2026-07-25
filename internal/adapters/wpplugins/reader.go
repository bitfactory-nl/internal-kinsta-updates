// internal/adapters/wpplugins/reader.go
package wpplugins

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// InstalledPlugin is one plugin directory under public/wp-content/plugins.
type InstalledPlugin struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
	Dir     string `json:"dir"`
}

var (
	headerVersionRe = regexp.MustCompile(`(?im)^[ \t/*]*Version:\s*(.+?)\s*$`)
	stableTagRe     = regexp.MustCompile(`(?im)^\s*Stable tag:\s*(.+?)\s*$`)
)

// ParseVersionHeader extracts a `Version:` header value from file contents,
// as found in plugin main files and theme style.css headers.
func ParseVersionHeader(data []byte) string {
	if m := headerVersionRe.FindSubmatch(data); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// ParseStableTag extracts the `Stable tag:` value from readme.txt contents.
func ParseStableTag(data []byte) string {
	if m := stableTagRe.FindSubmatch(data); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// PluginsDir is the fixed location of plugins within a project checkout.
func PluginsDir(projectPath string) string {
	return filepath.Join(projectPath, "public", "wp-content", "plugins")
}

// ReadInstalled scans public/wp-content/plugins/*/ and returns each plugin's
// slug and version. A missing plugins directory yields an empty slice.
func ReadInstalled(projectPath string) ([]InstalledPlugin, error) {
	base := PluginsDir(projectPath)
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []InstalledPlugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		ver := readPluginVersion(dir)
		out = append(out, InstalledPlugin{Slug: e.Name(), Version: ver, Dir: dir})
	}
	return out, nil
}

func readPluginVersion(dir string) string {
	// Prefer a Version: header from any top-level .php file.
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".php") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		if m := headerVersionRe.FindSubmatch(data); m != nil {
			return strings.TrimSpace(string(m[1]))
		}
	}
	// Fallback: Stable tag from readme.txt.
	if data, err := os.ReadFile(filepath.Join(dir, "readme.txt")); err == nil {
		if m := stableTagRe.FindSubmatch(data); m != nil {
			return strings.TrimSpace(string(m[1]))
		}
	}
	return ""
}
