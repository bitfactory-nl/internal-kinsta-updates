package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

type fakeLogSource struct {
	logs    map[string]string
	err     error
	envID   string
	file    string
	lines   int
	aanroep int
}

func (f *fakeLogSource) EnvironmentLogs(_ context.Context, envID, fileName string, lines int) (string, error) {
	f.aanroep++
	f.envID, f.file, f.lines = envID, fileName, lines
	if f.err != nil {
		return "", f.err
	}
	return f.logs[fileName], nil
}

// logServiceMetProject zet een LogService op met één project waarvan de checkout
// in een tijdelijke map staat.
func logServiceMetProject(t *testing.T, bron *fakeLogSource, bestanden ...string) (*LogService, string, string) {
	t.Helper()
	wortel := t.TempDir()
	for _, b := range bestanden {
		maakBestand(t, wortel, b)
	}
	projects := NewProjectService(nil)
	projects.projects = []domain.Project{{ID: "p1", DisplayName: "Voorbeeld", Path: wortel}}
	return NewLogService(projects, bron), "p1", wortel
}

func TestFetchGroepeertEnBeoordeelt(t *testing.T) {
	bron := &fakeLogSource{logs: map[string]string{"error": leesFixture(t, "kinsta_error.log")}}
	svc, projectID, _ := logServiceMetProject(t, bron,
		"public/wp-content/themes/voorbeeld/inc/listing-card.php",
		"public/wp-content/plugins/eigen-plugin/src/Widget.php",
		"public/wp-settings.php",
	)

	res, err := svc.Fetch(projectID, "env-1", "error", 500)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if bron.envID != "env-1" || bron.file != "error" || bron.lines != 500 {
		t.Errorf("verkeerd doorgegeven: env=%q file=%q lines=%d", bron.envID, bron.file, bron.lines)
	}
	if res.LinesReceived != 11 {
		t.Errorf("LinesReceived = %d, wil 11", res.LinesReceived)
	}
	if len(res.Groups) == 0 {
		t.Fatal("geen groepen")
	}

	perTitel := map[string]domain.LogGroup{}
	for _, g := range res.Groups {
		perTitel[g.Title] = g
	}

	// De fout in eigen plugincode is de kandidaat.
	var widget, abspath, listing, bot *domain.LogGroup
	for i := range res.Groups {
		g := &res.Groups[i]
		switch {
		case strings.Contains(g.Title, "count()"):
			widget = g
		case strings.Contains(g.Title, "ABSPATH"):
			abspath = g
		case strings.Contains(g.Title, "listing_price"):
			listing = g
		case g.Kind == domain.KindBotProbe:
			bot = g
		}
	}

	if widget == nil {
		t.Fatal("de fout in eigen plugincode ontbreekt")
	}
	if !widget.AIEligible {
		t.Errorf("eigen plugincode hoort een AI-kandidaat te zijn: %s", widget.AIReason)
	}
	if widget.RepoPath != "public/wp-content/plugins/eigen-plugin/src/Widget.php" {
		t.Errorf("RepoPath = %q", widget.RepoPath)
	}

	// Een core-bestand niet, ook al is het een fatal die in de repo staat.
	if abspath == nil {
		t.Fatal("de ABSPATH-fatal ontbreekt")
	}
	if abspath.AIEligible {
		t.Error("een core-bestand hoort geen AI-kandidaat te zijn")
	}
	if !abspath.IsCore {
		t.Error("wp-settings.php hoort als core gemarkeerd te zijn")
	}
	if !strings.Contains(abspath.AIReason, "core") {
		t.Errorf("reden legt core niet uit: %q", abspath.AIReason)
	}

	// Een warning in themacode wél.
	if listing == nil || !listing.AIEligible {
		t.Errorf("de themawarning hoort een kandidaat te zijn: %+v", listing)
	}

	// Botruis nooit.
	if bot == nil {
		t.Fatal("de bot-probe-groep ontbreekt")
	}
	if bot.AIEligible {
		t.Error("botruis hoort geen AI-kandidaat te zijn")
	}
	if bot.AIReason == "" {
		t.Error("ook een 'nee' hoort een uitleg te hebben")
	}
}

