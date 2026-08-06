package services

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// fakeMigrationSSH levert een vaste mappenlijst en, per commando, een echt
// tar.gz-archief zodat de pull end-to-end door de uitpakcode heen loopt.
type fakeMigrationSSH struct {
	listUit string
	archief []byte
	runErr  error
	dlErr   error

	mu    sync.Mutex
	calls []string
}

func (f *fakeMigrationSSH) RunCommand(_ context.Context, _ sshadapter.Target, cmd string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	f.mu.Unlock()
	return f.listUit, f.runErr
}

func (f *fakeMigrationSSH) DownloadCommand(_ context.Context, _ sshadapter.Target, cmd string, w io.Writer, onProgress func(int64)) error {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	f.mu.Unlock()
	if f.dlErr != nil {
		return f.dlErr
	}
	n, err := w.Write(f.archief)
	if onProgress != nil {
		onProgress(int64(n))
	}
	return err
}

func (f *fakeMigrationSSH) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

func newMigrationService(t *testing.T, ssh migrationSSH, projectPad string) (*MigrationService, *ProjectService) {
	t.Helper()
	ps := NewProjectService(nil)
	ps.projects = []domain.Project{{
		ID:          "p1",
		Path:        projectPad,
		DisplayName: "web-vanluykennl",
		Config: domain.ProjectConfig{
			SSH: &domain.SSHTarget{User: "vanluykennl"},
		},
		Deploy: domain.DeployConf{
			Link: domain.DeployLinks{Prod: "https://vanluyken.nl"},
		},
	}}
	svc := NewMigrationService(ps, nil)
	svc.kinsta = fakeKinstaSSH{ep: EnvSSHEndpoint{Host: "1.2.3.4", Port: 12345, EnvName: "live"}}
	svc.ssh = ssh
	svc.secrets = &fakeSecrets{}
	return svc, ps
}

func TestMigrationGetSettingsPrefillsFromEnvAndDeployConf(t *testing.T) {
	dir := t.TempDir()
	writeMultisiteEnvFile(t, dir)
	svc, _ := newMigrationService(t, &fakeMigrationSSH{}, dir)

	cfg, err := svc.GetSettings("p1")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !cfg.Multisite {
		t.Error("Multisite = false; .env zegt MULTISITE=true")
	}
	if cfg.ProdURL != "https://vanluyken.nl" {
		t.Errorf("ProdURL = %q; wil de prod-link uit deploy_conf", cfg.ProdURL)
	}
	if cfg.LocalURL != "https://vanluykennl.test" {
		t.Errorf("LocalURL = %q; wil https://<APP_DOMAIN>", cfg.LocalURL)
	}
	if cfg.ProdDomain != "vanluyken.nl" {
		t.Errorf("ProdDomain = %q", cfg.ProdDomain)
	}
	if cfg.LocalDomain != "vanluykennl.test" {
		t.Errorf("LocalDomain = %q; wil DOMAIN_CURRENT_SITE", cfg.LocalDomain)
	}
}

func TestMigrationSaveAndGetSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeMultisiteEnvFile(t, dir)
	svc, _ := newMigrationService(t, &fakeMigrationSSH{}, dir)

	wil := domain.MigrationCfg{
		Multisite:   true,
		ProdURL:     "https://vanluyken.nl",
		LocalURL:    "https://vanluykennl.test",
		ProdDomain:  "vanluyken.nl",
		LocalDomain: "vanluykennl.test",
		ExtraDomains: []domain.DomainPair{
			{Prod: "smartengine.nl", Local: "smartengine.test"},
			{Prod: "  ", Local: "leeg.test"}, // moet eruit gefilterd worden
		},
	}
	if err := svc.SaveSettings("p1", wil); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	// Echt van schijf teruglezen, niet uit de cache.
	opSchijf, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if opSchijf.Migration == nil {
		t.Fatal("migration-blok staat niet in .rdm.yml")
	}
	if len(opSchijf.Migration.ExtraDomains) != 1 {
		t.Errorf("ExtraDomains = %+v; lege paren horen gefilterd te zijn", opSchijf.Migration.ExtraDomains)
	}
	if opSchijf.Migration.ExtraDomains[0].Prod != "smartengine.nl" {
		t.Errorf("ExtraDomains[0] = %+v", opSchijf.Migration.ExtraDomains[0])
	}

	// En via de service, die nu het opgeslagen blok moet teruggeven in plaats
	// van opnieuw uit .env af te leiden.
	terug, err := svc.GetSettings("p1")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if len(terug.ExtraDomains) != 1 || terug.LocalDomain != "vanluykennl.test" {
		t.Errorf("teruggelezen cfg = %+v", terug)
	}
}

