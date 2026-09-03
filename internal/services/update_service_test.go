package services

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// nepFetcher vervangt de GitHub-client. aanroep wordt achter een mutex gelezen
// en geschreven: TestStartControleertNaDeInitieleVertraging leest hem vanuit de
// testgoroutine terwijl de achtergrondloop van UpdateService hem tegelijk
// verhoogt (go test -race wees deze race aan zonder deze lock).
type nepFetcher struct {
	rel    github.Release
	relErr error

	mu      sync.Mutex
	aanroep int
}

func (n *nepFetcher) LatestRelease(context.Context) (github.Release, error) {
	n.mu.Lock()
	n.aanroep++
	n.mu.Unlock()
	return n.rel, n.relErr
}

func (n *nepFetcher) DownloadAsset(context.Context, int64, io.Writer, func(int64, int64)) error {
	return errors.New("niet gebruikt in deze test")
}

// aanroepen geeft het aantal LatestRelease-aanroepen, veilig op te vragen
// terwijl de achtergrondloop meetelt.
func (n *nepFetcher) aanroepen() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.aanroep
}

// nepEmitter legt de events vast die de service uitstuurt. Ook hier een mutex:
// dezelfde reden als bij nepFetcher.aanroep hierboven.
type nepEmitter struct {
	mu     sync.Mutex
	events []string
	data   []any
}

func (n *nepEmitter) Emit(name string, data ...any) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, name)
	if len(data) > 0 {
		n.data = append(n.data, data[0])
	}
	return true
}

// eventNames geeft een snapshot van de uitgestuurde events, veilig op te
// vragen terwijl de achtergrondloop meetelt.
func (n *nepEmitter) eventNames() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.events...)
}

// testService bouwt een UpdateService die niets van de echte omgeving
// aanraakt: eigen state-pad in een tempmap, een nep-bundle-pad zodat de service
// zich "geïnstalleerd" waant, en een injecteerbare client.
func testService(t *testing.T, huidig string, f *nepFetcher) (*UpdateService, *nepEmitter) {
	t.Helper()
	dir := t.TempDir()
	em := &nepEmitter{}
	autoAan := true
	s := &UpdateService{
		cfg: &config.Global{
			PluginRepo: config.PluginRepo{GithubToken: "ghp_test"},
			Updates:    config.UpdatesGlobal{AutoCheck: &autoAan},
		},
		statePath:  filepath.Join(dir, "update-state.json"),
		logDir:     filepath.Join(dir, "logs"),
		bundlePath: filepath.Join(dir, "Kinsta Updater.app"),
		current:    huidig,
		emitter:    em,
		newClient:  func(string, string) releaseFetcher { return f },
	}
	return s, em
}

func nieuweRelease(tag, body string) github.Release {
	return github.Release{
		TagName: tag,
		Body:    body,
		Asset:   github.ReleaseAsset{ID: 42, Name: "RDM-Sites-Tool-" + tag + "-macOS.zip", Size: 12230392},
	}
}

func TestCheckVindtNieuwereVersieEnEmitEvent(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "## Wijzigingen\n\n### Nieuw\n- Zelf-update\n")}
	s, em := testService(t, "v0.2.9", f)

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if status.Available == nil {
		t.Fatal("Available = nil, wil v0.2.10")
	}
	if status.Available.Version != "v0.2.10" {
		t.Errorf("versie = %q, wil v0.2.10", status.Available.Version)
	}
	if status.Available.SizeBytes != 12230392 {
		t.Errorf("SizeBytes = %d", status.Available.SizeBytes)
	}
	if len(status.Available.Changes) != 1 || status.Available.Changes[0].Kind != domain.ChangeNieuw {
		t.Errorf("Changes = %+v, wil één nieuw-regel", status.Available.Changes)
	}
	if status.LastCheck.IsZero() {
		t.Error("LastCheck is niet gezet")
	}
	if len(em.events) != 1 || em.events[0] != "updates:available" {
		t.Errorf("events = %v, wil één updates:available", em.events)
	}
	if s.asset.ID != 42 {
		t.Errorf("asset = %+v, wil id 42 bewaard voor de installatie", s.asset)
	}
}

func TestCheckZonderNieuwereVersie(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.9", "")}
	s, em := testService(t, "v0.2.9", f)

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Available != nil {
		t.Errorf("Available = %+v, wil nil", status.Available)
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen", em.events)
	}
}

