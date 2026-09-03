package services

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/adapters/wpplugins"
	"github.com/rdm/sites-tool/internal/config"
)

// Dit bestand is de (tijdelijke) lokale-map-variant naast de betaalde-plugins-repo:
// een map op deze machine met verse zips, die per project in de werkmap gezet en op
// de héél huidige branch gecommit kunnen worden. De repo blijft bestaan en blijft de
// bron voor collega's en SSH-updates; dit is de kortere weg voor wie de zips net
// van de leverancier heeft gehaald.

// LocalPaidPlugin is één zip uit de lokale map. Een onleesbare zip krijgt een
// Error in plaats van dat hij stil verdwijnt — een typefout in een download wil je
// zien, niet raden.
type LocalPaidPlugin struct {
	Slug       string `json:"slug"`
	Version    string `json:"version"`
	FileName   string `json:"fileName"`
	IsDir      bool   `json:"isDir"`
	ModifiedAt int64  `json:"modifiedAt"`
	Error      string `json:"error,omitempty"`
	// Source zegt uit welke van de twee mogelijke bronnen deze plugin komt: de
	// (tijdelijke) losse map met zips ("map"), of de referentie-installatie
	// ("referentie"). Zichtbaar in de UI zodat een dubbele slug niet als toeval
	// oogt.
	Source string `json:"source"`
	// SourceDir is de map waarin FileName staat; nodig om de plugin te kunnen
	// toepassen ongeacht welke van de twee bronnen hij kwam. Niet naar de
	// frontend: die heeft er niets aan en het is een absoluut pad op deze Mac.
	SourceDir string `json:"-"`
}

// LocalPluginRow is de vergelijking van één zip met wat er in het project staat.
type LocalPluginRow struct {
	Slug           string `json:"slug"`
	FileName       string `json:"fileName"`
	FolderVersion  string `json:"folderVersion"`
	ProjectVersion string `json:"projectVersion"` // leeg = niet in dit project
	Newer          bool   `json:"newer"`
	// Source: "map" of "referentie" — zie LocalPaidPlugin.Source.
	Source string `json:"source"`
	// Error maakt een onbruikbare bron zichtbaar in het paneel zelf; een regel die
	// stil wegvalt leest als "de tool ziet mijn map niet".
	Error string `json:"error,omitempty"`
}

// LocalPluginOverview is wat de Plugins-tab nodig heeft: de rijen plus de branch
// waarop een commit terecht zou komen. Die branch tonen is geen detail — de commit
// gaat op de huidige checkout, dus de gebruiker moet zien welke dat is.
type LocalPluginOverview struct {
	Branch string           `json:"branch"`
	Rows   []LocalPluginRow `json:"rows"`
}

