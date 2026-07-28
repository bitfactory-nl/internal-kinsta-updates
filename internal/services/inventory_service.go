package services

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
)

// workingTreeRef labels entries read from the working directory because the
// project has no usable git default branch.
const workingTreeRef = "werkmap"

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
	// Ref is the git ref the version was read from (e.g. origin/release/1.0.x),
	// or "werkmap" when the working tree was used as fallback.
	Ref string `json:"ref"`
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
// installed versions (read from the project's default branch) and the latest
// wp.org version.
func (s *InventoryService) Plugins() ([]InventoryItem, error) {
	items := s.collect(s.readPlugins)
	s.resolveLatest(items, "plugin", func(ctx context.Context, slug string) (string, error) {
		v, _, err := s.wporg.LatestVersion(ctx, slug)
		return v, err
	})
	return finishItems(items), nil
}

// Themes returns every theme found in any project, with per-project
// installed versions (read from the project's default branch) and the latest
// wp.org version.
func (s *InventoryService) Themes() ([]InventoryItem, error) {
	items := s.collect(s.readThemes)
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
		ref := s.projectRef(p.Path)
		var v string
		if ref != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			data, err := gitcli.ShowFile(ctx, p.Path, ref, "public/wp-includes/version.php")
			cancel()
			if err == nil {
				v = wpplugins.ParseWPVersion(data)
			}
		} else {
			ref = workingTreeRef
			v, _ = wpplugins.ReadWPVersion(p.Path)
		}
		if v == "" {
			continue // not a WordPress project (on this ref)
		}
		report.Projects = append(report.Projects, InventoryProjectRef{
			ProjectID:   p.ID,
			ProjectName: p.DisplayName,
			Version:     v,
			Outdated:    latest != "" && compareVersions(v, latest) < 0,
			Ref:         ref,
		})
	}
	sort.Slice(report.Projects, func(i, j int) bool {
		return strings.ToLower(report.Projects[i].ProjectName) < strings.ToLower(report.Projects[j].ProjectName)
	})
	return report, nil
}

// FetchAllResult summarizes a git fetch across all project repositories.
type FetchAllResult struct {
	Fetched int      `json:"fetched"`
	Errors  []string `json:"errors"`
}

// fetchWorkers bounds concurrent git fetches (network + ssh agent load).
const fetchWorkers = 4