func TestCheckMeldtOvergeslagenVersieZonderEvent(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, em := testService(t, "v0.2.9", f)

	if err := s.Skip("v0.2.10"); err != nil {
		t.Fatalf("Skip: %v", err)
	}

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Available == nil || !status.Available.Skipped {
		t.Fatalf("Available = %+v, wil Skipped true (de badge blijft, de popup niet)", status.Available)
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen popup-event voor een overgeslagen versie", em.events)
	}
}

func TestCheckSkipGeldtNietVoorEenNogNieuwereVersie(t *testing.T) {
	s, em := testService(t, "v0.2.9", &nepFetcher{rel: nieuweRelease("v0.2.10", "")})
	if err := s.Skip("v0.2.10"); err != nil {
		t.Fatalf("Skip: %v", err)
	}

	s.newClient = func(string, string) releaseFetcher {
		return &nepFetcher{rel: nieuweRelease("v0.2.11", "")}
	}
	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if status.Available == nil || status.Available.Skipped {
		t.Fatalf("Available = %+v, wil v0.2.11 niet overgeslagen", status.Available)
	}
	if len(em.events) != 1 {
		t.Errorf("events = %v, wil één event voor de nieuwere versie", em.events)
	}
}

func TestCheckBewaartFoutZonderEvent(t *testing.T) {
	f := &nepFetcher{relErr: errors.New("status 401")}
	s, em := testService(t, "v0.2.9", f)

	if _, err := s.Check(); err == nil {
		t.Fatal("Check gaf geen fout")
	}
	status := s.Status()
	if status.LastError == "" {
		t.Error("LastError is leeg, wil de foutmelding voor Instellingen")
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen popup bij een mislukte check", em.events)
	}
}

func TestCheckZonderTokenGeeftDuidelijkeFout(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	s.cfg.PluginRepo.GithubToken = ""

	_, err := s.Check()
	if !errors.Is(err, errGeenUpdateToken) {
		t.Fatalf("err = %v, wil errGeenUpdateToken", err)
	}
	if f.aanroep != 0 {
		t.Error("er is een API-aanroep gedaan zonder token")
	}
}

func TestCheckGebruiktTokenOverride(t *testing.T) {
	var gotToken string
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	s.cfg.Updates.GithubToken = "ghp_override"
	s.newClient = func(token, repo string) releaseFetcher {
		gotToken = token
		return f
	}

	if _, err := s.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if gotToken != "ghp_override" {
		t.Errorf("token = %q, wil de override", gotToken)
	}
}

func TestCheckIsUitInDevBuild(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v9.9.9", "")}
	s, em := testService(t, "dev", f)

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Enabled {
		t.Error("Enabled = true in een dev-build")
	}
	if f.aanroep != 0 {
		t.Error("een dev-build heeft de API aangeroepen")
	}
	if len(em.events) != 0 {
		t.Errorf("events = %v, wil geen", em.events)
	}
}

func TestCheckIsUitBuitenEenAppBundle(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	s.bundlePath = ""

	status, _ := s.Check()
	if status.Enabled {
		t.Error("Enabled = true zonder .app-bundle")
	}
	if f.aanroep != 0 {
		t.Error("de API is aangeroepen zonder .app-bundle")
	}
}

func TestWhatsNewNaEenGeslaagdeUpdate(t *testing.T) {
	s, _ := testService(t, "v0.2.10", &nepFetcher{})
	st := updateState{
		LastRunVersion:   "v0.2.9",
		InstalledVersion: "v0.2.10",
		InstalledChanges: []domain.ChangeEntry{{Kind: domain.ChangeNieuw, Text: "Zelf-update"}},
	}
	if err := saveUpdateState(s.statePath, st); err != nil {
		t.Fatal(err)
	}

	got := s.WhatsNew()
	if got == nil {
		t.Fatal("WhatsNew = nil, wil de changelog van v0.2.10")
	}
	if got.Version != "v0.2.10" || len(got.Changes) != 1 {
		t.Errorf("WhatsNew = %+v", got)
	}

	// Tweede aanroep: het venster hoort maar één keer te komen.
	if tweede := s.WhatsNew(); tweede != nil {
		t.Errorf("tweede WhatsNew = %+v, wil nil", tweede)
	}
}