// LocalApplyPlugin is het resultaat voor één plugin.
type LocalApplyPlugin struct {
	Slug string `json:"slug"`
	From string `json:"from"`
	To   string `json:"to"`
	// Status: updated (gecommit) | unchanged (byte-identiek, niets te doen) | error
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// LocalApplyResult is het resultaat van één plaatsing.
type LocalApplyResult struct {
	Branch  string             `json:"branch"`
	Plugins []LocalApplyPlugin `json:"plugins"`
	// Stash beschrijft het werk dat vooraf automatisch geparkeerd is
	// (bijv. "stash@{0}: On release/1.0.x: …"), of is leeg als de werkmap al
	// schoon was. Dit moet zichtbaar zijn: werk dat de tool wegzet en niet
	// benoemt, is werk dat de gebruiker kwijt denkt te zijn.
	Stash string `json:"stash,omitempty"`
	// PullRequestURL is de PR die na de commit is geopend (of al open stond).
	PullRequestURL string `json:"pullRequestUrl,omitempty"`
	// PullRequestError vertelt waarom er géén PR kwam terwijl de commit wel
	// lukte — bijvoorbeeld geen token of geen GitHub-remote. Bewust apart van
	// de plugin-status: de update zelf is dan gewoon geslaagd.
	PullRequestError string `json:"pullRequestError,omitempty"`
	// PullRequestNumber en CanMerge voeden de merge-knop: het nummer om te
	// mergen, en of dit token dat op deze repo mag.
	PullRequestNumber int  `json:"pullRequestNumber,omitempty"`
	CanMerge          bool `json:"canMerge"`
}

// LocalDirConfigured reports whether either bron is ingesteld: de losse map of
// de referentie-installatie.
func (s *PluginService) LocalDirConfigured() bool {
	return s.localDir() != "" || s.referenceProjectPath() != ""
}

func (s *PluginService) localDir() string {
	return strings.TrimSpace(s.cfg.PluginRepo.LocalDir)
}

// referenceProjectPath is de checkout van de referentie-installatie, of "".
func (s *PluginService) referenceProjectPath() string {
	return strings.TrimSpace(s.cfg.PluginRepo.ReferenceProjectPath)
}

// ListLocalPaidPlugins scans beide mogelijke bronnen voor plugin-zips/-mappen:
// de (tijdelijke) losse map en de referentie-installatie. Geen van beide
// ingesteld levert (nil, nil): dat is de normale staat, geen fout.
//
// Bij een slug die in beide bronnen voorkomt wint de referentie-installatie —
// die wordt continu bijgehouden, de losse map is per definitie tijdelijk
// ("net van de leverancier gehaald"). De rij van de losse map valt dan weg,
// niet stil: ze staat niet meer apart in de lijst, maar er gaat ook niets
// verloren, want de referentie-versie is de versie die je wil.
func (s *PluginService) ListLocalPaidPlugins() ([]LocalPaidPlugin, error) {
	var uit []LocalPaidPlugin
	perSlug := map[string]int{} // slug -> index in uit, voor de referentie-override

	if dir := s.localDir(); dir != "" {
		rijen, err := scanPluginDir(dir, "map")
		if err != nil {
			return nil, fmt.Errorf("lokale pluginmap lezen: %w", err)
		}
		for _, r := range rijen {
			uit = append(uit, r)
			if r.Slug != "" {
				perSlug[r.Slug] = len(uit) - 1
			}
		}
	}
	if ref := s.referenceProjectPath(); ref != "" {
		rijen, err := scanPluginDir(wpplugins.PluginsDir(ref), "referentie")
		if err != nil {
			return nil, fmt.Errorf("referentie-installatie lezen: %w", err)
		}
		for _, r := range rijen {
			if r.Slug == "" {
				uit = append(uit, r) // foutrij, geen slug om op te dedupliceren
				continue
			}
			if i, dubbel := perSlug[r.Slug]; dubbel {
				uit[i] = r // referentie overschrijft de rij uit de losse map
				continue
			}
			perSlug[r.Slug] = len(uit)
			uit = append(uit, r)
		}
	}

	sort.Slice(uit, func(i, j int) bool { return uit[i].FileName < uit[j].FileName })
	return uit, nil
}

// scanPluginDir leest één map met plugin-zips en/of uitgepakte pluginmappen.
// Gedeeld tussen de losse map en de referentie-installatie: beide hebben
// exact dezelfde vorm (een map per plugin, of een zip per plugin).
func scanPluginDir(dir, source string) ([]LocalPaidPlugin, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var uit []LocalPaidPlugin
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // .DS_Store, .git en soortgenoten
		}
		rij := LocalPaidPlugin{FileName: e.Name(), IsDir: e.IsDir(), Source: source, SourceDir: dir}
		if info, ierr := e.Info(); ierr == nil {
			rij.ModifiedAt = info.ModTime().Unix()
		}
		switch {
		case e.IsDir():
			// Een uitgepakte pluginmap: de mapnaam is de slug, de versie komt uit
			// de header van het hoofdbestand — net als bij een zip.
			versie, perr := leesPluginDirInfo(filepath.Join(dir, e.Name()))
			if perr != nil {
				rij.Error = perr.Error()
			} else {
				rij.Slug, rij.Version = e.Name(), versie
			}
		case strings.EqualFold(filepath.Ext(e.Name()), ".zip"):
			data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
			if rerr != nil {
				rij.Error = rerr.Error()
				break
			}
			slug, versie, perr := leesPluginZipInfo(data)
			if perr != nil {
				rij.Error = perr.Error()
				break
			}
			rij.Slug, rij.Version = slug, versie
		default:
			continue // losse bestanden die geen zip zijn horen hier niet bij
		}
		uit = append(uit, rij)
	}
	return uit, nil
}

// LocalPluginDiff compares every readable zip in the folder against the plugin
// versions in the project's working tree.
func (s *PluginService) LocalPluginDiff(projectID string) (LocalPluginOverview, error) {
	p, ok := s.projects.Get(projectID)
	if !ok {
		return LocalPluginOverview{}, fmt.Errorf("project %q niet gevonden", projectID)
	}
	lijst, err := s.ListLocalPaidPlugins()
	if err != nil {
		return LocalPluginOverview{}, err
	}

	overzicht := LocalPluginOverview{}
	if st, serr := s.git.GetStatus(projectID); serr == nil {
		overzicht.Branch = st.Branch
	}

	pluginsDir := wpplugins.PluginsDir(p.Path)
	for _, z := range lijst {
		if z.Error != "" {
			overzicht.Rows = append(overzicht.Rows, LocalPluginRow{FileName: z.FileName, Error: z.Error})
			continue
		}
		rij := LocalPluginRow{
			Slug:           z.Slug,
			FileName:       z.FileName,
			FolderVersion:  z.Version,
			ProjectVersion: currentVersion(pluginsDir, z.Slug),
			Source:         z.Source,
		}
		rij.Newer = rij.ProjectVersion != "" && compareVersions(rij.FolderVersion, rij.ProjectVersion) > 0
		overzicht.Rows = append(overzicht.Rows, rij)
	}
	return overzicht, nil
}

