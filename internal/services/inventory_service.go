package services

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/adapters/wporg"
)

// inventoryCacheTTL bounds how often wp.org is asked for the same slug.
const inventoryCacheTTL = 6 * time.Hour

// inventoryLookupWorkers bounds concurrent wp.org lookups.
const inventoryLookupWorkers = 8

// inventoryResolver is the subset of the wp.org client used here (test seam).
type inventoryResolver interface {
	LatestVersion(ctx context.Context, slug string) (string, string, error)
	LatestThemeVersion(ctx context.Context, slug string) (string, error)
	LatestCoreVersion(ctx context.Context) (string, error)
}

// InventoryProjectRef is one project's installed version of an item.
type InventoryProjectRef struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	Version     string `json:"version"`
	Outdated    bool   `json:"outdated"`
}

// InventoryItem is one plugin or theme aggregated across all projects.
type InventoryItem struct {
	Slug          string                `json:"slug"`
	LatestVersion string                `json:"latestVersion"`
	Source        string                `json:"source"` // wporg | manual
	OutdatedCount int                   `json:"outdatedCount"`
	Projects      []InventoryProjectRef `json:"projects"`
}

// WPCoreReport lists each project's WordPress core version.
type WPCoreReport struct {
	LatestVersion string                `json:"latestVersion"`
	Projects      []InventoryProjectRef `json:"projects"`
}

type cachedVersion struct {
	version string
	at      time.Time
}

// InventoryService aggregates plugins, themes, and WP core versions across
// all scanned projects and annotates them with the latest wp.org version.
type InventoryService struct {
	projects projectLister
	wporg    inventoryResolver

	mu    sync.Mutex
	cache map[string]cachedVersion // "plugin:slug" | "theme:slug" | "core"
}

func NewInventoryService(projects *ProjectService) *InventoryService {
	return &InventoryService{
		projects: projects,
		wporg:    wporg.NewClient(),
		cache:    make(map[string]cachedVersion),
	}
}

// Plugins returns every plugin found in any project, with per-project
// installed versions and the latest wp.org version.
func (s *InventoryService) Plugins() ([]InventoryItem, error) {
	items := s.collect(func(path string) []installedRef {
		installed, err := wpplugins.ReadInstalled(path)
		if err != nil {
			return nil
		}
		refs := make([]installedRef, 0, len(installed))
		for _, ip := range installed {
			refs = append(refs, installedRef{slug: ip.Slug, version: ip.Version})
		}
		return refs
	})
	s.resolveLatest(items, "plugin", func(ctx context.Context, slug string) (string, error) {
		v, _, err := s.wporg.LatestVersion(ctx, slug)
		return v, err
	})
	return finishItems(items), nil
}

// Themes returns every theme found in any project, with per-project
// installed versions and the latest wp.org version.
func (s *InventoryService) Themes() ([]InventoryItem, error) {
	items := s.collect(func(path string) []installedRef {
		installed, err := wpplugins.ReadThemes(path)
		if err != nil {
			return nil
		}
		refs := make([]installedRef, 0, len(installed))
		for _, th := range installed {
			refs = append(refs, installedRef{slug: th.Slug, version: th.Version})
		}
		return refs
	})
	s.resolveLatest(items, "theme", s.wporg.LatestThemeVersion)
	return finishItems(items), nil
}

// WordPress returns each project's WP core version plus the latest release.
func (s *InventoryService) WordPress() (WPCoreReport, error) {
	report := WPCoreReport{Projects: []InventoryProjectRef{}}

	latest, ok := s.cached("core")
	if !ok {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		v, err := s.wporg.LatestCoreVersion(ctx)
		if err == nil {
			latest = v
			s.store("core", v)
		}
	}
	report.LatestVersion = latest

	for _, p := range s.projects.List() {
		v, err := wpplugins.ReadWPVersion(p.Path)
		if err != nil {
			continue // not a WordPress project
		}
		report.Projects = append(report.Projects, InventoryProjectRef{
			ProjectID:   p.ID,
			ProjectName: p.DisplayName,
			Version:     v,
			Outdated:    latest != "" && compareVersions(v, latest) < 0,
		})
	}
	sort.Slice(report.Projects, func(i, j int) bool {
		return strings.ToLower(report.Projects[i].ProjectName) < strings.ToLower(report.Projects[j].ProjectName)
	})
	return report, nil
}

type installedRef struct {
	slug    string
	version string
}

// collect walks all projects and groups installed items by slug.
func (s *InventoryService) collect(read func(path string) []installedRef) map[string]*InventoryItem {
	items := make(map[string]*InventoryItem)
	for _, p := range s.projects.List() {
		for _, ref := range read(p.Path) {
			it, ok := items[ref.slug]
			if !ok {
				it = &InventoryItem{Slug: ref.slug}
				items[ref.slug] = it
			}
			it.Projects = append(it.Projects, InventoryProjectRef{
				ProjectID:   p.ID,
				ProjectName: p.DisplayName,
				Version:     ref.version,
			})
		}
	}
	return items
}

// resolveLatest fills LatestVersion/Source for every item, using the cache
// and a bounded worker pool for wp.org lookups.
func (s *InventoryService) resolveLatest(items map[string]*InventoryItem, kind string, lookup func(ctx context.Context, slug string) (string, error)) {
	var missing []string
	for slug := range items {
		if v, ok := s.cached(kind + ":" + slug); ok {
			items[slug].LatestVersion = v
			continue
		}
		missing = append(missing, slug)
	}
	if len(missing) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sem := make(chan struct{}, inventoryLookupWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, slug := range missing {
		wg.Add(1)
		sem <- struct{}{}
		go func(slug string) {
			defer wg.Done()
			defer func() { <-sem }()
			v, err := lookup(ctx, slug)
			if err != nil {
				v = "" // niet op wp.org (of tijdelijk onbereikbaar): geen latest bekend
			}
			s.store(kind+":"+slug, v)
			mu.Lock()
			items[slug].LatestVersion = v
			mu.Unlock()
		}(slug)
	}
	wg.Wait()
}

// finishItems derives Source/Outdated flags and returns a sorted slice.
func finishItems(items map[string]*InventoryItem) []InventoryItem {
	out := make([]InventoryItem, 0, len(items))
	for _, it := range items {
		if it.LatestVersion == "" {
			it.Source = "manual"
		} else {
			it.Source = "wporg"
		}
		sort.Slice(it.Projects, func(i, j int) bool {
			return strings.ToLower(it.Projects[i].ProjectName) < strings.ToLower(it.Projects[j].ProjectName)
		})
		for i := range it.Projects {
			p := &it.Projects[i]
			p.Outdated = it.LatestVersion != "" && p.Version != "" && compareVersions(p.Version, it.LatestVersion) < 0
			if p.Outdated {
				it.OutdatedCount++
			}
		}
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

func (s *InventoryService) cached(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[key]
	if !ok || time.Since(c.at) > inventoryCacheTTL {
		return "", false
	}
	return c.version, true
}

func (s *InventoryService) store(key, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = cachedVersion{version: version, at: time.Now()}
}
