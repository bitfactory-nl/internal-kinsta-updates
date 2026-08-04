package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/config"

	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/domain"
)

// fakeSSHRunner vervangt de SSH-adapter: onthoudt het commando en geeft een
// vooraf bepaalde uitvoer terug.
type fakeSSHRunner struct {
	uit           string
	err           error
	calls         int
	laatste       string
	laatsteTarget sshadapter.Target
	voor          func() // haak om re-entrancy te testen
}

func (f *fakeSSHRunner) Upload(context.Context, sshadapter.Target, string, []byte) error {
	return nil
}

func (f *fakeSSHRunner) RunCommand(_ context.Context, t sshadapter.Target, cmd string) (string, error) {
	f.calls++
	f.laatste = cmd
	f.laatsteTarget = t
	if f.voor != nil {
		f.voor()
	}
	return f.uit, f.err
}

// fakeSecrets vervangt de keychain in tests.
type fakeSecrets struct {
	opgeslagen map[string]string
	zetFout    error
}

func (f *fakeSecrets) Set(account, secret string) error {
	if f.zetFout != nil {
		return f.zetFout
	}
	if f.opgeslagen == nil {
		f.opgeslagen = map[string]string{}
	}
	f.opgeslagen[account] = secret
	return nil
}

func (f *fakeSecrets) Get(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	account := strings.TrimPrefix(ref, "keychain:")
	secret, ok := f.opgeslagen[account]
	if !ok {
		return "", errors.New("niet in keychain: " + account)
	}
	return secret, nil
}

// fakeKinstaSSH levert een vast SSH-eindpunt zonder de Kinsta-API te raken.
type fakeKinstaSSH struct {
	ep  EnvSSHEndpoint
	err error
}

func (f fakeKinstaSSH) EnvironmentSSH(string, string) (EnvSSHEndpoint, error) {
	return f.ep, f.err
}

// sentinelUitvoer bouwt uitvoer zoals de server die geeft: ruis, de shell-regels
// en daarna het ingepakte resultaat tussen sentinels.
func sentinelUitvoer(t *testing.T, payload map[string]any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return "PHP Notice: iets onbelangrijks\n" +
		"RDM-ROOT:/www/site_123/public\n" +
		"RDM-DU:2048\n" +
		mediaSentinelStart + "\n" +
		base64.StdEncoding.EncodeToString(buf.Bytes()) + "\n" +
		mediaSentinelEnd + "\n"
}

func voorbeeldPayload() map[string]any {
	return map[string]any{
		"uploadsPath":     "/www/site_123/public/wp-content/uploads",
		"uploadsUrl":      "https://klant.nl/wp-content/uploads",
		"totalFiles":      12,
		"totalBytes":      999,
		"attachmentCount": 4,
		"referencedCount": 3,
		"byClass":         []map[string]any{{"class": "original", "files": 4, "bytes": 800}},
		"byPeriod":        []map[string]any{{"period": "2024/05", "files": 4, "bytes": 800}},
		"largest":         []map[string]any{{"path": "2024/05/groot.jpg", "bytes": 800, "class": "original"}},
		"categories": []map[string]any{
			{"category": "orphan_file", "hard": true, "files": 1, "bytes": 100, "samples": []any{}},
			{"category": "unreferenced", "hard": true, "files": 1, "bytes": 50, "samples": []any{}},
		},
		"detail":           []map[string]any{{"path": "2024/05/zwerver.jpg", "bytes": 100, "category": "orphan_file"}},
		"tablesScanned":    []string{"wp_posts", "wp_postmeta"},
		"referenceScanRan": true,
		"durationMs":       1234,
	}
}

func newMediaService(t *testing.T, runner sshRunner, projectPad string) (*MediaService, *ProjectService) {
	t.Helper()
	ps := NewProjectService(nil)
	ps.projects = []domain.Project{{
		ID:          "p1",
		Path:        projectPad,
		DisplayName: "web-vanluykennl",
		Config: domain.ProjectConfig{
			SSH: &domain.SSHTarget{User: "vanluykennl", Port: 12345},
		},
	}}
	svc := NewMediaService(ps, nil, NewMediaScanStore(t.TempDir()))
	svc.kinsta = fakeKinstaSSH{ep: EnvSSHEndpoint{Host: "1.2.3.4", Port: 12345, EnvName: "live"}}
	svc.ssh = runner
	svc.secrets = &fakeSecrets{}
	svc.now = func() time.Time { return time.Date(2026, 7, 29, 14, 2, 0, 0, time.UTC) }
	return svc, ps
}

