package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// orgSyncTimeout begrenst een hele sync-run: bij ~590 repos kan het ophalen van
// alle deploy_conf.json's een tijdje duren, maar niet onbeperkt.
const orgSyncTimeout = 10 * time.Minute

// orgSyncSlot is de enige claim-sleutel: er is maar één org om te syncen, dus
// één slot volstaat (zie het claim/release-patroon in MediaService).
const orgSyncSlot = "org-sync"

// orgSyncProgressEvery bepaalt hoe vaak tijdens het fetchen een voortgangsevent
// verschijnt; bij honderden repos wil de UI geen event per repo.
const orgSyncProgressEvery = 10

const deployConfPath = "deploy_conf.json"

// orgRepoLister somt de repositories van een organisatie op (test seam).
type orgRepoLister interface {
	ListOrgRepos(ctx context.Context, org string) ([]github.OrgRepo, error)
}

// orgContentsGetter leest ruwe bestandsinhoud uit een repo (test seam).
type orgContentsGetter interface {
	GetContentsRaw(ctx context.Context, owner, repo, path, ref string) ([]byte, error)
}

// orgSyncCacheEntry is de laatst bekende toestand van één repo, gebruikt om een
// vervolgsync alleen gewijzigde repos opnieuw te laten fetchen.
type orgSyncCacheEntry struct {
	PushedAt      string `json:"pushedAt"`
	DeployType    string `json:"deployType"`
	HasDeployConf bool   `json:"hasDeployConf"`
}

// orgSyncCacheFile is het volledige op-schijf schema: het laatste resultaat
// (zodat Last() zonder netwerk kan antwoorden) plus, per repo-naam
// (lowercase), de laatst bekende fetch-toestand voor het incrementele gedrag.
type orgSyncCacheFile struct {
	Result  domain.OrgSyncResult         `json:"result"`
	Entries map[string]orgSyncCacheEntry `json:"entries"`
}

// localMatch is één lokale checkout waarvan de origin-remote een owner/repo
// heeft opgeleverd.
type localMatch struct {
	projectID   string
	displayName string
	path        string
	remote      string
	owner       string
	repo        string
}

// OrgSyncService haalt de repolijst van de GitHub-organisatie op, classificeert
// elke repo via deploy_conf.json, en matcht het resultaat tegen de lokale
// checkouts zodat direct zichtbaar is welke WordPress-sites nog geen lokale
// map hebben.
type OrgSyncService struct {
	projects *ProjectService
	cfg      *config.Global

	// lister/contents zijn lazy: pas gevuld bij de eerste Sync, zodat de app
	// zonder geconfigureerd token gewoon start. Tests zetten ze direct op fakes.
	lister    orgRepoLister
	contents  orgContentsGetter
	remoteURL func(ctx context.Context, dir string) (string, error)
	// clone is de clone-seam: productie gebruikt git, tests een fake, zodat de
	// clone-logica zonder netwerk en zonder echte repo's te testen is.
	clone func(ctx context.Context, parentDir, url, name string) error

	emitter eventEmitter
	now     func() time.Time

	// storePath overschrijft het standaardpad (~/.config/rdm/org-sync.json);
	// leeg laten gebruikt DefaultOrgSyncPath().
	storePath string

	bezigMu sync.Mutex
	bezig   map[string]bool
}

// NewOrgSyncService bouwt de productieversie: de GitHub-client wordt pas bij de
// eerste Sync aangemaakt (zie ensureClient), en de remote wordt via git zelf
// gelezen.
func NewOrgSyncService(projects *ProjectService, cfg *config.Global) *OrgSyncService {
	return &OrgSyncService{
		projects:  projects,
		cfg:       cfg,
		remoteURL: defaultRemoteURL,
		clone:     defaultClone,
		now:       time.Now,
		bezig:     map[string]bool{},
	}
}

// defaultRemoteURL leest de origin-remote van een lokale checkout via git.
func defaultRemoteURL(ctx context.Context, dir string) (string, error) {
	return gitcli.Run(ctx, dir, "remote", "get-url", "origin")
}

// DefaultOrgSyncPath is ~/.config/rdm/org-sync.json.
func DefaultOrgSyncPath() string {
	home, err := os.UserHomeDir()
	return defaultOrgSyncPathFrom(home, err)
}