// FetchAll runs git fetch in every project repository so the
// origin/<default-branch> refs reflect the current remote state.
func (s *InventoryService) FetchAll() (FetchAllResult, error) {
	res := FetchAllResult{Errors: []string{}}
	sem := make(chan struct{}, fetchWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, p := range s.projects.List() {
		wg.Add(1)
		sem <- struct{}{}
		go func(id, name, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if !gitcli.RefExists(ctx, path, "HEAD") {
				return // not a git repo (or empty): nothing to fetch
			}
			if err := gitcli.Fetch(ctx, path); err != nil {
				mu.Lock()
				res.Errors = append(res.Errors, name+": "+err.Error())
				mu.Unlock()
				return
			}
			mu.Lock()
			res.Fetched++
			mu.Unlock()
		}(p.ID, p.DisplayName, p.Path)
	}
	wg.Wait()
	sort.Strings(res.Errors)
	return res, nil
}

type installedRef struct {
	slug    string
	version string
}

// projectRef resolves the git ref to read a project's files from: the
// remote-tracking default branch (origin/release/…) when available, else the
// local default branch. An empty result means "use the working tree".
func (s *InventoryService) projectRef(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return projectFileRef(ctx, path)
}

// collect walks all projects and groups installed items by slug. Items are
// read from each project's default branch; projects without a usable git ref
// fall back to the working tree.
func (s *InventoryService) collect(read func(path, gitRef string) []installedRef) map[string]*InventoryItem {
	items := make(map[string]*InventoryItem)
	for _, p := range s.projects.List() {
		gitRef := s.projectRef(p.Path)
		label := gitRef
		if label == "" {
			label = workingTreeRef
		}
		for _, ref := range read(p.Path, gitRef) {
			it, ok := items[ref.slug]
			if !ok {
				it = &InventoryItem{Slug: ref.slug}
				items[ref.slug] = it
			}
			it.Projects = append(it.Projects, InventoryProjectRef{
				ProjectID:   p.ID,
				ProjectName: p.DisplayName,
				Version:     ref.version,
				Ref:         label,
			})
		}
	}
	return items
}

// readPlugins lists plugins with versions at gitRef ("" = working tree).
func (s *InventoryService) readPlugins(path, gitRef string) []installedRef {
	if gitRef == "" {
		installed, err := wpplugins.ReadInstalled(path)
		if err != nil {
			return nil
		}
		refs := make([]installedRef, 0, len(installed))
		for _, ip := range installed {
			refs = append(refs, installedRef{slug: ip.Slug, version: ip.Version})
		}
		return refs
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slugs, err := gitcli.LsTreeDirs(ctx, path, gitRef, "public/wp-content/plugins")
	if err != nil || len(slugs) == 0 {
		return nil
	}

	// One grep for all plugin main-file Version: headers, one for readme
	// Stable tags — instead of a git show per file.
	versions := map[string]string{}
	lines, _ := gitcli.GrepTree(ctx, path, gitRef, `^[[:space:]/*]*Version:[[:space:]]*`,
		":(glob)public/wp-content/plugins/*/*.php")
	for _, l := range lines {
		p, match, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		slug := pathSegment(p, 3)
		if slug == "" || versions[slug] != "" {
			continue
		}
		if v := wpplugins.ParseVersionHeader([]byte(match)); v != "" {
			versions[slug] = v
		}
	}
	stable, _ := gitcli.GrepTree(ctx, path, gitRef, `^[[:space:]]*Stable tag:[[:space:]]*`,
		":(glob)public/wp-content/plugins/*/readme.txt")
	for _, l := range stable {
		p, match, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		slug := pathSegment(p, 3)
		if slug == "" || versions[slug] != "" {
			continue
		}
		if v := wpplugins.ParseStableTag([]byte(match)); v != "" {
			versions[slug] = v
		}
	}

	refs := make([]installedRef, 0, len(slugs))
	for _, slug := range slugs {
		refs = append(refs, installedRef{slug: slug, version: versions[slug]})
	}
	return refs
}

// readThemes lists themes with versions at gitRef ("" = working tree).
func (s *InventoryService) readThemes(path, gitRef string) []installedRef {
	if gitRef == "" {
		installed, err := wpplugins.ReadThemes(path)
		if err != nil {
			return nil
		}
		refs := make([]installedRef, 0, len(installed))
		for _, th := range installed {
			refs = append(refs, installedRef{slug: th.Slug, version: th.Version})
		}
		return refs
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slugs, err := gitcli.LsTreeDirs(ctx, path, gitRef, "public/wp-content/themes")
	if err != nil || len(slugs) == 0 {
		return nil
	}

	versions := map[string]string{}
	lines, _ := gitcli.GrepTree(ctx, path, gitRef, `^[[:space:]/*]*Version:[[:space:]]*`,
		":(glob)public/wp-content/themes/*/style.css")
	for _, l := range lines {
		p, match, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		slug := pathSegment(p, 3)
		if slug == "" || versions[slug] != "" {
			continue
		}
		if v := wpplugins.ParseVersionHeader([]byte(match)); v != "" {
			versions[slug] = v
		}
	}

	refs := make([]installedRef, 0, len(slugs))
	for _, slug := range slugs {
		refs = append(refs, installedRef{slug: slug, version: versions[slug]})
	}
	return refs
}

// pathSegment returns the n-th (0-based) segment of a slash-separated path.
func pathSegment(p string, n int) string {
	parts := strings.Split(p, "/")
	if n < len(parts) {
		return parts[n]
	}
	return ""
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
