package services

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/wporg"
	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
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
	// LocalVersion is de versie in de werkmap van de gebruiker.
	LocalVersion string `json:"localVersion"`
	// GithubVersion is de versie op de default release-branch van GitHub
	// (gelezen van origin/<branch>, die vooraf wordt bijgewerkt).
	GithubVersion string `json:"githubVersion"`
	// Outdated vergelijkt de GitHub-kolom met de laatste versie: dat is de
	// stand die naar productie gaat.
	Outdated bool `json:"outdated"`
	// LocalBehind is true als de werkmap achterloopt op GitHub — een hint om
	// te pullen, niet hetzelfde als verouderd.
	LocalBehind bool `json:"localBehind"`
	// Ref is the git ref the GitHub version was read from (e.g.
	// origin/release/1.0.x), or "werkmap" when the working tree was the only
	// available source.
	Ref string `json:"ref"`
}

// buildProjectRef stelt één rij samen uit de lokale en de GitHub-versie.
// Outdated volgt bewust de GitHub-kolom (dat is wat naar productie gaat);
// LocalBehind markeert alleen dat de checkout van de gebruiker achterloopt.
func buildProjectRef(projectID, projectName, local, github, latest, ref string) InventoryProjectRef {
	// Zonder GitHub-kolom (geen git-repo of geen origin-ref) valt de
	// verouderd-bepaling terug op de lokale versie, zodat een project niet
	// stil uit de telling verdwijnt.
	effectief := github
	if effectief == "" {
		effectief = local
	}
	return InventoryProjectRef{
		ProjectID:     projectID,
		ProjectName:   projectName,
		LocalVersion:  local,
		GithubVersion: github,
		Outdated:      latest != "" && effectief != "" && compareVersions(effectief, latest) < 0,
		LocalBehind:   local != "" && github != "" && compareVersions(local, github) < 0,
		Ref:           ref,
	}
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
	cfg      *config.Global
	// syncer houdt de origin-refs gelijk aan GitHub; nil = geen sync (tests).
	syncer *inventorySyncer

	mu    sync.Mutex
	cache map[string]cachedVersion // "plugin:slug" | "theme:slug" | "core"
}