// defaultOrgSyncPathFrom bouwt het pad op uit een (home, err) paar zoals
// os.UserHomeDir() dat teruggeeft; als losse functie is de foutfallback
// testbaar zonder de echte omgeving te hoeven forceren.
func defaultOrgSyncPathFrom(home string, err error) string {
	if err != nil || home == "" {
		// Nooit terugvallen op een relatief pad: de cwd van een .app-bundle is
		// onvoorspelbaar (vaak "/" of de bundle-map zelf), dus gebruik de
		// systeem-temp-map als home niet te bepalen is.
		return filepath.Join(os.TempDir(), "rdm", "org-sync.json")
	}
	return filepath.Join(home, ".config", "rdm", "org-sync.json")
}

// SetApp injects the Wails app reference (called after app creation).
func (s *OrgSyncService) SetApp(app *application.App) {
	s.emitter = app.Event
}

func (s *OrgSyncService) claim(slot string) bool {
	s.bezigMu.Lock()
	defer s.bezigMu.Unlock()
	if s.bezig[slot] {
		return false
	}
	s.bezig[slot] = true
	return true
}

func (s *OrgSyncService) release(slot string) {
	s.bezigMu.Lock()
	delete(s.bezig, slot)
	s.bezigMu.Unlock()
}

func (s *OrgSyncService) emit(p domain.OrgSyncProgress) {
	if s.emitter == nil {
		return
	}
	s.emitter.Emit("orgsync:progress", p)
}

// ensureClient maakt de echte GitHub-client pas aan bij de eerste sync, zodat
// de app zonder geconfigureerd token gewoon start. Tests die lister/contents al
// hebben gezet (fakes) slaan dit helemaal over.
func (s *OrgSyncService) ensureClient() error {
	if s.lister != nil && s.contents != nil {
		return nil
	}
	if s.cfg == nil {
		return fmt.Errorf("configuratie niet beschikbaar")
	}
	token, err := config.ResolveSecret(s.cfg.PluginRepo.GithubToken)
	if err != nil {
		return fmt.Errorf("github token: %w", err)
	}
	if token == "" {
		return fmt.Errorf("github token niet geconfigureerd (zie Instellingen)")
	}
	client := github.NewOrgClient(token)
	if s.lister == nil {
		s.lister = client
	}
	if s.contents == nil {
		s.contents = client
	}
	return nil
}

// localScan leest voor elk lokaal project de origin-remote en splitst die in
// owner/repo. Een project waarvan de remote niet te lezen of niet naar een
// GitHub-repo te herleiden is levert een warning op; een project zonder
// remote (lege string, geen fout) wordt stilzwijgend overgeslagen.
func (s *OrgSyncService) localScan(ctx context.Context) ([]localMatch, []string) {
	var matches []localMatch
	var warnings []string
	for _, p := range s.projects.List() {
		remote, err := s.remoteURL(ctx, p.Path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("remote van project %q niet te lezen: %v", p.DisplayName, err))
			continue
		}
		if strings.TrimSpace(remote) == "" {
			continue
		}
		owner, repo, err := github.ParseRepoFromRemote(remote)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("remote van project %q niet te herleiden tot een GitHub-repo: %v", p.DisplayName, err))
			continue
		}
		matches = append(matches, localMatch{
			projectID:   p.ID,
			displayName: p.DisplayName,
			path:        p.Path,
			remote:      remote,
			owner:       owner,
			repo:        repo,
		})
	}
	return matches, warnings
}

// resolveOrgFromMatches bepaalt de organisatie als de meest voorkomende owner
// onder de lokale checkouts. Matches die geen remote hadden of niet te parsen
// waren zijn hier al uit localScan weggefilterd (fouten dus stil overgeslagen).
// Bij een gelijke stand wint de alfabetisch kleinste owner: de eerdere
// implementatie liep over de map in (willekeurige) iteratievolgorde, waardoor
// de winnaar bij een tie van run tot run kon wisselen.
func resolveOrgFromMatches(matches []localMatch) (string, error) {
	counts := map[string]int{}
	for _, m := range matches {
		counts[strings.ToLower(m.owner)]++
	}
	if len(counts) == 0 {
		return "", fmt.Errorf("geen enkele lokale checkout heeft een GitHub-remote, dus de organisatie is niet te bepalen")
	}
	owners := make([]string, 0, len(counts))
	for owner := range counts {
		owners = append(owners, owner)
	}
	sort.Strings(owners)

	best, bestN := owners[0], counts[owners[0]]
	for _, owner := range owners[1:] {
		if n := counts[owner]; n > bestN {
			best, bestN = owner, n
		}
	}
	return best, nil
}

