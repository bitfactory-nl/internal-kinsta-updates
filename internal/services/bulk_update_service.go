package services

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// BulkUpdateService werkt alle WordPress-projecten in één run bij vanuit de
// referentie-installatie: de betaalde plugins én WordPress core. Per project is
// dat dezelfde route als de losse plugin-update — openstaand werk stashen, een
// branch aftakken van de verse remote-stand, committen, pushen, PR openen —
// maar hier over de hele portefeuille in één keer.
//
// Mergen zit er bewust niet in: dat blijft een losse keuze per project, want een
// bulk-merge naar productiebranches is geen knop die je per ongeluk indrukt.
type BulkUpdateService struct {
	cfg      *config.Global
	projects *ProjectService
	git      *GitService
	plugins  *PluginService

	emitter eventEmitter

	bezigMu sync.Mutex
	bezig   bool
}

func NewBulkUpdateService(cfg *config.Global, projects *ProjectService, git *GitService, plugins *PluginService) *BulkUpdateService {
	return &BulkUpdateService{cfg: cfg, projects: projects, git: git, plugins: plugins}
}

// SetApp injects the Wails app reference (called after app creation).
func (s *BulkUpdateService) SetApp(app *application.App) {
	s.emitter = app.Event
}

// BulkPluginPlan is één plugin die in dit project bijgewerkt zou worden.
type BulkPluginPlan struct {
	Slug string `json:"slug"`
	From string `json:"from"`
	To   string `json:"to"`
}

// BulkUpdateProjectPlan is wat er in één project te doen valt.
type BulkUpdateProjectPlan struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	// Branch is de branch waar het project nú op staat: die blijft na de run
	// ook de branch waar het weer op terugkomt.
	Branch string `json:"branch"`
	// CoreFrom/CoreTo zijn de WordPress-versies; CoreOutdated zegt of core
	// daadwerkelijk bijgewerkt wordt.
	CoreFrom     string `json:"coreFrom"`
	CoreTo       string `json:"coreTo"`
	CoreOutdated bool   `json:"coreOutdated"`
	// Plugins zijn alleen de plugins die achterlopen op de referentie.
	Plugins []BulkPluginPlan `json:"plugins"`
	// Skip is gevuld als er niets te doen is, of als dit project niet mee kan
	// (geen WordPress, geen webroot). De rij blijft dan zichtbaar met de reden
	// erbij: stil weglaten leest als "de tool ziet mijn project niet".
	Skip string `json:"skip,omitempty"`
}

// BulkUpdatePlan is de voorbeschouwing van een hele run.
type BulkUpdatePlan struct {
	// ReferenceCore is de WordPress-versie in de referentie-installatie: het
	// doel waar alle projecten naartoe gaan.
	ReferenceCore string                  `json:"referenceCore"`
	Projects      []BulkUpdateProjectPlan `json:"projects"`
}

