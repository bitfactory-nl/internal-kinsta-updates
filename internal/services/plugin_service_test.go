package services

import (
	"context"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/domain"
)

type fakeSSH struct {
	uploadPath string
	uploadData []byte
	cmds       []string
}

func (f *fakeSSH) Upload(_ context.Context, _ sshadapter.Target, remotePath string, data []byte) error {
	f.uploadPath = remotePath
	f.uploadData = append([]byte(nil), data...)
	return nil
}

func (f *fakeSSH) RunCommand(_ context.Context, _ sshadapter.Target, cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	return "Plugin installed.", nil
}

func TestUpdateViaSSH(t *testing.T) {
	f := &fakeSSH{}
	s := &PluginService{
		ssh:    f,
		cache:  []domain.PaidPlugin{{Slug: "acme-pro", LatestVersion: "2.0.0", ZipPath: "plugins/acme-pro.zip"}},
		cached: true,
		downloadZip: func(_ context.Context, path string) ([]byte, error) {
			if path != "plugins/acme-pro.zip" {
				t.Fatalf("downloadZip path = %q, want plugins/acme-pro.zip", path)
			}
			return []byte("ZIPDATA"), nil
		},
	}

	out, err := s.UpdateViaSSH(domain.SSHTarget{Host: "h", Port: 22, User: "u", Path: "/www/site"}, "acme-pro")
	if err != nil {
		t.Fatalf("UpdateViaSSH: %v", err)
	}
	if out != "Plugin installed." {
		t.Errorf("out = %q, want %q", out, "Plugin installed.")
	}
	if string(f.uploadData) != "ZIPDATA" {
		t.Errorf("uploaded %q, want ZIPDATA", f.uploadData)
	}
	if !strings.HasPrefix(f.uploadPath, "/tmp/rdm-acme-pro-") || !strings.HasSuffix(f.uploadPath, ".zip") {
		t.Errorf("upload path = %q, want /tmp/rdm-acme-pro-*.zip", f.uploadPath)
	}

	var sawInstall, sawCleanup bool
	for _, c := range f.cmds {
		if strings.Contains(c, "wp plugin install") && strings.Contains(c, "--force") && strings.Contains(c, "--path='/www/site'") {
			sawInstall = true
		}
		if strings.HasPrefix(c, "rm -f '/tmp/rdm-acme-pro-") {
			sawCleanup = true
		}
	}
	if !sawInstall {
		t.Errorf("install command missing; cmds=%v", f.cmds)
	}
	if !sawCleanup {
		t.Errorf("cleanup command missing; cmds=%v", f.cmds)
	}
}

func TestUpdateViaSSHUnknownPlugin(t *testing.T) {
	s := &PluginService{ssh: &fakeSSH{}, cache: []domain.PaidPlugin{}, cached: true}
	if _, err := s.UpdateViaSSH(domain.SSHTarget{Host: "h"}, "nope"); err == nil {
		t.Fatal("expected error for plugin not in manifest")
	}
}

func TestUpdateViaSSHMissingZipPath(t *testing.T) {
	s := &PluginService{
		ssh:    &fakeSSH{},
		cache:  []domain.PaidPlugin{{Slug: "acme-pro", LatestVersion: "2.0.0"}},
		cached: true,
	}
	if _, err := s.UpdateViaSSH(domain.SSHTarget{Host: "h"}, "acme-pro"); err == nil {
		t.Fatal("expected error for missing zip_path")
	}
}

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
