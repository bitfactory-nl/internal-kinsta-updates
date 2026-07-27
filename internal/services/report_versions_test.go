package services

import (
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/endoflife"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"php8.3", "8.3"},
		{"8.3-jit", "8.3"},
		{"24.16.0-bf3", "24.16.0"},
		{"v20.1", "20.1"},
		{"  22.17 ", "22.17"},
		{"${TAG_NODE}", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.in); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPHPFromDockerfile(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"bitfactory dev", "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.3-jit\nRUN true", "8.3"},
		{"bitfactory prod multi-stage", "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.3 AS composer\nFROM europe-docker.pkg.dev/bitfactory-nl/service-node/node:20.12.2 AS frontend", "8.3"},
		{"plain image", "FROM php:8.2-fpm-alpine", "8.2"},
		{"geen php", "FROM nginx:1.23", ""},
	}
	for _, tt := range tests {
		if got := phpFromDockerfile([]byte(tt.content)); got != tt.want {
			t.Errorf("%s: phpFromDockerfile = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNodeFromComposeAndDockerfile(t *testing.T) {
	compose := "services:\n  node:\n    image: europe-docker.pkg.dev/bitfactory-nl/service-node/node:24.16.0-bf3\n"
	if got := nodeFromCompose([]byte(compose)); got != "24.16.0" {
		t.Errorf("nodeFromCompose = %q, want 24.16.0", got)
	}
	variable := "services:\n  node:\n    image: europe-docker.pkg.dev/bitfactory-nl/service-node/node:${TAG_NODE}\n"
	if got := nodeFromCompose([]byte(variable)); got != "" {
		t.Errorf("nodeFromCompose(variabele) = %q, want \"\"", got)
	}
	dockerfile := "FROM europe-docker.pkg.dev/bitfactory-nl/service-node/node:20.12.2 AS frontend\n"
	if got := nodeFromDockerfile([]byte(dockerfile)); got != "20.12.2" {
		t.Errorf("nodeFromDockerfile = %q, want 20.12.2", got)
	}
	if got := nodeFromDockerfile(nil); got != "" {
		t.Errorf("nodeFromDockerfile(nil) = %q, want \"\"", got)
	}
}

// eolFlex bouwt een Flex uit een datumstring, of een bool bij "".
func eolFlex(t *testing.T, date string, b bool) endoflife.Flex {
	t.Helper()
	if date == "" {
		return endoflife.Flex{Bool: b}
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("eolFlex: %v", err)
	}
	return endoflife.Flex{IsDate: true, Date: d}
}

func TestLatestActive(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	php := []endoflife.Cycle{
		{Cycle: "8.5", Latest: "8.5.8", Support: eolFlex(t, "2027-12-31", false), EOL: eolFlex(t, "2029-12-31", false)},
		{Cycle: "8.3", Latest: "8.3.32", Support: eolFlex(t, "2025-12-31", false), EOL: eolFlex(t, "2027-12-31", false)},
	}
	node := []endoflife.Cycle{
		// 26 wordt pas in oktober 2026 LTS: mag nu nog niet gekozen worden.
		{Cycle: "26", Latest: "26.5.0", LTS: eolFlex(t, "2026-10-28", false), EOL: eolFlex(t, "2029-04-30", false)},
		{Cycle: "24", Latest: "24.18.0", LTS: eolFlex(t, "2025-10-28", false), EOL: eolFlex(t, "2028-04-30", false)},
		{Cycle: "25", Latest: "25.9.0", LTS: eolFlex(t, "", false), EOL: eolFlex(t, "2026-06-01", false)},
	}
	maria := []endoflife.Cycle{
		{Cycle: "12.3", Latest: "12.3.2", LTS: eolFlex(t, "", true), EOL: eolFlex(t, "2029-06-30", false)},
		{Cycle: "12.2", Latest: "12.2.2", LTS: eolFlex(t, "", false), EOL: eolFlex(t, "2026-05-13", false)},
	}
	wp := []endoflife.Cycle{
		{Cycle: "7.0", Latest: "7.0.2", EOL: eolFlex(t, "", false)}, // eol=false: nog geen EOL
		{Cycle: "6.9", Latest: "6.9.5", EOL: eolFlex(t, "2026-05-20", false)},
	}
	tests := []struct {
		product string
		cycles  []endoflife.Cycle
		want    string
	}{
		{"php", php, "8.5.8"},
		{"nodejs", node, "24.18.0"},
		{"mariadb", maria, "12.3.2"},
		{"wordpress", wp, "7.0.2"},
		{"php", nil, ""},
	}
	for _, tt := range tests {
		if got := latestActive(tt.product, tt.cycles, now); got != tt.want {
			t.Errorf("latestActive(%s) = %q, want %q", tt.product, got, tt.want)
		}
	}
}

func TestSupportedUntil(t *testing.T) {
	cycles := []endoflife.Cycle{
		{Cycle: "8.3", EOL: eolFlex(t, "2027-12-31", false)},
		{Cycle: "24", EOL: eolFlex(t, "2028-04-30", false)},
		{Cycle: "7.0", EOL: eolFlex(t, "", false)}, // bool false: geen datum bekend
	}
	tests := []struct{ current, want string }{
		{"8.3", "31-12-2027"},
		{"8.3.32", "31-12-2027"},
		{"24.16.0", "30-04-2028"},
		{"7.0.2", ""}, // eol=false → leeg laten
		{"5.6", ""},   // onbekende cycle
		{"", ""},
	}
	for _, tt := range tests {
		if got := supportedUntil(tt.current, cycles); got != tt.want {
			t.Errorf("supportedUntil(%q) = %q, want %q", tt.current, got, tt.want)
		}
	}
}