// ApplyLocalPlugins extracts the chosen zips into the project's plugin folder and
// commits each plugin. A plugin uit de losse map commit op de branch waar het
// project nu al op staat — geen branchwissel, de checkout is waar de update
// hoort. Een plugin uit de referentie-installatie krijgt altijd zijn eigen
// branch (zie ensureBranchFor): die installatie voedt élk project, dus een
// rechtstreekse commit op de huidige checkout zou zomaar een productie-branch
// kunnen zijn. Pushing blijft in beide gevallen een bewuste, aparte actie.
func (s *PluginService) ApplyLocalPlugins(projectID string, slugs []string) (LocalApplyResult, error) {
	if _, ok := s.projects.Get(projectID); !ok {
		return LocalApplyResult{}, fmt.Errorf("project %q niet gevonden", projectID)
	}
	if len(slugs) == 0 {
		return LocalApplyResult{}, fmt.Errorf("geen plugins geselecteerd")
	}

	lijst, err := s.ListLocalPaidPlugins()
	if err != nil {
		return LocalApplyResult{}, err
	}
	perSlug := map[string]LocalPaidPlugin{}
	for _, z := range lijst {
		if z.Error == "" {
			perSlug[z.Slug] = z
		}
	}

	// Eén bron voor de hele batch: zodra er ook maar één referentie-plugin bij
	// zit, gaat de héle batch naar de eigen branch — een commit die half op de
	// huidige checkout en half op een nieuwe branch belandt zou verwarrender
	// zijn dan deze ene, eenvoudige regel.
	source := "map"
	var refPlugins []LocalPaidPlugin
	for _, slug := range slugs {
		if z, ok := perSlug[slug]; ok && z.Source == "referentie" {
			source = "referentie"
			refPlugins = append(refPlugins, z)
		}
	}
	doel, err := s.ensureBranchFor(projectID, source, refPlugins)
	if err != nil {
		return LocalApplyResult{}, err
	}

	res := LocalApplyResult{Branch: doel.Branch, Stash: doel.Stash}
	for _, slug := range slugs {
		z, ok := perSlug[slug]
		if !ok {
			res.Plugins = append(res.Plugins, LocalApplyPlugin{
				Slug: slug, Status: "error",
				Error: "geen leesbare bron voor deze plugin (niet in de lokale map en niet in de referentie-installatie)",
			})
			continue
		}
		res.Plugins = append(res.Plugins, s.applyOnePlugin(projectID, z))
	}

	// Alleen een referentie-update krijgt een eigen branch, en dus ook alleen
	// die hoort te worden gepusht met een PR erop. Een commit uit de losse map
	// staat op de huidige checkout; die pushen zou een productie-branch kunnen
	// raken zonder dat erom gevraagd is.
	if source == "referentie" {
		if uit, prErr := s.pushEnOpenPR(projectID, doel.Branch, res.Plugins); prErr != nil {
			res.PullRequestError = prErr.Error()
		} else {
			res.PullRequestURL, res.PullRequestNumber, res.CanMerge = uit.URL, uit.Nummer, uit.CanMerge
		}
	}
	return res, nil
}

// applyOnePlugin plaatst en committeert precies één plugin in precies één
// project. Gaat ervan uit dat de aanroeper de staged-files-controle al heeft
// gedaan — ApplyLocalPlugins doet dat eenmalig voor de hele batch,
// applyOnePluginGuarded doet het per project voor de bulk-variant.
func (s *PluginService) applyOnePlugin(projectID string, z LocalPaidPlugin) LocalApplyPlugin {
	pr := LocalApplyPlugin{Slug: z.Slug}
	p, ok := s.projects.Get(projectID)
	if !ok {
		pr.Status = "error"
		pr.Error = fmt.Sprintf("project %q niet gevonden", projectID)
		return pr
	}

	pluginsDir := wpplugins.PluginsDir(p.Path)
	pr.From = currentVersion(pluginsDir, z.Slug)
	pr.To = z.Version

	var installErr error
	if z.IsDir {
		installErr = kopieerMapReplace(filepath.Join(z.SourceDir, z.FileName), pluginsDir, z.Slug)
	} else {
		data, rerr := os.ReadFile(filepath.Join(z.SourceDir, z.FileName))
		if rerr != nil {
			installErr = rerr
		} else {
			installErr = extractZipReplace(data, pluginsDir, z.Slug)
		}
	}
	if installErr != nil {
		pr.Status = "error"
		pr.Error = installErr.Error()
		return pr
	}
	relPad := "public/wp-content/plugins/" + z.Slug
	if err := s.git.StageFiles(projectID, []string{relPad}); err != nil {
		pr.Status = "error"
		pr.Error = err.Error()
		return pr
	}
	// Byte-identiek aan wat er al stond: dan is er niets om te committen. Zonder
	// deze controle loopt dat uit op een kale "git commit: exit status 1" — een
	// fout die niet vertelt dat er simpelweg niets te doen was. De aanroeper mag
	// vooraf al filteren (het hoofdmenu schakelt actuele projecten uit), maar de
	// per-project-weg laat een bewuste herinstallatie toe en komt hier dus wel.
	if st, serr := s.git.GetStatus(projectID); serr == nil && len(st.Staged) == 0 {
		pr.Status = "unchanged"
		return pr
	}
	van := pr.From
	if van == "" {
		van = "nieuw"
	}
	msg := fmt.Sprintf("chore(plugins): update %s %s→%s (betaald, %s)", z.Slug, van, z.Version, bronLabel(z.Source))
	// --no-verify: de inhoud van deze commit is een vendor-plugin, niet onze
	// code. Een husky/ESLint-hook die op al bestaande projectbestanden
	// struikelt mag een plugin-update niet blokkeren — dat gebeurde in de
	// praktijk en liet halve bulk-runs achter.
	if err := s.git.CommitNoVerify(projectID, msg); err != nil {
		pr.Status = "error"
		pr.Error = err.Error()
		return pr
	}
	pr.Status = "updated"
	return pr
}