// Org bepaalt de GitHub-organisatie op basis van de remotes van de lokale
// checkouts. Vooral bedoeld voor tests en losstaand gebruik; Sync roept
// dezelfde logica intern aan.
func (s *OrgSyncService) Org(ctx context.Context) (string, error) {
	matches, _ := s.localScan(ctx)
	return resolveOrgFromMatches(matches)
}

// Sync haalt de volledige repolijst van de organisatie op, classificeert elke
// niet-archived repo waarvan de PushedAt is gewijzigd (of altijd bij force),
// en matcht het resultaat tegen de lokale checkouts.
func (s *OrgSyncService) Sync(force bool) (domain.OrgSyncResult, error) {
	if !s.claim(orgSyncSlot) {
		return domain.OrgSyncResult{}, fmt.Errorf("er loopt al een synchronisatie")
	}
	defer s.release(orgSyncSlot)

	ctx, cancel := context.WithTimeout(context.Background(), orgSyncTimeout)
	defer cancel()

	if err := s.ensureClient(); err != nil {
		return domain.OrgSyncResult{}, err
	}

	matches, warnings := s.localScan(ctx)
	org, err := resolveOrgFromMatches(matches)
	if err != nil {
		return domain.OrgSyncResult{}, err
	}

	s.emit(domain.OrgSyncProgress{Phase: "lijst"})

	orgRepos, err := s.lister.ListOrgRepos(ctx, org)
	if err != nil {
		return domain.OrgSyncResult{}, fmt.Errorf("org-repos ophalen: %w", err)
	}

	cache, err := s.loadCache()
	if err != nil {
		if !errors.Is(err, errCacheCorrupt) {
			return domain.OrgSyncResult{}, err
		}
		// Zelfherstellend: een corrupt cachebestand (bv. door een afgebroken
		// schrijfactie) mag een sync niet laten falen. We vallen terug op een
		// lege cache (dus een volledige herfetch) en melden dit als warning;
		// de nieuwe, geldige cache overschrijft het kapotte bestand hieronder.
		cache = orgSyncCacheFile{Entries: map[string]orgSyncCacheEntry{}}
		warnings = append(warnings, fmt.Sprintf("org-sync cache was corrupt en is genegeerd (volledige herfetch): %v", err))
	}

	total := len(orgRepos)
	scanned, fromCache, fetched := 0, 0, 0
	repos := make([]domain.OrgSyncRepo, 0, total)
	newEntries := make(map[string]orgSyncCacheEntry, total)

	for _, or := range orgRepos {
		key := strings.ToLower(or.Name)
		prev, hadPrev := cache.Entries[key]

		row := domain.OrgSyncRepo{
			Name:     or.Name,
			FullName: or.FullName,
			HTMLURL:  or.HTMLURL,
			Archived: or.Archived,
			Fork:     or.Fork,
			PushedAt: or.PushedAt,
		}

		switch {
		case or.Archived:
			// Archived repos worden nooit gefetcht, ook niet met force=true: ze
			// staan toch niet meer in ontwikkeling, dus de laatst bekende waarde
			// (of leeg, als die er nooit was) volstaat.
			if hadPrev {
				row.DeployType = prev.DeployType
				row.HasDeployConf = prev.HasDeployConf
			}
		case !force && hadPrev && prev.PushedAt == or.PushedAt:
			row.DeployType = prev.DeployType
			row.HasDeployConf = prev.HasDeployConf
			fromCache++
		default:
			body, ferr := s.contents.GetContentsRaw(ctx, org, or.Name, deployConfPath, "")
			scanned++
			fetched++
			switch {
			case ferr == nil:
				var parsed struct {
					Type string `json:"type"`
				}
				if jerr := json.Unmarshal(body, &parsed); jerr != nil {
					row.HasDeployConf = true
					row.DeployType = ""
					warnings = append(warnings, fmt.Sprintf("deploy_conf.json van %s is geen geldige JSON", or.Name))
				} else {
					row.HasDeployConf = true
					row.DeployType = parsed.Type
				}
			case errors.Is(ferr, github.ErrNotFound):
				row.HasDeployConf = false
				row.DeployType = ""
			default:
				// Eén kapotte repo mag de hele sync niet laten klappen: warning
				// erbij, en de vorige waarde (indien bekend) blijft staan.
				warnings = append(warnings, fmt.Sprintf("deploy_conf.json van %s ophalen mislukt: %v", or.Name, ferr))
				if hadPrev {
					row.DeployType = prev.DeployType
					row.HasDeployConf = prev.HasDeployConf
				}
			}
			if fetched%orgSyncProgressEvery == 0 {
				s.emit(domain.OrgSyncProgress{Phase: "bezig", Repo: or.Name, Done: fetched, Total: total})
			}
		}

		row.IsWordPress = strings.HasPrefix(row.DeployType, "wordpress")
		repos = append(repos, row)
		newEntries[key] = orgSyncCacheEntry{PushedAt: or.PushedAt, DeployType: row.DeployType, HasDeployConf: row.HasDeployConf}
	}

	matchedRepos, localOnly := matchRepos(org, repos, matches)
	sortOrgSyncRepos(matchedRepos)

	result := domain.OrgSyncResult{
		Org:       org,
		FetchedAt: s.now(),
		Repos:     matchedRepos,
		LocalOnly: localOnly,
		Totals:    computeTotals(matchedRepos),
		Scanned:   scanned,
		FromCache: fromCache,
		Warnings:  warnings,
	}

	if err := s.saveCache(orgSyncCacheFile{Result: result, Entries: newEntries}); err != nil {
		return domain.OrgSyncResult{}, err
	}

	s.emit(domain.OrgSyncProgress{Phase: "klaar", Done: total, Total: total})

	return result, nil
}

