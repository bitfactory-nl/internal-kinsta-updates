package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

type importFix struct {
	svc        *ImportService
	cfg        *config.Global
	sec        *wpNepSecrets
	root       string
	sitesPath  string
	localsPath string
}

func importFixture(t *testing.T) importFix {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "Projects")
	mk := func(name, deploy, rdm string) {
		d := filepath.Join(root, name)
		os.MkdirAll(d, 0o755)
		if deploy != "" {
			os.WriteFile(filepath.Join(d, "deploy_conf.json"), []byte(deploy), 0o644)
		}
		if rdm != "" {
			os.MkdirAll(filepath.Join(d, ".rdm"), 0o755)
			os.WriteFile(filepath.Join(d, ".rdm", "config.yml"), []byte(rdm), 0o644)
		}
	}
	mk("web-bnl", `{"type":"wordpress_kinsta","link":{"prod":"https://b.nl"}}`, "kinsta:\n  site_id: k-b\n")
	mk("web-anl", `{"type":"wordpress_transip","link":{"prod":"https://a.nl"}}`, "")
	mk("tool-laravel", `{"type":"laravel_9"}`, "")
	sitesPath, localsPath := config.WPSitesPath(), config.LocalProjectsPath()
	ps := NewProjectService(sitesPath, localsPath)
	sec := &wpNepSecrets{m: map[string]string{}}
	wp := NewWPSiteService(sitesPath, localsPath, ps, sec)
	cfg := &config.Global{ProjectsRoots: []string{root}}
	rs := NewRegisterService(ps, wp, localsPath)
	return importFix{svc: NewImportService(cfg, wp, rs, sitesPath, localsPath), cfg: cfg, sec: sec, root: root, sitesPath: sitesPath, localsPath: localsPath}
}

func TestImportNeededEnCandidates(t *testing.T) {
	f := importFixture(t)
	need, err := f.svc.Needed()
	if err != nil || !need {
		t.Fatalf("needed %v %v", need, err)
	}
	c, err := f.svc.Candidates()
	if err != nil || len(c) != 3 {
		t.Fatalf("candidates %+v %v", c, err)
	}
	if c[0].Name != "tool-laravel" || c[0].WordPress {
		t.Fatalf("sortering/vlag: %+v", c[0])
	}
	if c[1].Name != "web-anl" || !c[1].WordPress || c[1].URL != "https://a.nl" {
		t.Fatalf("anl %+v", c[1])
	}
	if c[2].KinstaSiteID != "k-b" || c[2].DeployType != "wordpress_kinsta" {
		t.Fatalf("bnl %+v", c[2])
	}
}

func TestImportApply(t *testing.T) {
	f := importFixture(t)
	res, err := f.svc.Apply([]ImportChoice{
		{Path: filepath.Join(f.root, "web-bnl"), AsSite: true},
		{Path: filepath.Join(f.root, "tool-laravel"), AsSite: false},
	})
	if err != nil || res.Added != 2 || len(res.Notes) != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
	sites, _ := config.LoadWPSites(f.sitesPath)
	if len(sites) != 1 || sites[0].ID != "web-bnl" || sites[0].URL != "https://b.nl" || sites[0].KinstaSiteID != "k-b" || sites[0].Username != "mcp-bot" {
		t.Fatalf("sites %+v", sites)
	}
	if len(f.sec.m) != 0 {
		t.Fatalf("er mag geen Keychain-item zijn: %v", f.sec.m)
	}
	locals, _ := config.LoadLocalProjects(f.localsPath)
	if len(locals) != 1 {
		t.Fatalf("locals %+v", locals)
	}
	if !f.cfg.ImportDone {
		t.Fatal("ImportDone niet gezet")
	}
	if need, _ := f.svc.Needed(); need {
		t.Fatal("Needed moet nu false zijn")
	}
}

func TestImportOverslaan(t *testing.T) {
	f := importFixture(t)
	if res, err := f.svc.Apply(nil); err != nil || res.Added != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
	if need, _ := f.svc.Needed(); need {
		t.Fatal("na overslaan geen import meer")
	}
	g, _ := config.LoadGlobal()
	if !g.ImportDone {
		t.Fatal("import_done niet opgeslagen")
	}
}

func TestImportIdBotsing(t *testing.T) {
	f := importFixture(t)
	// Tweede map die hetzelfde id-voorstel oplevert als web-bnl.
	d := filepath.Join(t.TempDir(), "web-bnl")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "deploy_conf.json"), []byte(`{"type":"wordpress_kinsta","link":{"prod":"https://b2.nl"}}`), 0o644)
	_, err := f.svc.Apply([]ImportChoice{{Path: filepath.Join(f.root, "web-bnl"), AsSite: true}, {Path: d, AsSite: true}})
	if err != nil {
		t.Fatal(err)
	}
	sites, _ := config.LoadWPSites(f.sitesPath)
	if len(sites) != 2 || sites[1].ID != "web-bnl-2" {
		t.Fatalf("sites %+v", sites)
	}
}

