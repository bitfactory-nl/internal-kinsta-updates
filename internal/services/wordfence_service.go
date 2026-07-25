// internal/services/wordfence_service.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/wordfence"
	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// wporgResolver is the subset of the wp.org client used here (test seam).
type wporgResolver interface {
	LatestVersion(ctx context.Context, slug string) (string, string, error)
}

type FeedMeta struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Count     int       `json:"count"`
}

// WordfenceVulnFinding is a single vulnerability match against an installed
// plugin, produced by WordfenceService.MatchProjects. Named distinctly from
// VulnFinding (vuln_scan_service.go, Kinsta-environment scan results) to
// avoid a package-level identifier collision.
type WordfenceVulnFinding struct {
	Slug             string `json:"slug"`
	InstalledVersion string `json:"installedVersion"`
	LatestVersion    string `json:"latestVersion"`
	Source           string `json:"source"` // "wporg" | "manual"
	CVE              string `json:"cve"`
	Severity         string `json:"severity"`
	Title            string `json:"title"`
	VulnID           string `json:"vulnId"`
}

type ProjectVulnReport struct {
	ProjectID   string                 `json:"projectId"`
	ProjectName string                 `json:"projectName"`
	Path        string                 `json:"path"`
	Findings    []WordfenceVulnFinding `json:"findings"`
	Skipped     bool                   `json:"skipped"`
	SkipReason  string                 `json:"skipReason"`
}

type WordfenceService struct {
	cfg      *config.Global
	projects *ProjectService
	wporg    wporgResolver

	mu   sync.RWMutex
	meta FeedMeta
}

func NewWordfenceService(cfg *config.Global, projects *ProjectService) *WordfenceService {
	s := &WordfenceService{cfg: cfg, projects: projects, wporg: wporg.NewClient()}
	if m, err := readFeedMeta(); err == nil {
		s.meta = m
	}
	return s
}

func feedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "wordfence-production.json")
}

func metaPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "wordfence-meta.json")
}

// Refresh downloads the feed, caches it, and records metadata.
func (s *WordfenceService) Refresh(ctx context.Context) (FeedMeta, error) {
	key, err := config.ResolveSecret(s.cfg.Wordfence.APIKey)
	if err != nil {
		return FeedMeta{}, fmt.Errorf("wordfence api key: %w", err)
	}
	if key == "" {
		return FeedMeta{}, fmt.Errorf("wordfence API-key niet geconfigureerd (Instellingen → Wordfence)")
	}
	data, err := wordfence.NewClient(key).Fetch(ctx)
	if err != nil {
		return FeedMeta{}, err
	}
	vulns, err := wordfence.ParseFeed(data)
	if err != nil {
		return FeedMeta{}, fmt.Errorf("wordfence feed parse: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(feedPath()), 0o700); err != nil {
		return FeedMeta{}, err
	}
	if err := os.WriteFile(feedPath(), data, 0o600); err != nil {
		return FeedMeta{}, err
	}
	meta := FeedMeta{FetchedAt: time.Now(), Count: len(vulns)}
	if b, err := json.Marshal(meta); err == nil {
		_ = os.WriteFile(metaPath(), b, 0o600)
	}
	s.mu.Lock()
	s.meta = meta
	s.mu.Unlock()
	return meta, nil
}

// List returns the cached, parsed feed.
func (s *WordfenceService) List() ([]domain.Vulnerability, error) {
	data, err := os.ReadFile(feedPath())
	if os.IsNotExist(err) {
		return []domain.Vulnerability{}, nil
	}
	if err != nil {
		return nil, err
	}
	return wordfence.ParseFeed(data)
}

func (s *WordfenceService) LastFetched() FeedMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta
}

func readFeedMeta() (FeedMeta, error) {
	data, err := os.ReadFile(metaPath())
	if err != nil {
		return FeedMeta{}, err
	}
	var m FeedMeta
	return m, json.Unmarshal(data, &m)
}

// MatchProjects cross-references installed plugins against the cached feed.
func (s *WordfenceService) MatchProjects() ([]ProjectVulnReport, error) {
	vulns, err := s.List()
	if err != nil {
		return nil, err
	}
	// Index affected plugin ranges by slug.
	type vref struct {
		sw   domain.AffectedSoftware
		vuln domain.Vulnerability
	}
	bySlug := map[string][]vref{}
	for _, v := range vulns {
		for _, sw := range v.Software {
			if sw.Type != "plugin" || sw.Slug == "" {
				continue
			}
			bySlug[sw.Slug] = append(bySlug[sw.Slug], vref{sw: sw, vuln: v})
		}
	}

	latestCache := map[string]string{} // slug -> latest version ("" = manual)
	var reports []ProjectVulnReport
	for _, p := range s.projects.List() {
		installed, err := wpplugins.ReadInstalled(p.Path)
		if err != nil {
			reports = append(reports, ProjectVulnReport{
				ProjectID:   p.ID,
				ProjectName: p.DisplayName,
				Path:        p.Path,
				Skipped:     true,
				SkipReason:  err.Error(),
			})
			continue
		}
		rep := ProjectVulnReport{ProjectID: p.ID, ProjectName: p.DisplayName, Path: p.Path}
		for _, ip := range installed {
			refs := bySlug[ip.Slug]
			if len(refs) == 0 || ip.Version == "" {
				continue
			}
			hit := false
			var chosen vref
			for _, r := range refs {
				if isVersionAffected(ip.Version, r.sw) {
					hit = true
					chosen = r
					break
				}
			}
			if !hit {
				continue
			}
			latest, ok := latestCache[ip.Slug]
			if !ok {
				v, _, err := s.wporg.LatestVersion(context.Background(), ip.Slug)
				if err != nil {
					v = "" // manual
				}
				latest = v
				latestCache[ip.Slug] = v
			}
			source := "wporg"
			if latest == "" {
				source = "manual"
			}
			rep.Findings = append(rep.Findings, WordfenceVulnFinding{
				Slug:             ip.Slug,
				InstalledVersion: ip.Version,
				LatestVersion:    latest,
				Source:           source,
				CVE:              chosen.vuln.CVE,
				Severity:         chosen.vuln.Severity,
				Title:            chosen.vuln.Title,
				VulnID:           chosen.vuln.ID,
			})
		}
		if len(rep.Findings) > 0 {
			reports = append(reports, rep)
		}
	}
	return reports, nil
}

// isVersionAffected reports whether v falls in the affected range of sw.
func isVersionAffected(v string, sw domain.AffectedSoftware) bool {
	// Lower bound.
	if sw.AffectedFrom != "" && sw.AffectedFrom != "*" {
		c := compareVersions(v, sw.AffectedFrom)
		if c < 0 || (c == 0 && !sw.FromInclusive) {
			return false
		}
	}
	// Upper bound.
	if sw.AffectedTo != "" && sw.AffectedTo != "*" {
		c := compareVersions(v, sw.AffectedTo)
		if c > 0 || (c == 0 && !sw.ToInclusive) {
			return false
		}
	}
	return true
}