// Last laadt het laatst opgeslagen resultaat en voert de matching tegen de
// actuele lokale projecten opnieuw uit (dat is gratis en lokaal, dus "lokaal
// aanwezig" klopt ook zonder nieuwe netwerksync).
func (s *OrgSyncService) Last() (domain.OrgSyncResult, error) {
	cache, err := s.loadCache()
	if err != nil {
		if errors.Is(err, errCacheCorrupt) {
			return domain.OrgSyncResult{}, fmt.Errorf("org-sync cache is corrupt; een nieuwe synchronisatie herstelt dit: %w", err)
		}
		return domain.OrgSyncResult{}, err
	}
	if cache.Result.FetchedAt.IsZero() && len(cache.Result.Repos) == 0 {
		return domain.OrgSyncResult{}, fmt.Errorf("nog nooit gesynchroniseerd")
	}

	matches, _ := s.localScan(context.Background())
	result := cache.Result
	matchedRepos, localOnly := matchRepos(result.Org, result.Repos, matches)
	sortOrgSyncRepos(matchedRepos)
	result.Repos = matchedRepos
	result.LocalOnly = localOnly
	result.Totals = computeTotals(matchedRepos)
	return result, nil
}

// matchRepos vult LocalProjectID/LocalPath op elke repo-rij in aan de hand van
// de lokale checkouts, en bouwt de lijst van org-checkouts zonder org-repo.
func matchRepos(org string, repos []domain.OrgSyncRepo, matches []localMatch) ([]domain.OrgSyncRepo, []domain.OrgSyncLocalOnly) {
	byKey := make(map[string]localMatch, len(matches))
	for _, m := range matches {
		byKey[strings.ToLower(m.owner+"/"+m.repo)] = m
	}

	used := make(map[string]bool, len(byKey))
	out := make([]domain.OrgSyncRepo, len(repos))
	copy(out, repos)
	for i := range out {
		key := strings.ToLower(out[i].FullName)
		if m, ok := byKey[key]; ok {
			out[i].LocalProjectID = m.projectID
			out[i].LocalPath = m.path
			used[key] = true
		} else {
			out[i].LocalProjectID = ""
			out[i].LocalPath = ""
		}
	}

	var localOnly []domain.OrgSyncLocalOnly
	for key, m := range byKey {
		if used[key] {
			continue
		}
		if !strings.EqualFold(m.owner, org) {
			continue
		}
		localOnly = append(localOnly, domain.OrgSyncLocalOnly{
			ProjectID:   m.projectID,
			DisplayName: m.displayName,
			Path:        m.path,
			Remote:      m.remote,
		})
	}
	sort.Slice(localOnly, func(i, j int) bool { return localOnly[i].DisplayName < localOnly[j].DisplayName })

	return out, localOnly
}