func TestMediaScanEnvironmentSlaatOp(t *testing.T) {
	runner := &fakeSSHRunner{uit: sentinelUitvoer(t, voorbeeldPayload())}
	svc, _ := newMediaService(t, runner, t.TempDir())

	sum, err := svc.ScanEnvironment("p1", "env-1", nil)
	if err != nil {
		t.Fatalf("ScanEnvironment: %v", err)
	}
	if runner.calls != 1 {
		t.Errorf("SSH-oproepen = %d, wil 1: de hele scan hoort in één verbinding", runner.calls)
	}
	if !strings.Contains(runner.laatste, "wp eval-file -") || !strings.Contains(runner.laatste, "du -sk") {
		t.Errorf("commando mist de analyzer of de groottemeting:\n%s", runner.laatste)
	}
	if sum.Environment != "live" || sum.ProjectName != "web-vanluykennl" {
		t.Errorf("samenvatting = %+v", sum)
	}
	if sum.TotalFiles != 12 || sum.AttachmentCount != 4 {
		t.Errorf("cijfers niet overgenomen: %+v", sum)
	}
	if sum.DiskUsageBytes != 2048*1024 {
		t.Errorf("DiskUsageBytes = %d, wil 2 MiB uit de du-regel", sum.DiskUsageBytes)
	}

	// De categorie "geen referentie gevonden" mag nooit als hard feit blijven staan,
	// ook niet als de server dat beweert.
	for _, c := range sum.Categories {
		if c.Category == domain.MediaUnreferenced && c.Hard {
			t.Error("unreferenced is als hard feit doorgegeven")
		}
	}

	opgeslagen, err := svc.LatestScan("p1")
	if err != nil || opgeslagen == nil {
		t.Fatalf("LatestScan = %v, %v", opgeslagen, err)
	}
	rijen, err := svc.ScanDetail("p1", opgeslagen.ID, domain.MediaOrphanFile, "", 0, 10)
	if err != nil {
		t.Fatalf("ScanDetail: %v", err)
	}
	if len(rijen) != 1 || rijen[0].Path != "2024/05/zwerver.jpg" {
		t.Errorf("detailregels = %+v", rijen)
	}
}

func TestMediaScanEnvironmentWeigertTweedeScan(t *testing.T) {
	runner := &fakeSSHRunner{uit: sentinelUitvoer(t, voorbeeldPayload())}
	svc, _ := newMediaService(t, runner, t.TempDir())

	var tweedeFout error
	runner.voor = func() {
		_, tweedeFout = svc.ScanEnvironment("p1", "env-1", nil)
	}

	if _, err := svc.ScanEnvironment("p1", "env-1", nil); err != nil {
		t.Fatalf("eerste scan: %v", err)
	}
	if tweedeFout == nil {
		t.Fatal("een tweede scan tijdens de eerste hoort geweigerd te worden")
	}
	if !strings.Contains(tweedeFout.Error(), "loopt al") {
		t.Errorf("foutmelding = %q", tweedeFout)
	}
}

func TestMediaScanEnvironmentZonderSSHGebruiker(t *testing.T) {
	runner := &fakeSSHRunner{}
	svc, ps := newMediaService(t, runner, t.TempDir())
	ps.projects[0].Config.SSH = nil

	_, err := svc.ScanEnvironment("p1", "env-1", nil)
	if err == nil {
		t.Fatal("wil een fout zonder SSH-gebruiker")
	}
	if !strings.Contains(err.Error(), "SSH-gebruiker") {
		t.Errorf("foutmelding = %q; wil uitleg over de ontbrekende gebruiker", err)
	}
	if runner.calls != 0 {
		t.Error("er mag geen verbinding worden opgezet zonder gebruikersnaam")
	}
}

func TestMediaScanEnvironmentFoutcodeMetGeldigResultaat(t *testing.T) {
	runner := &fakeSSHRunner{
		uit: sentinelUitvoer(t, voorbeeldPayload()),
		err: errors.New("commando mislukt: exit status 255"),
	}
	svc, _ := newMediaService(t, runner, t.TempDir())

	sum, err := svc.ScanEnvironment("p1", "env-1", nil)
	if err != nil {
		t.Fatalf("een geldig resultaat met foutcode hoort bruikbaar te zijn: %v", err)
	}
	gemeld := strings.Join(sum.Scope.Notes, " | ")
	if !strings.Contains(gemeld, "foutcode") {
		t.Errorf("notities = %q; wil de foutcode vermeld hebben", gemeld)
	}
}

func TestMediaScanEnvironmentServerfout(t *testing.T) {
	runner := &fakeSSHRunner{
		uit: "RDM-ERR:geen wp-config.php gevonden\n",
		err: errors.New("commando mislukt: exit status 3"),
	}
	svc, _ := newMediaService(t, runner, t.TempDir())

	_, err := svc.ScanEnvironment("p1", "env-1", nil)
	if err == nil {
		t.Fatal("wil een fout wanneer de server geen resultaat geeft")
	}
	if !strings.Contains(err.Error(), "wp-config.php") {
		t.Errorf("foutmelding = %q; wil de melding van de server", err)
	}
}

