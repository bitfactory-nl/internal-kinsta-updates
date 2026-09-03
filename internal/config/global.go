package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// KeychainPrefix marks a config value as a reference to a keychain entry rather
// than a literal secret.
const KeychainPrefix = "keychain:"

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
	if !strings.HasPrefix(value, KeychainPrefix) {
		return value, nil
	}
	key := strings.TrimPrefix(value, KeychainPrefix)
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
		Updates:    UpdatesGlobal{AutoCheck: boolPtr(true)},
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
	if g.Updates.AutoCheck == nil {
		g.Updates.AutoCheck = boolPtr(true)
	}
}

// boolPtr is een hulpje voor optionele yaml-booleans, waar nil "niet ingevuld"
// betekent en dus iets anders is dan false.
func boolPtr(b bool) *bool { return &b }