// sortOrgSyncRepos zet WordPress-repos zonder lokale checkout vooraan (dat is
// precies het gat dat deze feature zichtbaar moet maken), en sorteert verder
// alfabetisch op FullName.
func sortOrgSyncRepos(repos []domain.OrgSyncRepo) {
	sort.SliceStable(repos, func(i, j int) bool {
		pi, pj := missingWPPriority(repos[i]), missingWPPriority(repos[j])
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(repos[i].FullName) < strings.ToLower(repos[j].FullName)
	})
}

func missingWPPriority(r domain.OrgSyncRepo) int {
	if r.IsWordPress && r.LocalPath == "" {
		return 0
	}
	return 1
}

func computeTotals(repos []domain.OrgSyncRepo) domain.OrgSyncTotals {
	var t domain.OrgSyncTotals
	for _, r := range repos {
		t.Repos++
		if r.Archived {
			t.Archived++
		}
		if r.IsWordPress {
			t.WordPress++
			switch {
			case r.LocalPath != "":
				t.WordPressLocal++
			case !r.Archived:
				t.WordPressMissing++
			}
		}
	}
	return t
}

func (s *OrgSyncService) storePathOrDefault() string {
	if s.storePath != "" {
		return s.storePath
	}
	return DefaultOrgSyncPath()
}

// errCacheCorrupt signaleert dat het cachebestand bestaat maar niet als JSON
// te parsen is (bv. door een crash of volle schijf halverwege een eerdere
// schrijfactie). Sync() behandelt dit als "geen cache" (volledige herfetch,
// met warning); Last() geeft een duidelijke fout die zegt dat een nieuwe
// synchronisatie dit herstelt.
var errCacheCorrupt = errors.New("org-sync cache corrupt")

func (s *OrgSyncService) loadCache() (orgSyncCacheFile, error) {
	path := s.storePathOrDefault()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return orgSyncCacheFile{Entries: map[string]orgSyncCacheEntry{}}, nil
		}
		return orgSyncCacheFile{}, fmt.Errorf("org-sync cache lezen: %w", err)
	}
	var cache orgSyncCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return orgSyncCacheFile{}, fmt.Errorf("org-sync cache parsen: %w (%v)", errCacheCorrupt, err)
	}
	if cache.Entries == nil {
		cache.Entries = map[string]orgSyncCacheEntry{}
	}
	return cache, nil
}

// saveCache schrijft eerst naar een tijdelijk bestand in dezelfde map en
// hernoemt dat vervolgens naar het echte pad. os.Rename is atomair binnen
// hetzelfde filesystem, dus een crash of volle schijf halverwege laat het
// bestaande cachebestand ongemoeid in plaats van afgekapte JSON achter te
// laten (wat os.WriteFile, dat eerst truncate en dan schrijft, wél kan doen).
func (s *OrgSyncService) saveCache(cache orgSyncCacheFile) error {
	path := s.storePathOrDefault()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("org-sync cachemap aanmaken: %w", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("org-sync cache serialiseren: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".org-sync-*.tmp")
	if err != nil {
		return fmt.Errorf("org-sync tijdelijk cachebestand aanmaken: %w", err)
	}
	tmpPath := tmp.Name()
	// Ruim het tempbestand op bij elk foutpad hieronder; na een geslaagde
	// os.Rename bestaat tmpPath niet meer, dus dan is dit een no-op.
	defer os.Remove(tmpPath)

	if _, werr := tmp.Write(data); werr != nil {
		tmp.Close()
		return fmt.Errorf("org-sync cache schrijven: %w", werr)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("org-sync cache schrijven: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("org-sync cache rechten zetten: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("org-sync cache atomisch vervangen: %w", err)
	}
	return nil
}
