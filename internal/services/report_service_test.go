package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/endoflife"
	"github.com/rdm/sites-tool/internal/adapters/kinsta"
	"github.com/rdm/sites-tool/internal/domain"
)

// --- fakes ---

type fakeReportProjects struct {
	projects map[string]domain.Project
}

func (f *fakeReportProjects) Get(id string) (domain.Project, bool) {
	p, ok := f.projects[id]
	return p, ok
}

type fakeReportKinsta struct {
	details *kinsta.SiteDetails
	detErr  error
	envs    *kinsta.EnvironmentDetails
	envErr  error
}

func (f *fakeReportKinsta) GetSiteDetails(siteID string) (*kinsta.SiteDetails, error) {
	return f.details, f.detErr
}

func (f *fakeReportKinsta) GetEnvironmentPluginsAndThemes(envID string) (*kinsta.EnvironmentDetails, error) {
	return f.envs, f.envErr
}

type fakeReportSecurity struct {
	result *SecurityScanResult
	err    error
}

func (f *fakeReportSecurity) GetScanResults(projectID string) (*SecurityScanResult, error) {
	return f.result, f.err
}

type fakeReportPDF struct {
	called  bool
	lastOut string
	err     error
}

func (f *fakeReportPDF) RenderPDF(_ context.Context, _, outPath string) error {
	f.called = true
	f.lastOut = outPath
	return f.err
}

func testProject(id string) domain.Project {
	return domain.Project{
		ID:          id,
		DisplayName: "Cefetra",
		Deploy:      domain.DeployConf{Link: domain.DeployLinks{Prod: "https://cefetra.nl"}},
		Config: domain.ProjectConfig{
			Kinsta: &domain.KinstaProjectCfg{
				SiteID: "site1",
				Environments: map[string]domain.KinstaEnvBinding{
					"production": {EnvID: "env-prod"},
					"staging":    {EnvID: "env-staging"},
				},
			},
		},
	}
}

// --- GetReport ---

func TestGetReportSkeletonWhenNoDraftStored(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	svc := NewReportService(projects, nil, nil, NewReportStore(dir), nil, nil, nil)

	r, err := svc.GetReport("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if r.ClientName != "Cefetra" {
		t.Fatalf("expected ClientName prefilled from DisplayName, got %q", r.ClientName)
	}
	if r.WebsiteName != "cefetra.nl" {
		t.Fatalf("expected WebsiteName hostname, got %q", r.WebsiteName)
	}
	if len(r.Monitoring) != 3 {
		t.Fatalf("expected 3 default monitoring rows, got %d", len(r.Monitoring))
	}
	for _, row := range r.Monitoring {
		if row.Status != "✔ OK" {
			t.Fatalf("expected default OK status, got %+v", row)
		}
	}
	if len(r.Software) != 5 || len(r.DependencyUpdates) != 2 || len(r.WPUpdates) != 2 {
		t.Fatalf("unexpected default row counts: %+v", r)
	}
	wantComponents := []string{compPHPProd, compPHPLocal, compMariaDB, compNode, compWordPress}
	for i, want := range wantComponents {
		if r.Software[i].Component != want {
			t.Fatalf("software rij %d = %q, want %q", i, r.Software[i].Component, want)
		}
	}
}

func TestMigrateSoftwareRowsSplitsPHP(t *testing.T) {
	oud := []domain.SoftwareRow{
		{Component: "PHP", Huidig: "8.2", Opmerking: "handmatig"},
		{Component: "MariaDB"},
	}
	rows := migrateSoftwareRows(oud)
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}
	if rows[0].Component != compPHPProd || rows[0].Huidig != "8.2" || rows[0].Opmerking != "handmatig" {
		t.Errorf("rij 0 = %+v, want hernoemde PHP-rij met behoud van waarden", rows[0])
	}
	if rows[1].Component != compPHPLocal {
		t.Errorf("rij 1 = %+v, want ingevoegde PHP (lokaal)", rows[1])
	}

	// Idempotent: nogmaals migreren verandert niets.
	again := migrateSoftwareRows(rows)
	if len(again) != 3 || again[1].Component != compPHPLocal {
		t.Errorf("migratie is niet idempotent: %+v", again)
	}
}