func TestMediaProbeEnvironment(t *testing.T) {
	runner := &fakeSSHRunner{uit: strings.Join([]string{
		"RDM-USER:vanluykennl",
		"RDM-HOME:/www/site_123",
		"RDM-ROOT:/www/site_123/public",
		"RDM-WPCLI:WP-CLI 2.10.0",
		"RDM-DU:4096",
	}, "\n")}
	svc, _ := newMediaService(t, runner, t.TempDir())

	probe, err := svc.ProbeEnvironment("p1", "env-1")
	if err != nil {
		t.Fatalf("ProbeEnvironment: %v", err)
	}
	if probe.User != "vanluykennl" || probe.Webroot != "/www/site_123/public" {
		t.Errorf("probe = %+v", probe)
	}
	if !strings.Contains(probe.WPCLI, "WP-CLI") {
		t.Errorf("WP-CLI-versie = %q", probe.WPCLI)
	}
	if probe.UploadsKB != 4096 {
		t.Errorf("UploadsKB = %d, wil 4096", probe.UploadsKB)
	}
}

func TestMediaProbeZonderWebroot(t *testing.T) {
	runner := &fakeSSHRunner{uit: "RDM-USER:klant\nRDM-HOME:/www/site_9\nRDM-ROOT:\n"}
	svc, _ := newMediaService(t, runner, t.TempDir())

	_, err := svc.ProbeEnvironment("p1", "env-1")
	if err == nil || !strings.Contains(err.Error(), "wp-config.php") {
		t.Errorf("fout = %v; wil uitleg dat de webroot niet gevonden is", err)
	}
}

func TestMediaSaveSSHAccessZetWachtwoordInKeychainNietInConfig(t *testing.T) {
	repo := t.TempDir()
	svc, _ := newMediaService(t, &fakeSSHRunner{}, repo)
	geheim := "Zw3rt-K0nijn!"

	if err := svc.SaveSSHAccess("p1", "steinweg", "/www/site/public", geheim); err != nil {
		t.Fatalf("SaveSSHAccess: %v", err)
	}

	// Het wachtwoord hoort in de keychain te staan, onder een leesbare naam.
	kc := svc.secrets.(*fakeSecrets)
	if kc.opgeslagen["ssh:web-vanluykennl"] != geheim {
		t.Errorf("keychain = %+v; wil het wachtwoord onder ssh:web-vanluykennl", kc.opgeslagen)
	}

	// En absoluut niet in de projectconfig: dat bestand staat in de klantrepo.
	data, err := os.ReadFile(filepath.Join(repo, config.ProjectConfigFile))
	if err != nil {
		t.Fatalf("lees %s: %v", config.ProjectConfigFile, err)
	}
	if strings.Contains(string(data), geheim) {
		t.Fatalf("%s bevat het wachtwoord in platte tekst:\n%s", config.ProjectConfigFile, data)
	}
	if !strings.Contains(string(data), "keychain:ssh:web-vanluykennl") {
		t.Errorf("%s mist de keychain-verwijzing:\n%s", config.ProjectConfigFile, data)
	}

	toegang, err := svc.GetSSHAccess("p1")
	if err != nil {
		t.Fatalf("GetSSHAccess: %v", err)
	}
	if !toegang.HasPassword || toegang.User != "steinweg" {
		t.Errorf("toegang = %+v", toegang)
	}
}

func TestMediaSaveSSHAccessZonderWachtwoordLaatBestaandeStaan(t *testing.T) {
	repo := t.TempDir()
	svc, _ := newMediaService(t, &fakeSSHRunner{}, repo)

	if err := svc.SaveSSHAccess("p1", "steinweg", "", "eerste"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveSSHAccess("p1", "steinweg", "/www/site/public", ""); err != nil {
		t.Fatal(err)
	}

	toegang, _ := svc.GetSSHAccess("p1")
	if !toegang.HasPassword {
		t.Error("een leeg wachtwoordveld hoort het opgeslagen wachtwoord niet te wissen")
	}
	if toegang.Path != "/www/site/public" {
		t.Errorf("pad = %q", toegang.Path)
	}
}

func TestMediaScanGebruiktWachtwoordUitKeychain(t *testing.T) {
	runner := &fakeSSHRunner{uit: sentinelUitvoer(t, voorbeeldPayload())}
	svc, _ := newMediaService(t, runner, t.TempDir())

	if err := svc.SaveSSHAccess("p1", "steinweg", "/www/site/public", "Zw3rt-K0nijn!"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ScanEnvironment("p1", "env-1", nil); err != nil {
		t.Fatalf("ScanEnvironment: %v", err)
	}

	if runner.laatsteTarget.Password != "Zw3rt-K0nijn!" {
		t.Errorf("wachtwoord niet meegegeven aan de SSH-verbinding: %+v", runner.laatsteTarget.User)
	}
	if runner.laatsteTarget.User != "steinweg" {
		t.Errorf("gebruiker = %q", runner.laatsteTarget.User)
	}
}