func TestMigrationSaveSettingsSchrijftGeenGeheimen(t *testing.T) {
	dir := t.TempDir()
	writeMultisiteEnvFile(t, dir)
	svc, _ := newMigrationService(t, &fakeMigrationSSH{}, dir)

	if err := svc.SaveSettings("p1", domain.MigrationCfg{ProdURL: "https://vanluyken.nl"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	// De projectconfig staat op .rdm/config.yml; het root-level .rdm.yml is de
	// oude plek. Dit pad hardcoderen als ".rdm.yml" liet de test slagen op een
	// bestand dat niet meer bestaat — en dus niets meer controleren.
	raw, err := os.ReadFile(filepath.Join(dir, config.ProjectConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	// Dit bestand staat in de klantrepo; er mag nooit een wachtwoord in staan.
	for _, verboden := range []string{"secret", "password:", "MYSQL_PWD"} {
		if strings.Contains(string(raw), verboden) {
			t.Errorf(".rdm.yml bevat %q: %s", verboden, raw)
		}
	}
}

func TestMigrationListUploadFolders(t *testing.T) {
	dir := t.TempDir()
	ssh := &fakeMigrationSSH{listUit: strings.Join([]string{
		"RDM-DIR:4096\t2025",
		"RDM-DIR:81920\t2026",
		"RDM-ROOTFILES:2",
	}, "\n")}
	svc, _ := newMigrationService(t, ssh, dir)

	folders, err := svc.ListUploadFolders("p1", "env-1")
	if err != nil {
		t.Fatalf("ListUploadFolders: %v", err)
	}
	if len(folders) != 3 {
		t.Fatalf("aantal = %d, wil 3 (2 mappen + losse bestanden): %+v", len(folders), folders)
	}
	if folders[2].Name != wortelBestandenMap {
		t.Errorf("laatste rij = %q, wil de pseudo-map voor losse bestanden", folders[2].Name)
	}
}

func TestMigrationListUploadFoldersMeldtServerfout(t *testing.T) {
	dir := t.TempDir()
	ssh := &fakeMigrationSSH{listUit: "RDM-ERR:geen wp-content/uploads gevonden"}
	svc, _ := newMigrationService(t, ssh, dir)

	_, err := svc.ListUploadFolders("p1", "env-1")
	if err == nil || !strings.Contains(err.Error(), "uploads") {
		t.Fatalf("verwachtte een duidelijke serverfout, kreeg: %v", err)
	}
}

func TestMigrationPullMediaSchrijftInLokaleUploads(t *testing.T) {
	dir := t.TempDir()
	archief := maakTarGz(t, []tarEntry{
		{naam: "2026/", typeflag: 0x35}, // tar.TypeDir
		{naam: "2026/foto.jpg", inhoud: "jpeg"},
		{naam: "2026/foto-150x150.jpg", inhoud: "thumb"},
	})
	ssh := &fakeMigrationSSH{archief: archief}
	svc, _ := newMigrationService(t, ssh, dir)

	res, err := svc.PullMedia("p1", "env-1", []string{"2026"})
	if err != nil {
		t.Fatalf("PullMedia: %v", err)
	}
	if res.FilesWritten != 2 {
		t.Errorf("FilesWritten = %d, wil 2", res.FilesWritten)
	}
	wilPad := filepath.Join(dir, "public", "wp-content", "uploads")
	if res.LocalPath != wilPad {
		t.Errorf("LocalPath = %q, wil %q", res.LocalPath, wilPad)
	}
	data, err := os.ReadFile(filepath.Join(wilPad, "2026", "foto.jpg"))
	if err != nil {
		t.Fatalf("gepulld bestand lezen: %v", err)
	}
	if string(data) != "jpeg" {
		t.Errorf("inhoud = %q", data)
	}

	// Het remote commando moet een tar naar stdout zijn voor de gekozen map.
	var tarCmd string
	for _, c := range ssh.snapshot() {
		if strings.Contains(c, "tar czf -") {
			tarCmd = c
		}
	}
	if !strings.Contains(tarCmd, "'2026'") {
		t.Errorf("tar-commando = %q, wil de gekozen map", tarCmd)
	}
}

func TestMigrationPullMediaWeigertZonderMappen(t *testing.T) {
	svc, _ := newMigrationService(t, &fakeMigrationSSH{}, t.TempDir())
	if _, err := svc.PullMedia("p1", "env-1", nil); err == nil {
		t.Fatal("verwachtte een fout zonder gekozen mappen")
	}
}

func TestMigrationPullMediaWeigertGelijktijdig(t *testing.T) {
	svc, _ := newMigrationService(t, &fakeMigrationSSH{}, t.TempDir())
	if !svc.claim("p1@env-1") {
		t.Fatal("test setup: claim zou moeten lukken")
	}
	defer svc.release("p1@env-1")

	_, err := svc.PullMedia("p1", "env-1", []string{"2026"})
	if err == nil || !strings.Contains(err.Error(), "loopt al") {
		t.Fatalf("verwachtte een 'al bezig'-fout, kreeg: %v", err)
	}
}

func TestMigrationPullMediaGeeftDownloadfoutDoor(t *testing.T) {
	dir := t.TempDir()
	ssh := &fakeMigrationSSH{dlErr: io.ErrUnexpectedEOF}
	svc, _ := newMigrationService(t, ssh, dir)

	if _, err := svc.PullMedia("p1", "env-1", []string{"2026"}); err == nil {
		t.Fatal("verwachtte dat een downloadfout wordt doorgegeven")
	}
}

func TestMigrationPullMediaRootBestandenGebruiktEigenCommando(t *testing.T) {
	dir := t.TempDir()
	archief := maakTarGz(t, []tarEntry{{naam: "./los.jpg", inhoud: "los"}})
	ssh := &fakeMigrationSSH{archief: archief}
	svc, _ := newMigrationService(t, ssh, dir)

	if _, err := svc.PullMedia("p1", "env-1", []string{wortelBestandenMap}); err != nil {
		t.Fatalf("PullMedia: %v", err)
	}

	var gebruikt string
	for _, c := range ssh.snapshot() {
		if strings.Contains(c, "tar czf -") {
			gebruikt = c
		}
	}
	if !strings.Contains(gebruikt, "-maxdepth 1 -type f") {
		t.Errorf("voor losse bestanden verwacht een find-gebaseerd tar-commando, kreeg: %q", gebruikt)
	}
	if strings.Contains(gebruikt, wortelBestandenMap) {
		t.Error("de pseudo-mapnaam hoort nooit als echt pad in het commando te staan")
	}
}

func TestMigrationLocalUploadsPath(t *testing.T) {
	dir := t.TempDir()
	svc, _ := newMigrationService(t, &fakeMigrationSSH{}, dir)
	pad, err := svc.LocalUploadsPath("p1")
	if err != nil {
		t.Fatalf("LocalUploadsPath: %v", err)
	}
	if pad != filepath.Join(dir, "public", "wp-content", "uploads") {
		t.Errorf("pad = %q", pad)
	}
}

// Compile-time bewijs dat de echte SSH-client de interface van deze service
// dekt; anders valt een signature-wijziging pas op bij het opstarten.
var _ migrationSSH = (*sshadapter.Client)(nil)

var _ = bytes.MinRead