func TestGetReportReturnsStoredDraft(t *testing.T) {
	dir := t.TempDir()
	store := NewReportStore(dir)
	if err := store.Save(domain.Report{ProjectID: "p1", Period: "Q2 2026", ClientName: "Custom Name"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	svc := NewReportService(projects, nil, nil, store, nil, nil, nil)

	r, err := svc.GetReport("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if r.ClientName != "Custom Name" {
		t.Fatalf("expected stored draft to win, got %q", r.ClientName)
	}
}

func TestGetReportUnknownProjectErrors(t *testing.T) {
	dir := t.TempDir()
	svc := NewReportService(&fakeReportProjects{projects: map[string]domain.Project{}}, nil, nil, NewReportStore(dir), nil, nil, nil)
	if _, err := svc.GetReport("nope", "Q2 2026"); err == nil {
		t.Fatal("expected error for unknown project")
	}
}

// --- Prefill ---

func TestPrefillFillsSoftwareVersionsAndUpdateCounts(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	kinstaFake := &fakeReportKinsta{
		details: &kinsta.SiteDetails{
			Environments: []kinsta.Environment{
				{ID: "env-prod", WordPressVersion: "6.5.2", ContainerInfo: kinsta.ContainerInfo{PHPEngineVersion: "php8.3"}},
				{ID: "env-staging", WordPressVersion: "6.0.0"},
			},
		},
		envs: &kinsta.EnvironmentDetails{
			Plugins: []kinsta.Plugin{
				{Name: "a", UpdateVersion: "2.0"},
				{Name: "b", UpdateVersion: ""},
				{Name: "c", UpdateVersion: "1.2", IsVersionVulnerable: true},
			},
		},
	}
	svc := NewReportService(projects, kinstaFake, nil, NewReportStore(dir), nil, nil, nil)

	r, err := svc.Prefill("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("Prefill: %v", err)
	}

	var php, wp string
	for _, row := range r.Software {
		if row.Component == compPHPProd {
			php = row.Huidig
		}
		if row.Component == compWordPress {
			wp = row.Huidig
		}
	}
	if php != "8.3" {
		t.Fatalf("expected genormaliseerde PHP version from prod env, got %q", php)
	}
	if wp != "6.5.2" {
		t.Fatalf("expected WordPress version from prod env (not staging), got %q", wp)
	}

	var wpUpdatesOpmerking string
	for _, row := range r.WPUpdates {
		if row.Naam == "WordPress plug-ins" {
			wpUpdatesOpmerking = row.Opmerking
		}
	}
	if wpUpdatesOpmerking != "2 plugin-updates beschikbaar" {
		t.Fatalf("expected plugin update count opmerking, got %q", wpUpdatesOpmerking)
	}

	found := false
	for _, a := range r.Acties {
		if a.Actie == "1 kwetsbare plugin(s) gevonden" && a.Wie == "Bitfactory" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected vulnerable-plugin actie row, got %+v", r.Acties)
	}
}

func TestPrefillIsIdempotentAcrossSaveCycles(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	store := NewReportStore(dir)

	// First cycle: one vulnerable plugin -> prefill, then save the draft
	// (mirrors the app flow: Prefill → user reviews/edits → SaveReport).
	kinstaFake1 := &fakeReportKinsta{
		envs: &kinsta.EnvironmentDetails{
			Plugins: []kinsta.Plugin{{Name: "a", IsVersionVulnerable: true}},
		},
	}
	svc1 := NewReportService(projects, kinstaFake1, nil, store, nil, nil, nil)
	r1, err := svc1.Prefill("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("Prefill 1: %v", err)
	}
	if err := svc1.SaveReport(r1); err != nil {
		t.Fatalf("SaveReport: %v", err)
	}

	// Second cycle: two vulnerable plugins now -> prefill again over the
	// saved draft. The stale "1 kwetsbare..." row must be replaced, not
	// duplicated alongside the fresh "2 kwetsbare..." row.
	kinstaFake2 := &fakeReportKinsta{
		envs: &kinsta.EnvironmentDetails{
			Plugins: []kinsta.Plugin{
				{Name: "a", IsVersionVulnerable: true},
				{Name: "b", IsVersionVulnerable: true},
			},
		},
	}
	svc2 := NewReportService(projects, kinstaFake2, nil, store, nil, nil, nil)
	r2, err := svc2.Prefill("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("Prefill 2: %v", err)
	}

	var matches []domain.ActieRow
	for _, a := range r2.Acties {
		if strings.Contains(a.Actie, "kwetsbare plugin") {
			matches = append(matches, a)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one vulnerable-plugin actie row after repeated prefill, got %d: %+v", len(matches), r2.Acties)
	}
	if matches[0].Actie != "2 kwetsbare plugin(s) gevonden" {
		t.Fatalf("expected latest count in actie text, got %q", matches[0].Actie)
	}
}

func TestPrefillIsNonFatalOnKinstaError(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	kinstaFake := &fakeReportKinsta{detErr: context.DeadlineExceeded, envErr: context.DeadlineExceeded}
	svc := NewReportService(projects, kinstaFake, nil, NewReportStore(dir), nil, nil, nil)

	r, err := svc.Prefill("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("Prefill should not fail on kinsta error, got %v", err)
	}
	if r.ClientName != "Cefetra" {
		t.Fatalf("expected skeleton to still be returned, got %+v", r)
	}
}

func TestPrefillAddsSecurityFindingsActieAndCounts(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	sec := &fakeReportSecurity{result: &SecurityScanResult{
		Findings: []SecurityFinding{
			{Source: "npm", Package: "x"},
			{Source: "npm", Package: "y"},
			{Source: "composer", Package: "z"},
		},
	}}
	svc := NewReportService(projects, nil, sec, NewReportStore(dir), nil, nil, nil)

	r, err := svc.Prefill("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("Prefill: %v", err)
	}

	found := false
	for _, a := range r.Acties {
		if a.Actie == "3 security-findings uit npm/composer audit" && a.Wie == "Bitfactory" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected security-findings actie row, got %+v", r.Acties)
	}

	var npmOp, composerOp string
	for _, row := range r.DependencyUpdates {
		if row.Naam == "NPM - Frontend packages" {
			npmOp = row.Opmerking
		}
		if row.Naam == "Composer - PHP packages" {
			composerOp = row.Opmerking
		}
	}
	if npmOp != "2 findings" || composerOp != "1 findings" {
		t.Fatalf("expected per-source finding counts, got npm=%q composer=%q", npmOp, composerOp)
	}
}

func TestPrefillIsNonFatalOnSecurityError(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	sec := &fakeReportSecurity{err: context.DeadlineExceeded}
	svc := NewReportService(projects, nil, sec, NewReportStore(dir), nil, nil, nil)

	if _, err := svc.Prefill("p1", "Q2 2026"); err != nil {
		t.Fatalf("Prefill should not fail on security error, got %v", err)
	}
}

func TestPrefillDoesNotSave(t *testing.T) {
	dir := t.TempDir()
	store := NewReportStore(dir)
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	svc := NewReportService(projects, nil, nil, store, nil, nil, nil)

	if _, err := svc.Prefill("p1", "Q2 2026"); err != nil {
		t.Fatalf("Prefill: %v", err)
	}
	got, err := store.Get("p1", "Q2 2026")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ProjectID != "" {
		t.Fatalf("expected Prefill not to persist, but found stored report: %+v", got)
	}
}

// --- SaveReport / ListReports ---

func TestSaveReportStampsUpdatedAtAndListsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	svc := NewReportService(projects, nil, nil, NewReportStore(dir), nil, nil, nil)

	if err := svc.SaveReport(domain.Report{ProjectID: "p1", Period: "Q1 2026", ClientName: "Cefetra"}); err != nil {
		t.Fatalf("SaveReport: %v", err)
	}
	if err := svc.SaveReport(domain.Report{ProjectID: "p1", Period: "Q2 2026", ClientName: "Cefetra"}); err != nil {
		t.Fatalf("SaveReport: %v", err)
	}

	list, err := svc.ListReports("p1")
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(list))
	}
	for _, r := range list {
		if r.UpdatedAt.IsZero() {
			t.Fatalf("expected UpdatedAt stamped, got zero for %+v", r)
		}
	}
}