// applyOnePluginGuarded is applyOnePlugin plus de branch-/schoon-werkmap-
// controle uit ensureBranchFor, voor gebruik in de bulk-variant waar elk
// project onafhankelijk zijn eigen git-status heeft — een conflict in het ene
// project mag de andere projecten niet blokkeren, dus dit geeft een foutrij
// terug in plaats van de hele bulk-aanroep te laten falen. De branch waarop
// het is beland gaat terug naar de aanroeper, want bij een referentie-plugin
// is dat per project een andere nieuwe branch.
func (s *PluginService) applyOnePluginGuarded(projectID string, z LocalPaidPlugin) (LocalApplyPlugin, applyDoel) {
	doel, err := s.ensureBranchFor(projectID, z.Source, []LocalPaidPlugin{z})
	if err != nil {
		return LocalApplyPlugin{Slug: z.Slug, Status: "error", Error: err.Error()}, doel
	}
	return s.applyOnePlugin(projectID, z), doel
}

// applyDoel is waar een reeks plugin-commits terechtkomt, plus wat er onderweg
// automatisch aan de kant is gezet.
type applyDoel struct {
	Branch string
	Stash  string
	// VanafRef is de ref waar de branch van aftakt: origin/<standaardbranch>
	// als die te bereiken was, anders de lokale standaardbranch. Zichtbaar in
	// het resultaat, want het verschil bepaalt of de PR bovenop de actuele
	// stand zit.
	VanafRef string
}

// referenceUpdateBranch bouwt de branchnaam voor een update uit de
// referentie-installatie: nooit de huidige checkout (die kan een
// productie-branch zijn). Eén plugin zet zijn eigen naam en versie in de
// branch — dat is wat je in een PR-overzicht wil zien, en het maakt de branch
// meteen herhaalbaar: dezelfde plugin op dezelfde versie hergebruikt dezelfde
// branch in plaats van er elke dag een nieuwe bij te maken. Een batch van
// meerdere plugins valt terug op de datum: alle slugs in één branchnaam
// proppen levert een onleesbare naam op.
func referenceUpdateBranch(plugins []LocalPaidPlugin) string {
	if len(plugins) == 1 {
		slug := veiligBranchDeel(plugins[0].Slug)
		versie := veiligBranchDeel(plugins[0].Version)
		switch {
		case slug != "" && versie != "":
			return "chore/plugin-" + slug + "-" + versie
		case slug != "":
			return "chore/plugin-" + slug + "-" + time.Now().Format("2006-01-02")
		}
	}
	return "chore/plugin-updates-" + time.Now().Format("2006-01-02")
}

// reOnveiligBranchTeken houdt alleen tekens over die zonder nadenken in een
// git-branchnaam mogen; al het andere wordt een streepje. Een pluginversie in
// het wild is niet altijd netjes ("1.2.3-beta 2", "2.0 (RC1)"), en zo'n string
// mag geen ongeldige ref of shell-verrassing opleveren.
var reOnveiligBranchTeken = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// veiligBranchDeel maakt van een slug of versie een bruikbaar stuk branchnaam.
func veiligBranchDeel(in string) string {
	uit := reOnveiligBranchTeken.ReplaceAllString(strings.ToLower(strings.TrimSpace(in)), "-")
	// ".." en een naam die op ".lock" eindigt zijn door git verboden; punten aan
	// de randen leveren daarnaast rare refs op.
	for strings.Contains(uit, "..") {
		uit = strings.ReplaceAll(uit, "..", ".")
	}
	uit = strings.Trim(uit, "-.")
	uit = strings.TrimSuffix(uit, ".lock")
	return strings.Trim(uit, "-.")
}

// ensureReferenceBranch zet de checkout van het project op (en maakt zo nodig)
// de referentie-update-branch, vertrekkend vanaf de standaardbranch van het
// project. Staat er nog werk open, dan wordt dat automatisch gestasht: een
// branchwissel zou het anders meenemen in de plugin-commit, en de gebruiker
// halverwege een bulk-run om handwerk vragen is geen bulk-run. De stash wordt
// benoemd teruggegeven zodat er niets ongemerkt verdwijnt.
func (s *PluginService) ensureReferenceBranch(projectID string, plugins []LocalPaidPlugin) (applyDoel, error) {
	return s.ensureUpdateBranch(projectID, referenceUpdateBranch(plugins))
}