func NewInventoryService(projects *ProjectService, cfg *config.Global) *InventoryService {
	return &InventoryService{
		projects: projects,
		wporg:    wporg.NewClient(),
		cfg:      cfg,
		syncer:   newInventorySyncer(gitSyncSource{cfg: cfg}),
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
	fromReference := s.applyReferenceLatest(items)
	return finishItems(items, fromReference), nil
}

// Themes returns every theme found in any project, with per-project
// installed versions (read from the project's default branch) and the latest
// wp.org version.
func (s *InventoryService) Themes() ([]InventoryItem, error) {
	items := s.collect(s.readThemes)
	s.resolveLatest(items, "theme", s.wporg.LatestThemeVersion)
	return finishItems(items, nil), nil
}

// referencePath is de ingestelde referentie-installatie, of "" als er geen is
// (ook als cfg zelf nil is — dat gebeurt in tests die de service met een
// struct-literal opbouwen zonder configuratie).
func (s *InventoryService) referencePath() string {
	if s.cfg == nil {
		return ""
	}
	return strings.TrimSpace(s.cfg.PluginRepo.ReferenceProjectPath)
}

// applyReferenceLatest laat de referentie-installatie de "laatste versie"
// bepalen voor elke geïnstalleerde plugin die daarin staat — ook als wp.org
// toevallig óók een versie kent. De referentie-installatie is voor betaalde
// plugins de enige echte bron; voor een plugin die ook op wp.org staat geldt
// hetzelfde: als de referentie 'm heeft, is dát de versie die naar klantsites
// hoort te gaan, dus die wint. Geeft de slugs terug die hierdoor zijn bepaald,
// zodat finishItems ze als "reference" kan labelen in plaats van "wporg".
//
// Een plugin die alleen in de referentie-installatie staat maar in geen enkel
// gescand project geïnstalleerd is, komt hier niet als nieuwe rij bij: dit
// overzicht toont wat er gebruikt wordt, niet wat er beschikbaar is.
func (s *InventoryService) applyReferenceLatest(items map[string]*InventoryItem) map[string]bool {
	touched := map[string]bool{}
	path := s.referencePath()
	if path == "" {
		return touched
	}
	installed, err := wpplugins.ReadInstalled(path)
	if err != nil {
		return touched // de rest van het overzicht mag hier niet op stuklopen
	}
	for _, ip := range installed {
		it, ok := items[ip.Slug]
		if !ok || ip.Version == "" {
			continue
		}
		it.LatestVersion = ip.Version
		touched[ip.Slug] = true
	}
	return touched
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

	projects := s.projects.List()
	s.syncGithubRefs(projects)

	for _, p := range projects {
		// Lokale kolom: de werkmap van de gebruiker.
		lokaal, _ := wpplugins.ReadWPVersion(p.Path)

		// GitHub-kolom: origin/<default branch>, net bijgewerkt door de sync.
		ref := s.projectRef(p.Path)
		var github string
		if ref != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			data, err := gitcli.ShowFile(ctx, p.Path, ref, "public/wp-includes/version.php")
			cancel()
			if err == nil {
				github = wpplugins.ParseWPVersion(data)
			}
		} else {
			ref = workingTreeRef
		}
		if lokaal == "" && github == "" {
			continue // geen WordPress-project
		}
		report.Projects = append(report.Projects,
			buildProjectRef(p.ID, p.DisplayName, lokaal, github, latest, ref))
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
	// De lokale refs zijn nu actueel; een direct volgend overzicht hoeft niet
	// opnieuw bij de GitHub API langs.
	if s.syncer != nil {
		s.syncer.MarkAllChecked(s.projects.List())
	}
	sort.Strings(res.Errors)
	return res, nil
}

type installedRef struct {
	slug    string
	version string
}

// scannableProjects is s.projects.List() zonder de referentie-installatie: die
// is geen klantsite, en zonder deze uitsluiting zou hij dubbel meetellen —
// zowel als gewoon projectrij als (via applyReferenceLatest) als bron voor de
// "laatste versie"-kolom van alle andere projecten.
func (s *InventoryService) scannableProjects() []domain.Project {
	path := s.referencePath()
	if path == "" {
		return s.projects.List()
	}
	path = filepath.Clean(path)

	alle := s.projects.List()
	uit := make([]domain.Project, 0, len(alle))
	for _, p := range alle {
		if filepath.Clean(p.Path) == path {
			continue
		}
		uit = append(uit, p)
	}
	return uit
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
	projects := s.scannableProjects()
	s.syncGithubRefs(projects)

	for _, p := range projects {
		gitRef := s.projectRef(p.Path)
		label := gitRef
		if label == "" {
			label = workingTreeRef
		}
		// Twee bronnen per project: de werkmap ("" = working tree) en de
		// GitHub-stand op origin/<default branch>.
		lokaal := map[string]string{}
		for _, ref := range read(p.Path, "") {
			lokaal[ref.slug] = ref.version
		}
		github := map[string]string{}
		if gitRef != "" {
			for _, ref := range read(p.Path, gitRef) {
				github[ref.slug] = ref.version
			}
		}

		for slug := range unionSlugs(lokaal, github) {
			it, ok := items[slug]
			if !ok {
				it = &InventoryItem{Slug: slug}
				items[slug] = it
			}
			it.Projects = append(it.Projects, InventoryProjectRef{
				ProjectID:     p.ID,
				ProjectName:   p.DisplayName,
				LocalVersion:  lokaal[slug],
				GithubVersion: github[slug],
				Ref:           label,
			})
		}
	}
	return items
}

// unionSlugs geeft de verzameling slugs die in minstens één van beide bronnen zit.
func unionSlugs(a, b map[string]string) map[string]struct{} {
	set := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		set[k] = struct{}{}
	}
	for k := range b {
		set[k] = struct{}{}
	}
	return set
}

// syncGithubRefs werkt de origin-refs bij waar GitHub vooruit is, zodat de
// GitHub-kolom de echte stand van de release-branch toont. Best-effort.
func (s *InventoryService) syncGithubRefs(projects []domain.Project) {
	if s.syncer == nil {
		return
	}
	// De syncer begrenst zijn eigen sweep (syncSweepBudget); deze context is
	// alleen een bovengrens voor het geval dat.
	ctx, cancel := context.WithTimeout(context.Background(), 2*syncSweepBudget)
	defer cancel()
	s.syncer.Sync(ctx, projects)
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
// finishItems sorteert en labelt elk item. fromReference markeert de slugs
// waarvan de laatste versie uit de referentie-installatie komt (nil bij
// Themes(), die kent dat begrip niet).
func finishItems(items map[string]*InventoryItem, fromReference map[string]bool) []InventoryItem {
	out := make([]InventoryItem, 0, len(items))
	for slug, it := range items {
		switch {
		case fromReference[slug]:
			it.Source = "reference"
		case it.LatestVersion == "":
			it.Source = "manual"
		default:
			it.Source = "wporg"
		}
		sort.Slice(it.Projects, func(i, j int) bool {
			return strings.ToLower(it.Projects[i].ProjectName) < strings.ToLower(it.Projects[j].ProjectName)
		})
		for i := range it.Projects {
			p := &it.Projects[i]
			// Outdated volgt de GitHub-kolom: dat is de stand die naar
			// productie gaat. LocalBehind is puur een pull-hint.
			*p = buildProjectRef(p.ProjectID, p.ProjectName, p.LocalVersion, p.GithubVersion, it.LatestVersion, p.Ref)
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
