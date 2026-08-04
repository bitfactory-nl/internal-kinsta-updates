package config

import (
	"os"
	"path/filepath"
	"strings"
)

const dotenvFile = ".env"

// ParseDotenv parses simple KEY=VALUE lines as found in a project's .env file.
// Blank lines and lines starting with '#' (after trimming) are ignored, as are
// lines without an '='. Keys and values are trimmed of surrounding whitespace;
// no quote handling is applied since real .env files in this codebase don't use
// quotes.
func ParseDotenv(data []byte) map[string]string {
	env := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		env[key] = value
	}
	return env
}

// LoadProjectEnv reads the project's local .env file from repoPath.
// Returns an empty map (not an error) when the file is absent: a project
// that hasn't been set up locally yet is a normal state, not a failure.
func LoadProjectEnv(repoPath string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, dotenvFile))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseDotenv(data), nil
}