// ensureUpdateBranch is ensureReferenceBranch met een expliciete branchnaam,
// zodat de bulk-update (plugins én core in één commit) zijn eigen naam kan
// meegeven in plaats van de per-plugin-naam.
//
// De branch takt af van origin/<standaardbranch> ná een fetch, niet van de
// lokale standaardbranch: die kan dagen achterlopen, en dan zou de update
// bovenop een verouderde stand landen en de PR een berg niet-gerelateerde
// verschillen tonen. Zonder origin (een repo zonder remote) valt het terug op
// de lokale branch, want dan is dat de enige waarheid die er is.
func (s *PluginService) ensureUpdateBranch(projectID, branch string) (applyDoel, error) {
	doel := applyDoel{}
	st, err := s.git.GetStatus(projectID)
	if err != nil {
		return doel, err
	}
	if isDirty(st) {
		bericht := fmt.Sprintf("rdm-sites-tool: auto-stash voor plugin-update %s", time.Now().Format("2006-01-02 15:04:05"))
		stash, serr := s.git.StashAllAndDescribe(projectID, bericht)
		if serr != nil {
			return doel, fmt.Errorf("openstaand werk stashen mislukt: %w", serr)
		}
		doel.Stash = stash
	}
	def, err := s.git.DefaultBranch(projectID)
	if err != nil {
		return doel, fmt.Errorf("standaardbranch bepalen: %w", err)
	}

	// Vertrekpunt: de verse remote-stand als die er is. Een mislukte fetch is
	// geen blokkade (offline, of geen remote) — dan is de lokale branch het
	// vertrekpunt en dat staat in het resultaat via Branch.
	vanaf := def
	if _, rerr := s.git.RemoteURL(projectID); rerr == nil {
		if ferr := s.git.Fetch(projectID); ferr == nil {
			if _, verr := s.git.RevParse(projectID, "origin/"+def); verr == nil {
				vanaf = "origin/" + def
			}
		}
	}
	doel.VanafRef = vanaf

	if err := s.git.CheckoutOrCreateBranch(projectID, branch, vanaf); err != nil {
		return doel, fmt.Errorf("branch %q aanmaken mislukt: %w", branch, err)
	}
	doel.Branch = branch
	return doel, nil
}

// ensureBranchFor bepaalt (en zet zo nodig) de branch voor een commit met deze
// bron. Een plugin uit de losse map verandert niets aan de bestaande checkout
// — alleen de staged-files-controle, zoals altijd. Een plugin uit de
// referentie-installatie gaat via ensureReferenceBranch altijd naar zijn eigen
// branch, met automatische stash van openstaand werk.
func (s *PluginService) ensureBranchFor(projectID, source string, plugins []LocalPaidPlugin) (applyDoel, error) {
	if source == "referentie" {
		return s.ensureReferenceBranch(projectID, plugins)
	}
	st, err := s.git.GetStatus(projectID)
	if err != nil {
		return applyDoel{}, err
	}
	if len(st.Staged) > 0 {
		return applyDoel{}, fmt.Errorf(
			"er staan al %d bestand(en) klaargezet om te committen; commit of unstage die eerst", len(st.Staged))
	}
	return applyDoel{Branch: st.Branch}, nil
}

// pluginPulls is de PR-kant van GitHub, klein gehouden zodat een test hem kan
// vervangen zonder netwerk.
type pluginPulls interface {
	FindOpenPull(ctx context.Context, owner, repo, head string) (*github.PullRequest, error)
	CreatePull(ctx context.Context, owner, repo, head, base, title, body string) (*github.PullRequest, error)
	GetRepoAccess(ctx context.Context, owner, repo string) (*github.RepoAccess, error)
	MergePull(ctx context.Context, owner, repo string, number int, method string) (*github.MergeResult, error)
}

// pullClient geeft de PR-client: de geïnjecteerde fake in tests, anders een
// verse client met het token uit de instellingen.
func (s *PluginService) pullClient() (pluginPulls, error) {
	if s.pulls != nil {
		return s.pulls, nil
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("configuratie niet beschikbaar")
	}
	token, err := config.ResolveSecret(s.cfg.PluginRepo.GithubToken)
	if err != nil {
		return nil, fmt.Errorf("github token: %w", err)
	}
	if token == "" {
		return nil, fmt.Errorf("github token niet geconfigureerd (zie Instellingen)")
	}
	return github.NewPullClient(token), nil
}

// pushEnOpenPR pusht de update-branch en opent daar een pull request op, of
// geeft de URL van een PR die al open stond (tweede plugin op dezelfde branch,
// of een herhaalde run). De commit is op dit punt al gelukt; een fout hier is
// dus geen mislukte update maar een aparte melding — vandaar dat de aanroeper
// dit in PullRequestError zet en de plugin-status met rust laat.
// prUitkomst is wat er na de commit met de pull request is gebeurd.
type prUitkomst struct {
	URL    string
	Nummer int
	// CanMerge is true als het token op déze repo mag mergen (push-recht). Alleen
	// dan biedt de UI een merge-knop aan: een knop die altijd op "geen rechten"
	// eindigt is erger dan geen knop.
	CanMerge bool
}