// Elke groep hoort een reden te hebben, ook de kandidaten: een knop zonder
// uitleg is in de UI erger dan geen knop.
func TestFetchVultAltijdEenReden(t *testing.T) {
	bron := &fakeLogSource{logs: map[string]string{"error": leesFixture(t, "kinsta_error.log")}}
	svc, projectID, _ := logServiceMetProject(t, bron, "public/wp-content/themes/voorbeeld/inc/listing-card.php")

	res, err := svc.Fetch(projectID, "e", "error", 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, g := range res.Groups {
		if g.AIReason == "" {
			t.Errorf("groep %q heeft geen reden", g.Title)
		}
	}
}

func TestFetchMarkeertPII(t *testing.T) {
	bron := &fakeLogSource{logs: map[string]string{"error": leesFixture(t, "kinsta_error.log")}}
	svc, projectID, _ := logServiceMetProject(t, bron)

	res, err := svc.Fetch(projectID, "e", "error", 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	var gevonden bool
	for _, g := range res.Groups {
		if strings.Contains(g.Title, "SMTP Error") && g.HasPII {
			gevonden = true
		}
	}
	if !gevonden {
		t.Error("de WP_Error-groep met persoonsgegevens is niet als zodanig gemarkeerd")
	}
}

func TestFetchWeigertOnbekendBestand(t *testing.T) {
	svc, projectID, _ := logServiceMetProject(t, &fakeLogSource{})
	if _, err := svc.Fetch(projectID, "e", "debug", 10); err == nil {
		t.Error("onbekend logbestand werd geaccepteerd")
	}
}

func TestFetchWeigertZonderOmgeving(t *testing.T) {
	svc, projectID, _ := logServiceMetProject(t, &fakeLogSource{})
	if _, err := svc.Fetch(projectID, "", "error", 10); err == nil {
		t.Error("lege omgeving werd geaccepteerd")
	}
}

func TestFetchGeeftAPIFoutDoor(t *testing.T) {
	bron := &fakeLogSource{err: fmt.Errorf("status 404")}
	svc, projectID, _ := logServiceMetProject(t, bron)
	if _, err := svc.Fetch(projectID, "e", "error", 10); err == nil {
		t.Error("API-fout werd ingeslikt")
	}
}

func TestFetchWaarschuwtBijRuisEnMaximum(t *testing.T) {
	bron := &fakeLogSource{logs: map[string]string{"error": leesFixture(t, "kinsta_error.log")}}
	svc, projectID, _ := logServiceMetProject(t, bron)

	// 11 regels gevraagd en 11 gekregen: dan is er waarschijnlijk meer.
	res, err := svc.Fetch(projectID, "e", "error", 11)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	samen := strings.Join(res.Warnings, " | ")
	if !strings.Contains(samen, "maximum") {
		t.Errorf("waarschuwing over het maximum ontbreekt: %s", samen)
	}
	if !strings.Contains(samen, "botverkeer") {
		t.Errorf("waarschuwing over botverkeer ontbreekt: %s", samen)
	}
}

func TestFetchWaarschuwtBijLeegLog(t *testing.T) {
	bron := &fakeLogSource{logs: map[string]string{"error": "\n\n"}}
	svc, projectID, _ := logServiceMetProject(t, bron)
	res, err := svc.Fetch(projectID, "e", "error", 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "leeg") {
		t.Errorf("waarschuwingen = %v", res.Warnings)
	}
}

func TestGroupByIDNaFetch(t *testing.T) {
	bron := &fakeLogSource{logs: map[string]string{"error": leesFixture(t, "kinsta_error.log")}}
	svc, projectID, _ := logServiceMetProject(t, bron)

	res, err := svc.Fetch(projectID, "e", "error", 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	wil := res.Groups[0]
	got, file, err := svc.GroupByID(projectID, wil.ID)
	if err != nil {
		t.Fatalf("GroupByID: %v", err)
	}
	if got.ID != wil.ID || file != domain.LogFileError {
		t.Errorf("got = %q/%q", got.ID, file)
	}
	if _, _, err := svc.GroupByID(projectID, "bestaatniet"); err == nil {
		t.Error("onbekende melding gaf geen fout")
	}
}

func TestFixPreviewIsGescrubdEnCompleet(t *testing.T) {
	bron := &fakeLogSource{logs: map[string]string{"error": leesFixture(t, "kinsta_error.log")}}
	svc, projectID, _ := logServiceMetProject(t, bron, "public/wp-content/plugins/eigen-plugin/src/Widget.php")

	res, err := svc.Fetch(projectID, "e", "error", 100)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	var kandidaat domain.LogGroup
	for _, g := range res.Groups {
		if g.AIEligible {
			kandidaat = g
			break
		}
	}
	if kandidaat.ID == "" {
		t.Fatal("geen kandidaat gevonden")
	}

	preview, err := svc.FixPreview(projectID, kandidaat.ID)
	if err != nil {
		t.Fatalf("FixPreview: %v", err)
	}
	if preview.Branch != "fix/log-"+kandidaat.ID {
		t.Errorf("branch = %q", preview.Branch)
	}
	if !strings.Contains(preview.Prompt, "Widget.php") {
		t.Errorf("prompt mist het bestand:\n%s", preview.Prompt)
	}
	// De prompt is precies wat verzonden wordt, dus die moet gescrubd zijn.
	if reEmail.MatchString(preview.Prompt) {
		t.Errorf("prompt bevat een e-mailadres:\n%s", preview.Prompt)
	}
	if reIPv4.MatchString(preview.Prompt) {
		t.Errorf("prompt bevat een ip-adres:\n%s", preview.Prompt)
	}
	// En de grenzen moeten erin staan.
	for _, moet := range []string{"wp-includes", "Commit en push niet", "php -l"} {
		if !strings.Contains(preview.Prompt, moet) {
			t.Errorf("prompt mist %q", moet)
		}
	}
}

// TestFixPreviewVanPIIGroepMaskeert bewijst dat de preview van de groep met
// persoonsgegevens die gegevens niet bevat en dat de UI dat te zien krijgt.
func TestFixPreviewVanPIIGroepMaskeert(t *testing.T) {
	// Een melding met persoonsgegevens die wél naar projectcode wijst.
	ruw := `2026/08/05 12:44:51 [error] 42251#42251: *37774 FastCGI sent in stderr: "PHP message: PHP Warning:  mail() faalde voor test.persoon@example.com in /www/voorbeeld_706/public/wp-content/themes/voorbeeld/ajax/form.php on line 20" while reading response header from upstream, client: 80.56.116.54, server: voorbeeld.nl, request: "POST /form HTTP/2.0", host: "voorbeeld.nl"`
	bron := &fakeLogSource{logs: map[string]string{"error": ruw}}
	svc, projectID, _ := logServiceMetProject(t, bron, "public/wp-content/themes/voorbeeld/ajax/form.php")

	res, err := svc.Fetch(projectID, "e", "error", 10)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("aantal groepen = %d", len(res.Groups))
	}
	g := res.Groups[0]
	if !g.AIEligible {
		t.Fatalf("hoort een kandidaat te zijn: %s", g.AIReason)
	}
	if !g.HasPII {
		t.Error("HasPII hoort true te zijn")
	}

	preview, err := svc.FixPreview(projectID, g.ID)
	if err != nil {
		t.Fatalf("FixPreview: %v", err)
	}
	if strings.Contains(preview.Prompt, "test.persoon@example.com") {
		t.Errorf("prompt bevat het e-mailadres:\n%s", preview.Prompt)
	}
	if len(preview.Masked) == 0 {
		t.Error("Masked hoort te melden wat er gemaskeerd is")
	}
}

func TestFetchOnbekendProject(t *testing.T) {
	svc := NewLogService(NewProjectService(nil), &fakeLogSource{})
	if _, err := svc.Fetch("bestaatniet", "e", "error", 10); err == nil {
		t.Error("onbekend project gaf geen fout")
	}
}

func TestBeoordeelAIKandidaatTabel(t *testing.T) {
	tests := []struct {
		naam string
		g    domain.LogGroup
		wil  bool
	}{
		{"botruis", domain.LogGroup{Kind: domain.KindBotProbe}, false},
		{"access", domain.LogGroup{Kind: domain.KindAccess}, false},
		{"nginx", domain.LogGroup{Kind: domain.KindNginx}, false},
		{"php zonder bestand", domain.LogGroup{Kind: domain.KindPHPFatal}, false},
		{"php met bestand buiten de checkout", domain.LogGroup{Kind: domain.KindPHPFatal, File: "/www/x/public/a.php"}, false},
		{"php in core", domain.LogGroup{Kind: domain.KindPHPFatal, File: "/www/x/public/wp-settings.php", RepoPath: "public/wp-settings.php", IsCore: true}, false},
		{"php in themacode", domain.LogGroup{Kind: domain.KindPHPWarning, File: "/www/x/public/wp-content/themes/t/f.php", RepoPath: "public/wp-content/themes/t/f.php", Line: 3}, true},
		{"deprecated in plugincode", domain.LogGroup{Kind: domain.KindPHPDeprecated, File: "/www/x/public/wp-content/plugins/p/p.php", RepoPath: "public/wp-content/plugins/p/p.php"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			got, reden := beoordeelAIKandidaat(tt.g)
			if got != tt.wil {
				t.Errorf("AIEligible = %v, wil %v (reden: %s)", got, tt.wil, reden)
			}
			if reden == "" {
				t.Error("reden is leeg")
			}
		})
	}
}

func TestAantalRegels(t *testing.T) {
	if got := aantalRegels("a\n\nb\n"); got != 2 {
		t.Errorf("aantalRegels = %d, wil 2", got)
	}
	if got := aantalRegels("   \n"); got != 0 {
		t.Errorf("aantalRegels = %d, wil 0", got)
	}
}

// verrijkGroep hoort een pad buiten de checkout niet als treffer te melden.
func TestVerrijkGroepPadOntsnapping(t *testing.T) {
	wortel := t.TempDir()
	buiten := filepath.Join(filepath.Dir(wortel), "buiten.php")
	_ = os.WriteFile(buiten, []byte("<?php"), 0o644)
	defer os.Remove(buiten)

	g := verrijkGroep(domain.LogGroup{
		Kind: domain.KindPHPFatal,
		File: "/www/x/public/../../" + filepath.Base(buiten),
	}, wortel)
	if g.RepoPath != "" {
		t.Errorf("RepoPath = %q, wil leeg", g.RepoPath)
	}
	if g.AIEligible {
		t.Error("een bestand buiten de checkout hoort geen kandidaat te zijn")
	}
}