func TestWhatsNewBijEersteStartOoit(t *testing.T) {
	s, _ := testService(t, "v0.2.9", &nepFetcher{})

	if got := s.WhatsNew(); got != nil {
		t.Fatalf("WhatsNew = %+v, wil nil bij een verse installatie", got)
	}

	st, err := loadUpdateState(s.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastRunVersion != "v0.2.9" {
		t.Errorf("LastRunVersion = %q, wil v0.2.9 zodat het venster later niet onterecht komt", st.LastRunVersion)
	}
}

func TestWhatsNewNegeertEenVreemdeInstalledVersion(t *testing.T) {
	// Handmatig een oudere build teruggezet: de bewaarde changelog hoort niet
	// bij de versie die nu draait.
	s, _ := testService(t, "v0.2.9", &nepFetcher{})
	if err := saveUpdateState(s.statePath, updateState{
		LastRunVersion:   "v0.2.8",
		InstalledVersion: "v0.2.10",
		InstalledChanges: []domain.ChangeEntry{{Kind: domain.ChangeNieuw, Text: "Iets anders"}},
	}); err != nil {
		t.Fatal(err)
	}

	if got := s.WhatsNew(); got != nil {
		t.Errorf("WhatsNew = %+v, wil nil", got)
	}
}

func TestBundlePathFor(t *testing.T) {
	cases := []struct {
		exe string
		wil string
	}{
		{"/Applications/Kinsta Updater.app/Contents/MacOS/rdm-sites-tool", "/Applications/Kinsta Updater.app"},
		{"/Users/x/Projects/RDM-Sites-tool/bin/rdm-sites-tool", ""},
		{"/Users/x/bin/Contents/MacOS/rdm-sites-tool", ""},
		{"/tmp/thing.app/Contents/rdm-sites-tool", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := bundlePathFor(c.exe); got != c.wil {
			t.Errorf("bundlePathFor(%q) = %q, wil %q", c.exe, got, c.wil)
		}
	}
}

func TestStartDraaitNietAlsAutoCheckUitStaat(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)
	uit := false
	s.cfg.Updates.AutoCheck = &uit
	s.initialDelay = 10 * time.Millisecond
	s.interval = time.Hour

	s.Start()
	t.Cleanup(s.Stop)

	// De loop draait nu ook met de toggle uit (zodat aanzetten in Instellingen
	// geen herstart vergt), maar slaat elke ronde over. Wacht tot ruim na de
	// initiële vertraging om aan te tonen dat die ronde niets heeft gedaan.
	time.Sleep(100 * time.Millisecond)
	if f.aanroepen() != 0 {
		t.Error("de loop heeft gecontroleerd terwijl automatisch controleren uit staat")
	}
}

func TestStartControleertNaDeInitieleVertraging(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, em := testService(t, "v0.2.9", f)
	s.initialDelay = 10 * time.Millisecond
	s.interval = time.Hour

	s.Start()
	t.Cleanup(s.Stop)

	// Wacht op zowel de aanroep als het event: de achtergrondloop verhoogt
	// aanroep vóórdat Check() de state wegschrijft en het event uitstuurt, dus
	// stoppen zodra alleen aanroep > 0 is levert een race op met de emit die
	// er nog aan zit te komen.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.aanroepen() > 0 && len(em.eventNames()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f.aanroepen() == 0 {
		t.Fatal("de loop heeft niet gecontroleerd")
	}
	if len(em.eventNames()) == 0 {
		t.Error("de loop stuurde geen updates:available")
	}
}

func TestCheckGeeftLegeLijstEnGeenNilZonderChangelog(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "## Installatie\n\n1. Download\n")}
	s, _ := testService(t, "v0.2.9", f)

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Available == nil {
		t.Fatal("Available = nil")
	}
	if status.Available.Changes == nil {
		t.Error("Changes = nil, wil een lege (niet-nil) slice zodat JSON [] geeft")
	}
	if len(status.Available.Changes) != 0 {
		t.Errorf("Changes = %+v, wil leeg", status.Available.Changes)
	}
}

func TestCheckHoudtSaveFoutZichtbaar(t *testing.T) {
	f := &nepFetcher{rel: nieuweRelease("v0.2.10", "")}
	s, _ := testService(t, "v0.2.9", f)

	// s.statePath onder een gewoon bestand zetten laat MkdirAll (in
	// saveUpdateState) falen: er is geen map te maken waar al een bestand ligt.
	bestand := filepath.Join(t.TempDir(), "geen-map")
	if err := os.WriteFile(bestand, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.statePath = filepath.Join(bestand, "update-state.json")

	status, err := s.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Available == nil {
		t.Fatal("Available = nil, wil v0.2.10 ondanks de save-fout")
	}
	if s.Status().LastError == "" {
		t.Error("LastError is leeg, wil de save-fout zichtbaar in Instellingen")
	}
	if !strings.Contains(s.Status().LastError, "update-state") {
		t.Errorf("LastError = %q, wil een verwijzing naar update-state", s.Status().LastError)
	}
}
