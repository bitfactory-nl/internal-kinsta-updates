package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

// schrijfWPCore legt een minimale maar echte WordPress-webroot neer: de twee
// core-mappen, een version.php met het versienummer, een core-rootbestand en
// een wp-config.php + wp-content die NIET aangeraakt mogen worden.
func schrijfWPCore(t *testing.T, projectDir, versie string) {
	t.Helper()
	wpRoot := filepath.Join(projectDir, "public")
	for _, dir := range []string{"wp-admin", "wp-includes"} {
		if err := os.MkdirAll(filepath.Join(wpRoot, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	schrijf := func(rel, inhoud string) {
		pad := filepath.Join(wpRoot, rel)
		if err := os.MkdirAll(filepath.Dir(pad), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pad, []byte(inhoud), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	schrijf("wp-includes/version.php", "<?php\n$wp_version = '"+versie+"';\n")
	schrijf("wp-admin/admin.php", "<?php // core "+versie+"\n")
	schrijf("index.php", "<?php // core index "+versie+"\n")
	// Projecteigen bestanden die core-updates niet mogen aanraken.
	schrijf("wp-config.php", "<?php // GEHEIME PROJECTCONFIG\n")
	schrijf("wp-content/themes/eigen-thema/style.css", "/* eigen thema */\n")
}

// bulkProject maakt één WordPress-project met git-repo, een bare origin en een
// deploy_conf.json die het als WordPress markeert.
func bulkProject(t *testing.T, root, naam, coreVersie string, plugins map[string]string) string {
	t.Helper()
	projectDir := filepath.Join(root, naam)
	schrijfWPCore(t, projectDir, coreVersie)
	pluginsDir := filepath.Join(projectDir, "public", "wp-content", "plugins")
	for slug, versie := range plugins {
		lokalePluginMap(t, pluginsDir, slug, versie)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "deploy_conf.json"),
		[]byte(`{"type":"wordpress_kinsta","link":{"prod":"https://`+naam+`.nl"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	runGitApply(t, projectDir, "init")
	runGitApply(t, projectDir, "checkout", "-b", "release/1.0.x")
	runGitApply(t, projectDir, "config", "user.email", "test@example.com")
	runGitApply(t, projectDir, "config", "user.name", "Test")
	runGitApply(t, projectDir, "add", "-A")
	runGitApply(t, projectDir, "commit", "-m", "initial")
	return projectDir
}

// bulkOpstelling zet een referentie-installatie en n projecten klaar, met een
// echte bare origin per project (via pushInsteadOf, zoals bij de PR-tests).
func bulkOpstelling(t *testing.T, refCore string, refPlugins map[string]string) (svc *BulkUpdateService, ps *ProjectService, refDir string, root string) {
	t.Helper()
	refDir = t.TempDir()
	schrijfWPCore(t, refDir, refCore)
	// Een referentie-installatie heeft altijd een pluginmap, ook als er in deze
	// test geen plugins in staan.
	if err := os.MkdirAll(filepath.Join(refDir, "public", "wp-content", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	for slug, versie := range refPlugins {
		lokalePluginMap(t, filepath.Join(refDir, "public", "wp-content", "plugins"), slug, versie)
	}
	root = t.TempDir()
	return nil, nil, refDir, root
}

// bulkService bouwt de service nadat de projecten onder root staan.
func bulkService(t *testing.T, refDir, root string, pulls pluginPulls) (*BulkUpdateService, *ProjectService) {
	t.Helper()
	ps := NewProjectService([]string{root})
	if _, err := ps.Scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	cfg := &config.Global{}
	cfg.PluginRepo.ReferenceProjectPath = refDir
	git := NewGitService(ps)
	plugin := &PluginService{cfg: cfg, projects: ps, git: git, pulls: pulls}
	return NewBulkUpdateService(cfg, ps, git, plugin), ps
}

func projectIDVoor(t *testing.T, ps *ProjectService, dir string) string {
	t.Helper()
	for _, p := range ps.List() {
		if p.Path == dir {
			return p.ID
		}
	}
	t.Fatalf("project %q niet gevonden na scan", dir)
	return ""
}

// TestBulkUpdatePlanZietPluginsEnCore is de voorbeschouwing: wat zou er
// gebeuren. Niets mag gewijzigd worden.
func TestBulkUpdatePlanZietPluginsEnCore(t *testing.T) {
	_, _, refDir, root := bulkOpstelling(t, "7.1", map[string]string{"acf-pro": "6.4.1", "gravityforms": "2.9.1"})
	achter := bulkProject(t, root, "site-achter", "6.8", map[string]string{"acf-pro": "6.2.0", "gravityforms": "2.9.1"})
	bij := bulkProject(t, root, "site-bij", "7.1", map[string]string{"acf-pro": "6.4.1"})

	svc, ps := bulkService(t, refDir, root, &fakePluginPulls{})
	voorHash := runGitApply(t, achter, "rev-parse", "HEAD")

	plan, err := svc.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.ReferenceCore != "7.1" {
		t.Errorf("ReferenceCore = %q, wil 7.1", plan.ReferenceCore)
	}

	perProject := map[string]BulkUpdateProjectPlan{}
	for _, r := range plan.Projects {
		perProject[r.ProjectName] = r
	}

	a := perProject["site-achter"]
	if !a.CoreOutdated || a.CoreFrom != "6.8" || a.CoreTo != "7.1" {
		t.Errorf("site-achter core = %+v", a)
	}
	if len(a.Plugins) != 1 || a.Plugins[0].Slug != "acf-pro" || a.Plugins[0].From != "6.2.0" || a.Plugins[0].To != "6.4.1" {
		t.Errorf("site-achter plugins = %+v, wil alleen de verouderde acf-pro", a.Plugins)
	}
	if a.Branch != "release/1.0.x" {
		t.Errorf("site-achter Branch = %q", a.Branch)
	}

	b := perProject["site-bij"]
	if b.CoreOutdated || len(b.Plugins) != 0 || b.Skip != "al bij" {
		t.Errorf("site-bij = %+v, wil niets te doen", b)
	}

	// Plan verandert niets.
	if naHash := runGitApply(t, achter, "rev-parse", "HEAD"); naHash != voorHash {
		t.Error("Plan heeft iets gecommit")
	}
	if status := runGitApply(t, achter, "status", "--porcelain"); status != "" {
		t.Errorf("Plan heeft de werkmap aangeraakt:\n%s", status)
	}
	_ = ps
	_ = bij
}

// TestBulkUpdatePlanSluitReferentieEnNietWordPressUit: de
// referentie-installatie is geen klantsite, en een niet-WordPress-project hoort
// hier niet in.
func TestBulkUpdatePlanSluitReferentieEnNietWordPressUit(t *testing.T) {
	root := t.TempDir()
	// De referentie-installatie staat zelf óók onder de projecten-root en is
	// een geldig WordPress-project: precies de situatie op de echte machine.
	refDir := filepath.Join(root, "internal-wordpress-paid-plugins")
	bulkProject(t, root, "internal-wordpress-paid-plugins", "7.1", map[string]string{"acf-pro": "6.4.1"})
	bulkProject(t, root, "site-wp", "6.8", map[string]string{"acf-pro": "6.2.0"})

	// Een Laravel-project: geen WordPress, hoort niet mee te doen.
	laravel := filepath.Join(root, "app-laravel")
	if err := os.MkdirAll(laravel, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(laravel, "deploy_conf.json"), []byte(`{"type":"laravel_9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitApply(t, laravel, "init")
	runGitApply(t, laravel, "config", "user.email", "t@e.nl")
	runGitApply(t, laravel, "config", "user.name", "T")
	runGitApply(t, laravel, "add", "-A")
	runGitApply(t, laravel, "commit", "-m", "init")

	svc, _ := bulkService(t, refDir, root, &fakePluginPulls{})
	plan, err := svc.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, r := range plan.Projects {
		if r.ProjectName == "internal-wordpress-paid-plugins" {
			t.Error("de referentie-installatie staat in het plan als klantsite")
		}
		if r.ProjectName == "app-laravel" {
			t.Error("een Laravel-project staat in het plan")
		}
	}
	if len(plan.Projects) != 1 || plan.Projects[0].ProjectName != "site-wp" {
		t.Errorf("plan = %+v, wil alleen site-wp", plan.Projects)
	}
}

// TestBulkUpdateRunVolledigePijplijn is de kern: stash → branch vanaf de verse
// remote → plugin-commit → core-commit → push → PR, en daarna de checkout terug
// waar hij stond met het geparkeerde werk erop.
func TestBulkUpdateRunVolledigePijplijn(t *testing.T) {
	_, _, refDir, root := bulkOpstelling(t, "7.1", map[string]string{"acf-pro": "6.4.1"})
	projectDir := bulkProject(t, root, "site-achter", "6.8", map[string]string{"acf-pro": "6.2.0"})
	bare := metPushDoelwit(t, projectDir)

	// Openstaand werk: dit moet gestasht worden en na de run terugkomen.
	if err := os.WriteFile(filepath.Join(projectDir, "wip.txt"), []byte("mijn werk"), 0o644); err != nil {
		t.Fatal(err)
	}

	nep := &fakePluginPulls{}
	svc, ps := bulkService(t, refDir, root, nep)
	id := projectIDVoor(t, ps, projectDir)

	res, err := svc.Run([]string{id})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Projects) != 1 {
		t.Fatalf("projects = %+v", res.Projects)
	}
	r := res.Projects[0]
	if r.Status != "updated" {
		t.Fatalf("Status = %q, error = %q, coreError = %q", r.Status, r.Error, r.CoreError)
	}

	// Stash gemaakt én gemeld.
	if r.Stash == "" {
		t.Error("openstaand werk is niet gemeld als stash")
	}
	// Branch met de juiste naam, afgetakt van de remote-stand.
	if !strings.HasPrefix(r.Branch, "chore/wp-updates-") {
		t.Errorf("Branch = %q", r.Branch)
	}
	if r.VanafRef != "origin/release/1.0.x" {
		t.Errorf("VanafRef = %q, wil origin/release/1.0.x (de verse remote-stand)", r.VanafRef)
	}
	// Plugin en core beide bijgewerkt.
	if len(r.Plugins) != 1 || r.Plugins[0].Status != "updated" || r.Plugins[0].To != "6.4.1" {
		t.Errorf("plugins = %+v", r.Plugins)
	}
	if r.CoreStatus != "updated" || r.CoreFrom != "6.8" || r.CoreTo != "7.1" {
		t.Errorf("core = %s %s→%s (%s)", r.CoreStatus, r.CoreFrom, r.CoreTo, r.CoreError)
	}
	// PR aangemaakt, met core én plugins in de tekst.
	if r.PullRequestURL == "" || r.PullRequestNumber == 0 {
		t.Errorf("geen PR: url=%q nr=%d fout=%q", r.PullRequestURL, r.PullRequestNumber, r.PullRequestError)
	}
	if !strings.Contains(nep.titel, "WordPress 6.8→7.1") || !strings.Contains(nep.titel, "plugin") {
		t.Errorf("PR-titel = %q, wil core én plugins", nep.titel)
	}
	if !strings.Contains(nep.body, "acf-pro") || !strings.Contains(nep.body, "origin/release/1.0.x") {
		t.Errorf("PR-body = %q", nep.body)
	}

	// De branch staat op de remote.
	if b := runGitApply(t, projectDir, "--git-dir="+bare, "branch", "--list"); !strings.Contains(b, r.Branch) {
		t.Errorf("remote branches = %q, wil %q", b, r.Branch)
	}

	// De checkout is terug waar hij stond, met het werk erop.
	if r.RestoredBranch != "release/1.0.x" || !r.StashRestored {
		t.Errorf("herstel: branch=%q stash=%v fout=%q", r.RestoredBranch, r.StashRestored, r.RestoreError)
	}
	if huidig := runGitApply(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD"); huidig != "release/1.0.x" {
		t.Errorf("checkout staat op %q, wil release/1.0.x", huidig)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "wip.txt")); err != nil {
		t.Errorf("het geparkeerde werk is niet teruggekomen: %v", err)
	}
	// De release-branch zelf is niet verschoven.
	if runGitApply(t, projectDir, "rev-parse", "release/1.0.x") != runGitApply(t, projectDir, "rev-parse", "origin/release/1.0.x") {
		t.Error("release/1.0.x is verschoven; die hoort onaangeraakt te blijven")
	}
}

// TestBulkUpdateRunCoreRaaktProjectbestandenNiet is de belangrijkste
// veiligheidscheck: een core-update mag wp-config.php en wp-content niet
// aanraken.
func TestBulkUpdateRunCoreRaaktProjectbestandenNiet(t *testing.T) {
	_, _, refDir, root := bulkOpstelling(t, "7.1", nil)
	projectDir := bulkProject(t, root, "site-achter", "6.8", nil)
	metPushDoelwit(t, projectDir)

	// De referentie heeft een ándere wp-config en een ánder eigen thema: die
	// mogen niet naar het project overwaaien.
	if err := os.WriteFile(filepath.Join(refDir, "public", "wp-config.php"),
		[]byte("<?php // REFERENTIE-CONFIG\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, ps := bulkService(t, refDir, root, &fakePluginPulls{})
	id := projectIDVoor(t, ps, projectDir)

	res, err := svc.Run([]string{id})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	r := res.Projects[0]
	if r.CoreStatus != "updated" {
		t.Fatalf("core niet bijgewerkt: %+v", r)
	}

	// Na de run staat de checkout weer op de oude branch, dus de bestanden op
	// schijf zijn de oude. Wat telt is wat er op de update-branch is gecommit —
	// dat is wat in de PR terechtkomt.
	toon := func(pad string) string {
		return runGitApply(t, projectDir, "show", r.Branch+":"+pad)
	}

	// De core op de branch is de nieuwe versie.
	if v := toon("public/wp-includes/version.php"); !strings.Contains(v, "7.1") {
		t.Errorf("core op %s = %q, wil 7.1", r.Branch, v)
	}
	// wp-config.php is nog van het project, niet die van de referentie.
	if c := toon("public/wp-config.php"); !strings.Contains(c, "GEHEIME PROJECTCONFIG") {
		t.Errorf("wp-config.php op de branch is overschreven met: %s", c)
	}
	// Het eigen thema staat er nog.
	if th := toon("public/wp-content/themes/eigen-thema/style.css"); !strings.Contains(th, "eigen thema") {
		t.Errorf("eigen thema op de branch = %q", th)
	}

	// En op de oude branch is niets veranderd.
	if v := leesWPVersie(filepath.Join(projectDir, "public")); v != "6.8" {
		t.Errorf("werkmap-core = %q; de oude branch hoort onaangeraakt te zijn", v)
	}
}

// TestBulkUpdateRunIsoleertFoutPerProject: een project dat niet lukt mag de
// andere niet tegenhouden.
func TestBulkUpdateRunIsoleertFoutPerProject(t *testing.T) {
	_, _, refDir, root := bulkOpstelling(t, "7.1", map[string]string{"acf-pro": "6.4.1"})
	goedDir := bulkProject(t, root, "site-goed", "6.8", map[string]string{"acf-pro": "6.2.0"})
	metPushDoelwit(t, goedDir)

	svc, ps := bulkService(t, refDir, root, &fakePluginPulls{})
	goed := projectIDVoor(t, ps, goedDir)

	res, err := svc.Run([]string{"bestaat-niet", goed})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	perID := map[string]BulkUpdateProjectResult{}
	for _, r := range res.Projects {
		perID[r.ProjectID] = r
	}
	if perID["bestaat-niet"].Status != "error" {
		t.Errorf("onbekend project = %+v, wil error", perID["bestaat-niet"])
	}
	if perID[goed].Status != "updated" {
		t.Errorf("site-goed = %+v, wil updated", perID[goed])
	}
}

// TestBulkUpdateRunProjectDatAlBijIsCommitNiets: geen commit, geen branch, geen
// PR voor een project dat al op de referentie-versies staat.
func TestBulkUpdateRunProjectDatAlBijIsCommitNiets(t *testing.T) {
	_, _, refDir, root := bulkOpstelling(t, "7.1", map[string]string{"acf-pro": "6.4.1"})
	projectDir := bulkProject(t, root, "site-bij", "7.1", map[string]string{"acf-pro": "6.4.1"})
	metPushDoelwit(t, projectDir)

	nep := &fakePluginPulls{}
	svc, ps := bulkService(t, refDir, root, nep)
	id := projectIDVoor(t, ps, projectDir)
	voorHash := runGitApply(t, projectDir, "rev-parse", "HEAD")

	res, err := svc.Run([]string{id})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	r := res.Projects[0]
	if r.Status != "nothing" {
		t.Errorf("Status = %q, wil nothing", r.Status)
	}
	if r.Branch != "" {
		t.Errorf("er is een branch gemaakt voor een project dat al bij is: %q", r.Branch)
	}
	if nep.aangemaakt != 0 {
		t.Error("er is een PR aangemaakt voor een project dat al bij is")
	}
	if naHash := runGitApply(t, projectDir, "rev-parse", "HEAD"); naHash != voorHash {
		t.Error("er is gecommit in een project dat al bij is")
	}
	if huidig := runGitApply(t, projectDir, "rev-parse", "--abbrev-ref", "HEAD"); huidig != "release/1.0.x" {
		t.Errorf("checkout staat op %q", huidig)
	}
}

// TestBulkUpdateRunGeenReferentieIngesteld: zonder referentie-installatie kan
// deze pagina niets, en dat moet een duidelijke fout zijn.
func TestBulkUpdateRunGeenReferentieIngesteld(t *testing.T) {
	ps := NewProjectService([]string{t.TempDir()})
	svc := NewBulkUpdateService(&config.Global{}, ps, NewGitService(ps), &PluginService{cfg: &config.Global{}})
	if _, err := svc.Plan(); err == nil {
		t.Error("Plan zonder referentie-installatie hoort te falen")
	}
	if _, err := svc.Run([]string{"p1"}); err == nil {
		t.Error("Run zonder referentie-installatie hoort te falen")
	}
}