// pushEnOpenPR pusht de update-branch en opent er een pull request op, of
// geeft de al open PR voor dezelfde branch terug.
func (s *PluginService) pushEnOpenPR(projectID, branch string, gedaan []LocalApplyPlugin) (prUitkomst, error) {
	var bijgewerkt []LocalApplyPlugin
	for _, p := range gedaan {
		if p.Status == "updated" {
			bijgewerkt = append(bijgewerkt, p)
		}
	}
	if len(bijgewerkt) == 0 {
		return prUitkomst{}, nil // niets gecommit: niets om te pushen, en geen fout
	}
	titel, body := prTekst(bijgewerkt)
	return s.pushEnOpenPRMet(projectID, branch, titel, body)
}

// pushEnOpenPRMet is pushEnOpenPR met een zelf opgestelde titel en beschrijving,
// zodat de bulk-update (plugins én WordPress core in één branch) zijn eigen
// tekst kan meegeven.
func (s *PluginService) pushEnOpenPRMet(projectID, branch, titel, body string) (prUitkomst, error) {
	if branch == "" {
		return prUitkomst{}, fmt.Errorf("geen branch om te pushen")
	}

	remote, err := s.git.RemoteURL(projectID)
	if err != nil {
		return prUitkomst{}, err
	}
	owner, repo, err := github.ParseRepoFromRemote(remote)
	if err != nil {
		return prUitkomst{}, fmt.Errorf("origin-URL %q: %w", remote, err)
	}
	base, err := s.git.DefaultBranch(projectID)
	if err != nil {
		return prUitkomst{}, fmt.Errorf("standaardbranch bepalen: %w", err)
	}
	pulls, err := s.pullClient()
	if err != nil {
		return prUitkomst{}, err
	}

	if err := s.git.PushSetUpstream(projectID, branch); err != nil {
		return prUitkomst{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	uit := prUitkomst{}
	// Een bestaande open PR op dezelfde branch hergebruiken; de push hierboven
	// heeft de nieuwe commit er al aan toegevoegd.
	if open, ferr := pulls.FindOpenPull(ctx, owner, repo, branch); ferr == nil && open != nil {
		uit.URL, uit.Nummer = open.HTMLURL, open.Number
	} else {
		pr, cerr := pulls.CreatePull(ctx, owner, repo, branch, base, titel, body)
		if cerr != nil {
			return prUitkomst{}, fmt.Errorf("pull request aanmaken: %w", cerr)
		}
		if pr == nil {
			return prUitkomst{}, nil
		}
		uit.URL, uit.Nummer = pr.HTMLURL, pr.Number
	}

	// Mag dit token hier mergen? Kan de tool dat niet vaststellen, dan komt er
	// geen merge-knop; de PR-link staat er dan nog steeds. Dit is bewust geen
	// fout: de PR is gelukt, alleen het mergen is onbekend.
	if acc, aerr := pulls.GetRepoAccess(ctx, owner, repo); aerr == nil && acc != nil {
		uit.CanMerge = acc.CanPush && acc.MergeMethod() != ""
	}
	return uit, nil
}

// MergePRResult is de uitkomst van een merge-poging via de UI.
type MergePRResult struct {
	Merged  bool   `json:"merged"`
	SHA     string `json:"sha,omitempty"`
	Message string `json:"message,omitempty"`
}

// MergePluginPullRequest merget de pull request van een plugin-update in het
// project waar hij bij hoort. Bewust een aparte, expliciete actie: de knop in
// de UI is de bevestiging, en pas hier gaat er iets naar de default branch.
// De merge-methode volgt wat de repo toestaat.
func (s *PluginService) MergePluginPullRequest(projectID string, number int) (MergePRResult, error) {
	if number <= 0 {
		return MergePRResult{}, fmt.Errorf("ongeldig pull request-nummer %d", number)
	}
	remote, err := s.git.RemoteURL(projectID)
	if err != nil {
		return MergePRResult{}, err
	}
	owner, repo, err := github.ParseRepoFromRemote(remote)
	if err != nil {
		return MergePRResult{}, fmt.Errorf("origin-URL %q: %w", remote, err)
	}
	pulls, err := s.pullClient()
	if err != nil {
		return MergePRResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	acc, err := pulls.GetRepoAccess(ctx, owner, repo)
	if err != nil {
		return MergePRResult{}, fmt.Errorf("rechten op %s/%s bepalen: %w", owner, repo, err)
	}
	if !acc.CanPush {
		return MergePRResult{}, fmt.Errorf("dit token heeft geen push-recht op %s/%s en mag dus niet mergen", owner, repo)
	}
	methode := acc.MergeMethod()
	if methode == "" {
		return MergePRResult{}, fmt.Errorf("%s/%s staat geen enkele merge-methode toe", owner, repo)
	}

	mr, err := pulls.MergePull(ctx, owner, repo, number, methode)
	if err != nil {
		return MergePRResult{}, err
	}
	return MergePRResult{Merged: mr.Merged, SHA: mr.SHA, Message: mr.Message}, nil
}

// prTekst bouwt titel en beschrijving van de pull request uit de plugins die
// daadwerkelijk zijn bijgewerkt.
func prTekst(bijgewerkt []LocalApplyPlugin) (string, string) {
	titel := fmt.Sprintf("chore(plugins): betaalde plugins bijwerken (%d)", len(bijgewerkt))
	if len(bijgewerkt) == 1 {
		p := bijgewerkt[0]
		van := p.From
		if van == "" {
			van = "nieuw"
		}
		titel = fmt.Sprintf("chore(plugins): update %s %s→%s", p.Slug, van, p.To)
	}

	var b strings.Builder
	b.WriteString("Automatische update van betaalde plugins uit de referentie-installatie, ")
	b.WriteString("aangemaakt met de RDM Sites Tool.\n\n")
	for _, p := range bijgewerkt {
		van := p.From
		if van == "" {
			van = "nieuw"
		}
		fmt.Fprintf(&b, "- `%s`: %s → %s\n", p.Slug, van, p.To)
	}
	b.WriteString("\nAlleen de pluginmappen hierboven zijn vervangen; de rest van het project is ongewijzigd.\n")
	b.WriteString("De commit is met `--no-verify` gemaakt: de inhoud komt uit de referentie-installatie, ")
	b.WriteString("dus de lint-hooks van dit project zijn er niet op van toepassing.\n")
	return titel, b.String()
}

// bronLabel zet Source om in het label dat in het commitbericht komt.
func bronLabel(source string) string {
	if source == "referentie" {
		return "referentie-installatie"
	}
	return "lokale map"
}

// BulkApplyProjectResult is de uitkomst voor één project binnen een
// bulk-update van precies één plugin naar meerdere projecten.
type BulkApplyProjectResult struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	From        string `json:"from"`
	To          string `json:"to"`
	// Status: updated | unchanged | error — zie LocalApplyPlugin.Status.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// Branch is de branch waarop de commit is beland (leeg bij een error). Bij
	// een referentie-plugin is dit per project een eigen, nieuwe branch — zie
	// ensureReferenceBranch — dus dit staat hier bewust per resultaat, niet
	// eenmalig op BulkApplyResult.
	Branch string `json:"branch,omitempty"`
	// Stash beschrijft het werk dat in dít project automatisch geparkeerd is,
	// of is leeg als de werkmap al schoon was. Per project, want elk project
	// heeft zijn eigen werkmap.
	Stash string `json:"stash,omitempty"`
	// PullRequestURL, PullRequestError, PullRequestNumber en CanMerge: zie
	// LocalApplyResult.
	PullRequestURL    string `json:"pullRequestUrl,omitempty"`
	PullRequestError  string `json:"pullRequestError,omitempty"`
	PullRequestNumber int    `json:"pullRequestNumber,omitempty"`
	CanMerge          bool   `json:"canMerge"`
}

// BulkApplyResult is het resultaat van ApplyPluginToProjects.
type BulkApplyResult struct {
	Slug    string                   `json:"slug"`
	Version string                   `json:"version"`
	Results []BulkApplyProjectResult `json:"results"`
}

// ApplyPluginToProjects plaatst en committeert één plugin (uit de losse map of
// de referentie-installatie, referentie wint bij een dubbele slug — zie
// ListLocalPaidPlugins) in meerdere projecten tegelijk. Dit is de omgekeerde
// richting van ApplyLocalPlugins (daar: veel plugins, één project; hier: één
// plugin, veel projecten) — vandaar de gedeelde kern in applyOnePlugin.
//
// Elk project is onafhankelijk: een mislukking in het ene project (bijvoorbeeld
// al iets staged, of een schrijffout) stopt de andere projecten niet.
func (s *PluginService) ApplyPluginToProjects(slug string, projectIDs []string) (BulkApplyResult, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return BulkApplyResult{}, fmt.Errorf("geen plugin opgegeven")
	}
	if len(projectIDs) == 0 {
		return BulkApplyResult{}, fmt.Errorf("geen projecten geselecteerd")
	}

	lijst, err := s.ListLocalPaidPlugins()
	if err != nil {
		return BulkApplyResult{}, err
	}
	var z LocalPaidPlugin
	gevonden := false
	for _, x := range lijst {
		if x.Slug == slug && x.Error == "" {
			z, gevonden = x, true
			break
		}
	}
	if !gevonden {
		return BulkApplyResult{}, fmt.Errorf(
			"geen leesbare bron voor plugin %q (niet in de lokale map en niet in de referentie-installatie)", slug)
	}

	res := BulkApplyResult{Slug: slug, Version: z.Version}
	for _, projectID := range projectIDs {
		naam := projectID
		if p, ok := s.projects.Get(projectID); ok {
			naam = p.DisplayName
		}
		r, doel := s.applyOnePluginGuarded(projectID, z)
		rij := BulkApplyProjectResult{
			ProjectID: projectID, ProjectName: naam,
			From: r.From, To: r.To, Status: r.Status, Error: r.Error,
			Branch: doel.Branch, Stash: doel.Stash,
		}
		// Per project pushen en een eigen PR openen: elk project is een eigen
		// repo, dus één gedeelde PR bestaat hier niet.
		if z.Source == "referentie" && r.Status == "updated" {
			if uit, prErr := s.pushEnOpenPR(projectID, doel.Branch, []LocalApplyPlugin{r}); prErr != nil {
				rij.PullRequestError = prErr.Error()
			} else {
				rij.PullRequestURL, rij.PullRequestNumber, rij.CanMerge = uit.URL, uit.Nummer, uit.CanMerge
			}
		}
		res.Results = append(res.Results, rij)
	}
	return res, nil
}

// rePluginVersion pakt de Version-regel uit een WordPress-pluginheader.
var rePluginVersion = regexp.MustCompile(`(?mi)^\s*\*?\s*Version:\s*([^\s*]+)`)

// rePluginName herkent het hoofdbestand: dat draagt de Plugin Name-header.
var rePluginName = regexp.MustCompile(`(?mi)^\s*\*?\s*Plugin Name:\s*\S`)

// leesPluginZipInfo determines slug and version from a plugin zip: the single
// top-level directory is the slug, and the version comes from the header of the
// PHP file that carries "Plugin Name:". Both wp.org and paid vendors ship this
// shape; anything else is not installable as a plugin and is reported as such.
func leesPluginZipInfo(zipData []byte) (slug, version string, err error) {
	zr, zerr := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if zerr != nil {
		return "", "", fmt.Errorf("geen leesbare zip: %w", zerr)
	}

	topmappen := map[string]bool{}
	for _, f := range zr.File {
		naam := strings.TrimPrefix(f.Name, "./")
		if i := strings.IndexByte(naam, '/'); i > 0 {
			topmappen[naam[:i]] = true
		}
	}
	if len(topmappen) != 1 {
		return "", "", fmt.Errorf("zip heeft geen enkele topmap (gevonden: %d); dit is geen pluginzip", len(topmappen))
	}
	for m := range topmappen {
		slug = m
	}

	// Alleen php-bestanden dírect in de topmap: daar hoort de header te staan.
	for _, f := range zr.File {
		naam := strings.TrimPrefix(f.Name, "./")
		if path.Dir(naam) != slug || !strings.EqualFold(path.Ext(naam), ".php") {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			continue
		}
		kop, _ := io.ReadAll(io.LimitReader(rc, 8192))
		rc.Close()
		if !rePluginName.Match(kop) {
			continue
		}
		if m := rePluginVersion.FindSubmatch(kop); m != nil {
			return slug, strings.TrimSpace(string(m[1])), nil
		}
	}
	return "", "", fmt.Errorf("geen pluginheader (Plugin Name + Version) gevonden in %s/", slug)
}

// leesPluginDirInfo reads the version from an unpacked plugin folder: the PHP file
// directly in the folder that carries "Plugin Name:" also carries the version. The
// folder name is the slug, same convention as a zip's top-level directory.
func leesPluginDirInfo(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("map lezen: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".php") {
			continue
		}
		f, oerr := os.Open(filepath.Join(dir, e.Name()))
		if oerr != nil {
			continue
		}
		kop, _ := io.ReadAll(io.LimitReader(f, 8192))
		f.Close()
		if !rePluginName.Match(kop) {
			continue
		}
		if m := rePluginVersion.FindSubmatch(kop); m != nil {
			return strings.TrimSpace(string(m[1])), nil
		}
	}
	return "", fmt.Errorf("geen pluginheader (Plugin Name + Version) gevonden in %s/", filepath.Base(dir))
}