// --- ExportPDF ---

func TestExportPDFErrorsWithoutApp(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{"p1": testProject("p1")}}
	svc := NewReportService(projects, nil, nil, NewReportStore(dir), &fakeReportPDF{}, nil, nil)

	if _, err := svc.ExportPDF("p1", "Q2 2026"); err == nil {
		t.Fatal("expected error when app is not initialized")
	}
}

// --- Prefill: repo-bestanden en EOL-data ---

type fakeEOL struct {
	byProduct map[string][]endoflife.Cycle
}

func (f *fakeEOL) Cycles(_ context.Context, product string) ([]endoflife.Cycle, error) {
	c, ok := f.byProduct[product]
	if !ok {
		return nil, fmt.Errorf("onbekend product %q", product)
	}
	return c, nil
}

type fakeRepoFiles struct {
	files map[string]string // relPath -> inhoud
}

func (f *fakeRepoFiles) ReadProjectFile(_ domain.Project, relPath string) ([]byte, error) {
	c, ok := f.files[relPath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(c), nil
}

func TestPrefillFromRepoAndEOL(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{
		"p1": {DisplayName: "Klant", Path: "/tmp/x"},
	}}
	repo := &fakeRepoFiles{files: map[string]string{
		".bitfactory/docker/php-fpm/Dockerfile.dev": "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.3-jit",
		"docker-compose.yaml":                       "services:\n  node:\n    image: europe-docker.pkg.dev/bitfactory-nl/service-node/node:24.10\n",
	}}
	eol := &fakeEOL{byProduct: map[string][]endoflife.Cycle{
		"php": {
			{Cycle: "8.5", Latest: "8.5.8", Support: eolFlex(t, "2027-12-31", false), EOL: eolFlex(t, "2029-12-31", false)},
			{Cycle: "8.3", Latest: "8.3.32", Support: eolFlex(t, "2025-12-31", false), EOL: eolFlex(t, "2027-12-31", false)},
		},
		"nodejs": {
			{Cycle: "24", Latest: "24.18.0", LTS: eolFlex(t, "2025-10-28", false), EOL: eolFlex(t, "2028-04-30", false)},
		},
		"mariadb": {
			{Cycle: "12.3", Latest: "12.3.2", LTS: eolFlex(t, "", true), EOL: eolFlex(t, "2029-06-30", false)},
		},
		"wordpress": {
			{Cycle: "7.0", Latest: "7.0.2", EOL: eolFlex(t, "", false)},
		},
	}}
	svc := NewReportService(projects, nil, nil, NewReportStore(dir), nil, eol, repo)

	r, err := svc.Prefill("p1", "Q3 2026")
	if err != nil {
		t.Fatalf("Prefill: %v", err)
	}
	get := func(component string) domain.SoftwareRow {
		for _, row := range r.Software {
			if row.Component == component {
				return row
			}
		}
		t.Fatalf("rij %q ontbreekt: %+v", component, r.Software)
		return domain.SoftwareRow{}
	}

	if row := get(compPHPLocal); row.Huidig != "8.3" || row.OndersteundTot != "31-12-2027" || row.Laatste != "8.5.8" {
		t.Errorf("PHP (lokaal) = %+v", row)
	}
	if row := get(compNode); row.Huidig != "24.10" || row.OndersteundTot != "30-04-2028" || row.Laatste != "24.18.0" {
		t.Errorf("Node = %+v", row)
	}
	// Geen Kinsta in deze test: PHP (productie) blijft leeg maar krijgt wel "Laatste".
	if row := get(compPHPProd); row.Huidig != "" || row.Laatste != "8.5.8" || row.OndersteundTot != "" {
		t.Errorf("PHP (productie) = %+v", row)
	}
	// MariaDB: alleen Laatste (Huidig onbekend).
	if row := get(compMariaDB); row.Laatste != "12.3.2" || row.OndersteundTot != "" {
		t.Errorf("MariaDB = %+v", row)
	}
}

func TestPrefillRepoFallbackNaarDockerfile(t *testing.T) {
	dir := t.TempDir()
	projects := &fakeReportProjects{projects: map[string]domain.Project{
		"p1": {DisplayName: "Klant", Path: "/tmp/x"},
	}}
	repo := &fakeRepoFiles{files: map[string]string{
		".bitfactory/docker/php-fpm/Dockerfile": "FROM europe-docker.pkg.dev/bitfactory-nl/service-php/php:8.2 AS composer\nFROM europe-docker.pkg.dev/bitfactory-nl/service-node/node:20.12.2 AS frontend",
	}}
	svc := NewReportService(projects, nil, nil, NewReportStore(dir), nil, nil, repo)

	r, err := svc.Prefill("p1", "Q3 2026")
	if err != nil {
		t.Fatalf("Prefill: %v", err)
	}
	for _, row := range r.Software {
		switch row.Component {
		case compPHPLocal:
			if row.Huidig != "8.2" {
				t.Errorf("PHP (lokaal) fallback = %q, want 8.2", row.Huidig)
			}
		case compNode:
			if row.Huidig != "20.12.2" {
				t.Errorf("Node fallback = %q, want 20.12.2", row.Huidig)
			}
		}
	}
}
