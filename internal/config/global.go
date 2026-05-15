package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const keychainPrefix = "keychain:"

func GlobalConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "config.yml")
}

func LoadGlobal() (Global, error) {
	path := GlobalConfigPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultGlobal(), nil
	}
	if err != nil {
		return Global{}, fmt.Errorf("read global config: %w", err)
	}

	var g Global
	if err := yaml.Unmarshal(data, &g); err != nil {
		return Global{}, fmt.Errorf("parse global config: %w", err)
	}
	applyDefaults(&g)
	return g, nil
}

func SaveGlobal(g Global) error {
	path := GlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(g)
	if err != nil {
		return fmt.Errorf("marshal global config: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// ResolveSecret resolves a keychain: reference to its actual value.
// For keychain: references it calls the macOS security CLI.
// For plain strings it returns as-is (dev/test only).
func ResolveSecret(value string) (string, error) {
	if !strings.HasPrefix(value, keychainPrefix) {
		return value, nil
	}
	key := strings.TrimPrefix(value, keychainPrefix)
	return keychainGet(key)
}

func defaultGlobal() Global {
	home, _ := os.UserHomeDir()
	return Global{
		ProjectsRoots: []string{filepath.Join(home, "Projects")},
		Editor:        "cursor",
		Notifications: Notifications{
			EnableVulnerabilityAlerts: true,
			ScanIntervalMinutes:       60,
		},
		Git: GitGlobal{
			DefaultRemote: "origin",
			PruneOnFetch:  true,
		},
		PluginRepo: PluginRepo{Ref: "main"},
	}
}

func applyDefaults(g *Global) {
	if g.Git.DefaultRemote == "" {
		g.Git.DefaultRemote = "origin"
	}
	if g.PluginRepo.Ref == "" {
		g.PluginRepo.Ref = "main"
	}
	if g.Notifications.ScanIntervalMinutes == 0 {
		g.Notifications.ScanIntervalMinutes = 60
	}
	if g.Editor == "" {
		g.Editor = "cursor"
	}
}