// Een site uit een eerdere feature staat al in wp-sites.yml: de import blijft
// nodig en die map wordt niet nogmaals als kandidaat aangeboden.
func TestImportMetBestaandeSite(t *testing.T) {
	f := importFixture(t)
	bestaand := filepath.Join(f.root, "web-anl")
	err := config.SaveWPSites(f.sitesPath, []domain.WPSite{{
		ID: "anl", Name: "A", URL: "https://a.nl", Username: "mcp-bot", ProjectPath: bestaand + "/",
	}})
	if err != nil {
		t.Fatal(err)
	}
	need, err := f.svc.Needed()
	if err != nil || !need {
		t.Fatalf("needed %v %v", need, err)
	}
	c, err := f.svc.Candidates()
	if err != nil || len(c) != 2 {
		t.Fatalf("candidates %+v %v", c, err)
	}
	for _, k := range c {
		if filepath.Clean(k.Path) == bestaand {
			t.Fatalf("al geregistreerde map is kandidaat: %+v", k)
		}
	}
}

// telHerladen telt hoe vaak de projectlijst wordt herladen.
type telHerladen struct {
	*ProjectService
	n int
}

func (t *telHerladen) Reload() ([]domain.Project, error) {
	t.n++
	return t.ProjectService.Reload()
}

// Apply herlaadt de projectlijst één keer, aan het eind, hoeveel items er ook zijn.
func TestImportApplyHerlaadtEenKeer(t *testing.T) {
	f := importFixture(t)
	teller := &telHerladen{ProjectService: f.svc.register.projects}
	f.svc.sites.projects = teller
	_, err := f.svc.Apply([]ImportChoice{
		{Path: filepath.Join(f.root, "web-bnl"), AsSite: true},
		{Path: filepath.Join(f.root, "web-anl"), AsSite: true},
		{Path: filepath.Join(f.root, "tool-laravel"), AsSite: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if teller.n != 1 {
		t.Fatalf("%d keer herladen, verwacht 1", teller.n)
	}
	if got := len(f.svc.register.projects.List()); got != 3 {
		t.Fatalf("%d projecten na import", got)
	}
}

// Een onderbroken import (lokale projecten, maar import_done nog niet gezet)
// gaat verder: Needed blijft waar en al geregistreerde mappen zijn geen kandidaat.
func TestImportOnderbrokenGaatVerder(t *testing.T) {
	f := importFixture(t)
	laravel := filepath.Join(f.root, "tool-laravel")
	if err := config.SaveLocalProjects(f.localsPath, []domain.LocalProject{{Path: laravel}}); err != nil {
		t.Fatal(err)
	}
	need, err := f.svc.Needed()
	if err != nil || !need {
		t.Fatalf("needed %v %v", need, err)
	}
	c, err := f.svc.Candidates()
	if err != nil || len(c) != 2 {
		t.Fatalf("candidates %+v %v", c, err)
	}
	for _, k := range c {
		if filepath.Clean(k.Path) == laravel {
			t.Fatalf("geregistreerd lokaal project is kandidaat: %+v", k)
		}
	}
}

// Lukt het opslaan van import_done niet, dan blijft de vlag in het geheugen uit
// en komt de fout terug.
func TestImportDoneAlleenNaOpslaan(t *testing.T) {
	f := importFixture(t)
	// Een map op de plek van config.yml laat SaveGlobal falen.
	if err := os.MkdirAll(config.GlobalConfigPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Apply(nil); err == nil {
		t.Fatal("verwacht een fout van SaveGlobal")
	}
	if f.cfg.ImportDone {
		t.Fatal("ImportDone hoort false te blijven")
	}
}

// Een WordPress-map zonder geldige URL wordt geen site maar een lokaal
// project, met een notitie in het resultaat in plaats van een fout.
func TestImportZonderURLWordtLokaal(t *testing.T) {
	f := importFixture(t)
	leeg := filepath.Join(f.root, "web-leeg")
	os.MkdirAll(leeg, 0o755)
	os.WriteFile(filepath.Join(leeg, "deploy_conf.json"), []byte(`{"type":"wordpress_kinsta"}`), 0o644)
	res, err := f.svc.Apply([]ImportChoice{{Path: leeg, AsSite: true}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || len(res.Notes) != 1 {
		t.Fatalf("resultaat %+v", res)
	}
	sites, _ := config.LoadWPSites(f.sitesPath)
	if len(sites) != 0 {
		t.Fatalf("hoort geen site te worden: %+v", sites)
	}
	locals, _ := config.LoadLocalProjects(f.localsPath)
	if len(locals) != 1 || filepath.Clean(locals[0].Path) != leeg {
		t.Fatalf("locals %+v", locals)
	}
}