// kopieerMapReplace copies an unpacked plugin folder into pluginsDir/<slug> with
// the same discipline as extractZipReplace: stage the full copy first, only then
// swap the old folder out. macOS droppings (.DS_Store), VCS folders and symlinks
// stay behind — those belong to the source machine, not in a customer repo.
func kopieerMapReplace(srcDir, pluginsDir, slug string) error {
	src, err := os.Stat(srcDir)
	if err != nil || !src.IsDir() {
		return fmt.Errorf("bronmap ontbreekt: %s", srcDir)
	}

	temp, err := os.MkdirTemp(pluginsDir, "."+slug+".tmp-copy-")
	if err != nil {
		return fmt.Errorf("tijdelijke map aanmaken: %w", err)
	}
	defer os.RemoveAll(temp)
	staged := filepath.Join(temp, slug)

	err = filepath.WalkDir(srcDir, func(pad string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		naam := d.Name()
		if naam == ".git" || naam == ".svn" || naam == "node_modules" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if naam == ".DS_Store" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // symlinks kunnen buiten de bron wijzen; overslaan is veilig
		}
		rel, rerr := filepath.Rel(srcDir, pad)
		if rerr != nil {
			return rerr
		}
		dest := filepath.Join(staged, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return kopieerBestand(pad, dest)
	})
	if err != nil {
		return fmt.Errorf("kopiëren: %w", err)
	}

	// Zelfde slot als extractZipReplace: pas als de kopie compleet klaarstaat gaat
	// de oude map weg.
	target := filepath.Join(pluginsDir, slug)
	if info, err := os.Stat(staged); err != nil || !info.IsDir() {
		return fmt.Errorf("kopie is niet compleet klaargezet")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("oude plugin verwijderen: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		return fmt.Errorf("kopie op zijn plek zetten: %w", err)
	}
	return nil
}

func kopieerBestand(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