// BulkUpdateProjectResult is de uitkomst voor één project.
type BulkUpdateProjectResult struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	// Status: updated | nothing (niets te doen) | error
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`

	Stash    string `json:"stash,omitempty"`
	Branch   string `json:"branch,omitempty"`
	VanafRef string `json:"vanafRef,omitempty"`

	CoreFrom   string `json:"coreFrom,omitempty"`
	CoreTo     string `json:"coreTo,omitempty"`
	CoreStatus string `json:"coreStatus,omitempty"` // updated | unchanged | error
	CoreError  string `json:"coreError,omitempty"`

	Plugins []LocalApplyPlugin `json:"plugins"`

	PullRequestURL    string `json:"pullRequestUrl,omitempty"`
	PullRequestNumber int    `json:"pullRequestNumber,omitempty"`
	PullRequestError  string `json:"pullRequestError,omitempty"`
	CanMerge          bool   `json:"canMerge"`

	// Na de run wordt de checkout teruggezet naar de branch waar hij op stond
	// en de stash teruggehaald: een bulk-run over tientallen repo's mag ze niet
	// allemaal op een update-branch achterlaten. Deze velden maken zichtbaar of
	// dat gelukt is.
	RestoredBranch string `json:"restoredBranch,omitempty"`
	StashRestored  bool   `json:"stashRestored"`
	RestoreError   string `json:"restoreError,omitempty"`
}

// BulkUpdateResult is de uitkomst van de hele run.
type BulkUpdateResult struct {
	Projects []BulkUpdateProjectResult `json:"projects"`
}

// BulkUpdateProgress gaat per fase naar de frontend, zodat een run over
// tientallen repo's niet als een stille wachttijd voelt.
type BulkUpdateProgress struct {
	Index       int    `json:"index"`
	Total       int    `json:"total"`
	ProjectName string `json:"projectName"`
	Phase       string `json:"phase"`
}

func (s *BulkUpdateService) emit(p BulkUpdateProgress) {
	if s.emitter == nil {
		return
	}
	s.emitter.Emit("bulkupdate:progress", p)
}

// referentiePad is de checkout van de referentie-installatie, of "".
func (s *BulkUpdateService) referentiePad() string {
	if s.cfg == nil {
		return ""
	}
	return strings.TrimSpace(s.cfg.PluginRepo.ReferenceProjectPath)
}

// wordpressProjecten zijn de projecten die voor deze run in aanmerking komen:
// WordPress volgens deploy_conf.json, en niet de referentie-installatie zelf.
func (s *BulkUpdateService) wordpressProjecten() []domain.Project {
	ref := s.referentiePad()
	var uit []domain.Project
	for _, p := range s.projects.List() {
		if !strings.HasPrefix(p.Deploy.Type, "wordpress") {
			continue
		}
		if ref != "" && filepath.Clean(p.Path) == filepath.Clean(ref) {
			continue // de referentie-installatie is geen klantsite
		}
		uit = append(uit, p)
	}
	return uit
}

// Plan geeft de voorbeschouwing: per project welke plugins en welke
// core-versie er bijgewerkt zouden worden. Niets wordt gewijzigd — dit is wat
// de gebruiker te zien krijgt vóór hij een run start.
func (s *BulkUpdateService) Plan() (BulkUpdatePlan, error) {
	ref := s.referentiePad()
	if ref == "" {
		return BulkUpdatePlan{}, fmt.Errorf("geen referentie-installatie ingesteld (zie Instellingen)")
	}

	refCore := leesWPVersie(wpRootDir(ref))
	if refCore == "" {
		return BulkUpdatePlan{}, fmt.Errorf("kan de WordPress-versie van de referentie-installatie niet lezen in %s", wpRootDir(ref))
	}

	refPlugins, err := s.plugins.ListLocalPaidPlugins()
	if err != nil {
		return BulkUpdatePlan{}, err
	}
	perSlug := map[string]LocalPaidPlugin{}
	for _, z := range refPlugins {
		if z.Error == "" && z.Slug != "" {
			perSlug[z.Slug] = z
		}
	}

	plan := BulkUpdatePlan{ReferenceCore: refCore}
	for _, p := range s.wordpressProjecten() {
		plan.Projects = append(plan.Projects, s.planVoorProject(p, perSlug, refCore))
	}
	return plan, nil
}

// planVoorProject stelt de voorbeschouwing voor één project op.
func (s *BulkUpdateService) planVoorProject(p domain.Project, refPlugins map[string]LocalPaidPlugin, refCore string) BulkUpdateProjectPlan {
	rij := BulkUpdateProjectPlan{
		ProjectID:   p.ID,
		ProjectName: p.DisplayName,
		CoreTo:      refCore,
	}
	if st, err := s.git.GetStatus(p.ID); err == nil {
		rij.Branch = st.Branch
	}

	rij.CoreFrom = leesWPVersie(wpRootDir(p.Path))
	if rij.CoreFrom == "" {
		rij.Skip = "geen leesbare WordPress-webroot in dit project"
		return rij
	}
	rij.CoreOutdated = compareVersions(rij.CoreFrom, refCore) < 0

	// Plugins vergelijken met wat er in de werkmap van dit project staat.
	installed, err := wpplugins.ReadInstalled(p.Path)
	if err != nil {
		rij.Skip = fmt.Sprintf("plugins niet te lezen: %v", err)
		return rij
	}
	for _, ip := range installed {
		z, ok := refPlugins[ip.Slug]
		if !ok || z.Version == "" || ip.Version == "" {
			continue // niet in de referentie, of geen versie om op te vergelijken
		}
		if compareVersions(ip.Version, z.Version) < 0 {
			rij.Plugins = append(rij.Plugins, BulkPluginPlan{Slug: ip.Slug, From: ip.Version, To: z.Version})
		}
	}

	if !rij.CoreOutdated && len(rij.Plugins) == 0 {
		rij.Skip = "al bij"
	}
	return rij
}

// Run werkt de gekozen projecten bij. Elk project is onafhankelijk: een
// mislukking bij het ene stopt de rest niet, en elk project krijgt zijn eigen
// branch, commit(s) en pull request.
func (s *BulkUpdateService) Run(projectIDs []string) (BulkUpdateResult, error) {
	if len(projectIDs) == 0 {
		return BulkUpdateResult{}, fmt.Errorf("geen projecten geselecteerd")
	}
	ref := s.referentiePad()
	if ref == "" {
		return BulkUpdateResult{}, fmt.Errorf("geen referentie-installatie ingesteld (zie Instellingen)")
	}
	if !s.claim() {
		return BulkUpdateResult{}, fmt.Errorf("er loopt al een bulk-update")
	}
	defer s.release()

	refCore := leesWPVersie(wpRootDir(ref))
	if refCore == "" {
		return BulkUpdateResult{}, fmt.Errorf("kan de WordPress-versie van de referentie-installatie niet lezen")
	}

	refPlugins, err := s.plugins.ListLocalPaidPlugins()
	if err != nil {
		return BulkUpdateResult{}, err
	}
	perSlug := map[string]LocalPaidPlugin{}
	for _, z := range refPlugins {
		if z.Error == "" && z.Slug != "" {
			perSlug[z.Slug] = z
		}
	}

	// De core één keer inlezen en voor elk project hergebruiken: dat zijn
	// duizenden bestanden, die haal je niet per project opnieuw van schijf.
	// Pas inlezen als er ook echt een project is dat core nodig heeft.
	var coreBestanden map[string][]byte
	coreNodig := func() error {
		if coreBestanden != nil {
			return nil
		}
		b, cerr := leesCoreWebroot(wpRootDir(ref))
		if cerr != nil {
			return cerr
		}
		coreBestanden = b
		return nil
	}

	branch := "chore/wp-updates-" + time.Now().Format("2006-01-02")
	res := BulkUpdateResult{}
	for i, id := range projectIDs {
		naam := id
		if p, ok := s.projects.Get(id); ok {
			naam = p.DisplayName
		}
		s.emit(BulkUpdateProgress{Index: i + 1, Total: len(projectIDs), ProjectName: naam, Phase: "start"})
		res.Projects = append(res.Projects, s.updateOneProject(id, naam, branch, refCore, perSlug, coreNodig, &coreBestanden, i+1, len(projectIDs)))
	}
	s.emit(BulkUpdateProgress{Index: len(projectIDs), Total: len(projectIDs), Phase: "klaar"})
	return res, nil
}

// updateOneProject doorloopt de hele route voor één project.
func (s *BulkUpdateService) updateOneProject(
	projectID, naam, branch, refCore string,
	refPlugins map[string]LocalPaidPlugin,
	coreNodig func() error,
	coreBestanden *map[string][]byte,
	index, total int,
) (r BulkUpdateProjectResult) {
	// Named return: herstel() loopt via defer en past r nog aan ná de laatste
	// return. Met een naamloze returnwaarde zouden die aanpassingen verdwijnen.
	r = BulkUpdateProjectResult{ProjectID: projectID, ProjectName: naam, CoreTo: refCore}

	p, ok := s.projects.Get(projectID)
	if !ok {
		r.Status = "error"
		r.Error = fmt.Sprintf("project %q niet gevonden", projectID)
		return r
	}

	plan := s.planVoorProject(p, refPlugins, refCore)
	r.CoreFrom = plan.CoreFrom
	if plan.Skip != "" {
		r.Status = "nothing"
		r.Error = plan.Skip
		return r
	}

	// 1. Stashen en een branch aftakken van de verse remote-stand.
	s.emit(BulkUpdateProgress{Index: index, Total: total, ProjectName: naam, Phase: "branch"})
	doel, err := s.plugins.ensureUpdateBranch(projectID, branch)
	r.Stash, r.Branch, r.VanafRef = doel.Stash, doel.Branch, doel.VanafRef
	if err != nil {
		r.Status = "error"
		r.Error = err.Error()
		return r
	}
	// Wat er hierna ook gebeurt: de checkout hoort terug te gaan naar waar hij
	// stond, en een stash die wij maakten hoort teruggehaald te worden.
	defer s.herstel(projectID, plan.Branch, &r)

	// 2. Plugins, elk met een eigen commit — dat leest in de PR beter dan één
	// grote commit met twintig plugins erin.
	s.emit(BulkUpdateProgress{Index: index, Total: total, ProjectName: naam, Phase: "plugins"})
	for _, pl := range plan.Plugins {
		z, ok := refPlugins[pl.Slug]
		if !ok {
			continue
		}
		r.Plugins = append(r.Plugins, s.plugins.applyOnePlugin(projectID, z))
	}

	// 3. WordPress core uit de referentie-installatie.
	if plan.CoreOutdated {
		s.emit(BulkUpdateProgress{Index: index, Total: total, ProjectName: naam, Phase: "core"})
		if err := s.updateCore(projectID, p.Path, plan.CoreFrom, refCore, coreNodig, coreBestanden); err != nil {
			r.CoreStatus = "error"
			r.CoreError = err.Error()
		} else {
			r.CoreStatus = "updated"
		}
	} else {
		r.CoreStatus = "unchanged"
	}

	// 4. Alleen pushen als er echt iets gecommit is.
	gelukt := 0
	for _, pl := range r.Plugins {
		if pl.Status == "updated" {
			gelukt++
		}
	}
	if gelukt == 0 && r.CoreStatus != "updated" {
		r.Status = "nothing"
		if r.Error == "" {
			r.Error = "er is niets gecommit"
		}
		return r
	}

	s.emit(BulkUpdateProgress{Index: index, Total: total, ProjectName: naam, Phase: "push + PR"})
	titel, body := bulkPRTekst(r)
	uit, prErr := s.plugins.pushEnOpenPRMet(projectID, r.Branch, titel, body)
	if prErr != nil {
		r.PullRequestError = prErr.Error()
	} else {
		r.PullRequestURL, r.PullRequestNumber, r.CanMerge = uit.URL, uit.Nummer, uit.CanMerge
	}
	r.Status = "updated"
	return r
}

// updateCore vervangt de core in het project door die van de
// referentie-installatie en committeert dat als eigen commit.
func (s *BulkUpdateService) updateCore(
	projectID, projectPath, van, naar string,
	coreNodig func() error,
	coreBestanden *map[string][]byte,
) error {
	if err := coreNodig(); err != nil {
		return fmt.Errorf("core van de referentie-installatie lezen: %w", err)
	}
	if err := schrijfCore(*coreBestanden, wpRootDir(projectPath)); err != nil {
		return fmt.Errorf("core vervangen: %w", err)
	}
	// -A onder public/: de nieuwe versie kan bestanden hebben weggehaald, en die
	// moeten ook uit de repository. wp-content valt hier ook onder, maar de
	// plugin-commits zijn al gedaan, dus daar staat niets meer open.
	if err := s.git.StageAllPath(projectID, "public"); err != nil {
		return err
	}
	st, err := s.git.GetStatus(projectID)
	if err != nil {
		return err
	}
	if len(st.Staged) == 0 {
		return nil // byte-identiek: niets te committen
	}
	msg := fmt.Sprintf("fix(wordpress): update WordPress core %s→%s (referentie-installatie)", weergaveVersie(van), naar)
	return s.git.CommitNoVerify(projectID, msg)
}

// herstel zet de checkout terug op de branch waar het project op stond en haalt
// een door ons gemaakte stash weer terug. Een bulk-run over tientallen repo's
// mag die niet allemaal op een update-branch met geparkeerd werk achterlaten.
// Mislukt het herstel, dan staat dat in het resultaat — de stash-aanduiding
// staat er toch al bij, dus er gaat niets verloren.
func (s *BulkUpdateService) herstel(projectID, oudeBranch string, r *BulkUpdateProjectResult) {
	if oudeBranch == "" || oudeBranch == r.Branch {
		return
	}
	if err := s.git.CheckoutBranch(projectID, oudeBranch); err != nil {
		r.RestoreError = fmt.Sprintf("terugzetten naar %s mislukt: %v", oudeBranch, err)
		return
	}
	r.RestoredBranch = oudeBranch

	if r.Stash == "" {
		return
	}
	// stash@{0} is de stash die wij net maakten: er is sindsdien niets bijgekomen.
	if err := s.git.StashPop(projectID, 0); err != nil {
		r.RestoreError = fmt.Sprintf("stash terughalen mislukt (%s blijft staan): %v", r.Stash, err)
		return
	}
	r.StashRestored = true
}

// bulkPRTekst stelt titel en beschrijving van de pull request op uit wat er in
// dit project daadwerkelijk is bijgewerkt.
func bulkPRTekst(r BulkUpdateProjectResult) (string, string) {
	var bijgewerkt []LocalApplyPlugin
	for _, p := range r.Plugins {
		if p.Status == "updated" {
			bijgewerkt = append(bijgewerkt, p)
		}
	}

	var delen []string
	if r.CoreStatus == "updated" {
		delen = append(delen, fmt.Sprintf("WordPress %s→%s", weergaveVersie(r.CoreFrom), r.CoreTo))
	}
	if len(bijgewerkt) > 0 {
		delen = append(delen, fmt.Sprintf("%d plugin(s)", len(bijgewerkt)))
	}
	titel := "chore(wordpress): " + strings.Join(delen, " + ") + " bijwerken"

	var b strings.Builder
	b.WriteString("Automatische update vanuit de referentie-installatie, aangemaakt met de RDM Sites Tool.\n\n")
	if r.CoreStatus == "updated" {
		fmt.Fprintf(&b, "**WordPress core:** %s → %s\n\n", weergaveVersie(r.CoreFrom), r.CoreTo)
		b.WriteString("Alleen `wp-admin/`, `wp-includes/` en de core-bestanden in de webroot zijn vervangen. ")
		b.WriteString("`wp-config.php` en `wp-content/` (behalve de plugins hieronder) zijn ongewijzigd.\n\n")
	}
	if len(bijgewerkt) > 0 {
		b.WriteString("**Betaalde plugins:**\n")
		for _, p := range bijgewerkt {
			van := p.From
			if van == "" {
				van = "nieuw"
			}
			fmt.Fprintf(&b, "- `%s`: %s → %s\n", p.Slug, van, p.To)
		}
		b.WriteString("\n")
	}
	if r.VanafRef != "" {
		fmt.Fprintf(&b, "Afgetakt van `%s`.\n", r.VanafRef)
	}
	b.WriteString("De commits zijn met `--no-verify` gemaakt: de inhoud komt uit de referentie-installatie, ")
	b.WriteString("dus de lint-hooks van dit project zijn er niet op van toepassing.\n")
	return titel, b.String()
}

func (s *BulkUpdateService) claim() bool {
	s.bezigMu.Lock()
	defer s.bezigMu.Unlock()
	if s.bezig {
		return false
	}
	s.bezig = true
	return true
}

func (s *BulkUpdateService) release() {
	s.bezigMu.Lock()
	s.bezig = false
	s.bezigMu.Unlock()
}
