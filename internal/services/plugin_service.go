package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// sshRunner is the subset of the SSH adapter PluginService needs; defined here
// so it can be faked in tests. *sshadapter.Client satisfies it.
type sshRunner interface {
	Upload(ctx context.Context, t sshadapter.Target, remotePath string, data []byte) error
	RunCommand(ctx context.Context, t sshadapter.Target, cmd string) (string, error)
}

// PluginService compares the paid-plugin repo manifest against the plugins
// installed on a Kinsta environment.
type PluginService struct {
	cfg      *config.Global
	kinsta   *KinstaService
	ssh      sshRunner
	projects *ProjectService
	git      *GitService

	// downloadZip is a test seam; when nil, the GitHub client is used.
	downloadZip func(ctx context.Context, path string) ([]byte, error)

	// pulls is een test seam voor het openen van pull requests; is hij nil,
	// dan wordt er een verse GitHub-client met het token uit de instellingen
	// gebouwd. Zie pullClient in plugin_local.go.
	pulls pluginPulls

	mu     sync.RWMutex
	cache  []domain.PaidPlugin
	cached bool
}

func NewPluginService(cfg *config.Global, kinsta *KinstaService, projects *ProjectService, git *GitService) *PluginService {
	return &PluginService{cfg: cfg, kinsta: kinsta, ssh: sshadapter.NewClient(), projects: projects, git: git}
}

func (s *PluginService) client() (*github.Client, error) {
	token, err := config.ResolveSecret(s.cfg.PluginRepo.GithubToken)
	if err != nil {
		return nil, fmt.Errorf("github token: %w", err)
	}
	if token == "" {
		return nil, fmt.Errorf("github token niet geconfigureerd")
	}
	if s.cfg.PluginRepo.Repo == "" {
		return nil, fmt.Errorf("plugin repo niet geconfigureerd")
	}
	return github.NewClient(token, s.cfg.PluginRepo.Repo, s.cfg.PluginRepo.Ref), nil
}

// IsConfigured reports whether the paid-plugin repo is set up.
func (s *PluginService) IsConfigured() bool {
	return s.cfg.PluginRepo.GithubToken != "" && s.cfg.PluginRepo.Repo != ""
}

// ListPaidPlugins returns the manifest, cached after the first fetch.
func (s *PluginService) ListPaidPlugins() ([]domain.PaidPlugin, error) {
	s.mu.RLock()
	if s.cached {
		defer s.mu.RUnlock()
		return s.cache, nil
	}
	s.mu.RUnlock()
	return s.fetchAndCache()
}

// RefreshIndex discards the cache and re-fetches the manifest.
func (s *PluginService) RefreshIndex() error {
	s.mu.Lock()
	s.cached = false
	s.cache = nil
	s.mu.Unlock()
	_, err := s.fetchAndCache()
	return err
}

func (s *PluginService) fetchAndCache() ([]domain.PaidPlugin, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	plugins, err := c.GetManifest(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache = plugins
	s.cached = true
	s.mu.Unlock()
	return plugins, nil
}

// Diff compares the plugins installed on a Kinsta environment against the
// paid-plugin manifest.
func (s *PluginService) Diff(envID string) ([]domain.PluginDiff, error) {
	paid, err := s.ListPaidPlugins()
	if err != nil {
		return nil, err
	}
	details, err := s.kinsta.GetEnvironmentPluginsAndThemes(envID)
	if err != nil {
		return nil, err
	}
	return diffPlugins(details.Plugins, paid), nil
}

// diffPlugins is the pure comparison logic: for each installed plugin, classify
// its status relative to the paid-plugin manifest.
func diffPlugins(installed []kinsta.Plugin, paid []domain.PaidPlugin) []domain.PluginDiff {
	repo := make(map[string]domain.PaidPlugin, len(paid))
	for _, p := range paid {
		repo[p.Slug] = p
	}

	out := make([]domain.PluginDiff, 0, len(installed))
	for _, ip := range installed {
		d := domain.PluginDiff{
			Slug:             ip.Name,
			InstalledVersion: ip.Version,
			IsVulnerable:     ip.IsVersionVulnerable,
		}
		if paidP, ok := repo[ip.Name]; ok {
			d.Source = domain.SourcePrivateRepo
			d.AvailableVersion = paidP.LatestVersion
			switch {
			case ip.IsVersionVulnerable:
				d.Status = domain.DiffVulnerable
			case paidP.LatestVersion != "" && paidP.LatestVersion != ip.Version:
				d.Status = domain.DiffUpdate
			default:
				d.Status = domain.DiffUpToDate
			}
		} else {
			d.Source = domain.SourceWPOrg
			d.AvailableVersion = ip.UpdateVersion
			if ip.IsVersionVulnerable {
				d.Status = domain.DiffVulnerable
			} else {
				d.Status = domain.DiffNotFound
			}
		}
		out = append(out, d)
	}
	return out
}

// UpdateViaSSH downloads the paid plugin's zip from the private repo, uploads it
// to the target host, and installs it via wp-cli (--force overwrites the current
// version). The uploaded temp file is removed afterwards. Returns wp-cli stdout.
func (s *PluginService) UpdateViaSSH(target domain.SSHTarget, slug string) (string, error) {
	paid, err := s.ListPaidPlugins()
	if err != nil {
		return "", err
	}

	var entry *domain.PaidPlugin
	for i := range paid {
		if paid[i].Slug == slug {
			entry = &paid[i]
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("plugin %q niet in manifest", slug)
	}
	if entry.ZipPath == "" {
		return "", fmt.Errorf("plugin %q heeft geen zip_path in manifest", slug)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	zip, err := s.fetchZip(ctx, entry.ZipPath)
	if err != nil {
		return "", err
	}

	tgt := sshadapter.Target{Host: target.Host, Port: target.Port, User: target.User}
	remote := fmt.Sprintf("/tmp/rdm-%s-%d.zip", slug, time.Now().UnixNano())
	if err := s.ssh.Upload(ctx, tgt, remote, zip); err != nil {
		return "", err
	}
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_, _ = s.ssh.RunCommand(cctx, tgt, fmt.Sprintf("rm -f '%s'", remote))
	}()

	install := fmt.Sprintf("wp plugin install '%s' --force", remote)
	if p := strings.TrimSpace(target.Path); p != "" {
		install += fmt.Sprintf(" --path='%s'", p)
	}
	return s.ssh.RunCommand(ctx, tgt, install)
}

func (s *PluginService) fetchZip(ctx context.Context, path string) ([]byte, error) {
	if s.downloadZip != nil {
		return s.downloadZip(ctx, path)
	}
	gh, err := s.client()
	if err != nil {
		return nil, err
	}
	return gh.DownloadZip(ctx, path)
}
