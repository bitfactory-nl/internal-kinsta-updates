package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	"github.com/rdm/sites-tool/internal/domain"
)

func TestDiffPlugins(t *testing.T) {
	paid := []domain.PaidPlugin{
		{Slug: "acme-pro", LatestVersion: "2.0.0"},
		{Slug: "beta-forms", LatestVersion: "1.0.0"},
		{Slug: "vuln-plugin", LatestVersion: "3.0.0"},
	}

	installed := []kinsta.Plugin{
		{Name: "acme-pro", Version: "1.5.0"},                               // paid, outdated
		{Name: "beta-forms", Version: "1.0.0"},                             // paid, up to date
		{Name: "vuln-plugin", Version: "2.0.0", IsVersionVulnerable: true}, // paid, vulnerable
		{Name: "akismet", Version: "5.3", UpdateVersion: "5.4"},            // wp.org, not in repo
		{Name: "old-free", Version: "1.0", IsVersionVulnerable: true},      // wp.org, vulnerable
	}

	got := diffPlugins(installed, paid)
	if len(got) != len(installed) {
		t.Fatalf("got %d diffs, want %d", len(got), len(installed))
	}

	byslug := make(map[string]domain.PluginDiff, len(got))
	for _, d := range got {
		byslug[d.Slug] = d
	}

	tests := []struct {
		slug      string
		status    domain.DiffStatus
		source    domain.PluginSource
		available string
		vuln      bool
	}{
		{"acme-pro", domain.DiffUpdate, domain.SourcePrivateRepo, "2.0.0", false},
		{"beta-forms", domain.DiffUpToDate, domain.SourcePrivateRepo, "1.0.0", false},
		{"vuln-plugin", domain.DiffVulnerable, domain.SourcePrivateRepo, "3.0.0", true},
		{"akismet", domain.DiffNotFound, domain.SourceWPOrg, "5.4", false},
		{"old-free", domain.DiffVulnerable, domain.SourceWPOrg, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			d, ok := byslug[tt.slug]
			if !ok {
				t.Fatalf("missing diff for %s", tt.slug)
			}
			if d.Status != tt.status {
				t.Errorf("status = %q, want %q", d.Status, tt.status)
			}
			if d.Source != tt.source {
				t.Errorf("source = %q, want %q", d.Source, tt.source)
			}
			if d.AvailableVersion != tt.available {
				t.Errorf("available = %q, want %q", d.AvailableVersion, tt.available)
			}
			if d.IsVulnerable != tt.vuln {
				t.Errorf("vuln = %v, want %v", d.IsVulnerable, tt.vuln)
			}
		})
	}
}

func TestDiffPluginsEmpty(t *testing.T) {
	if got := diffPlugins(nil, nil); len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}
