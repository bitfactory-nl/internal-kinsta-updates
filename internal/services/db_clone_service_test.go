package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/domain"
)

// fakeDBSSH stands in for the SSH adapter in DBCloneService tests.
type fakeDBSSH struct {
	downloadContent []byte
	downloadErr     error
	runErr          error

	mu    sync.Mutex
	calls []string
}

func (f *fakeDBSSH) RunCommand(_ context.Context, _ sshadapter.Target, cmd string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	f.mu.Unlock()
	if strings.Contains(cmd, "wp search-replace") {
		return "RDM-DBSIZE:" + strconv.Itoa(len(f.downloadContent)) + "\n", f.runErr
	}
	return "", f.runErr
}

func (f *fakeDBSSH) Download(_ context.Context, _ sshadapter.Target, _ string, w io.Writer, onProgress func(int64)) error {
	if f.downloadErr != nil {
		return f.downloadErr
	}
	if _, err := w.Write(f.downloadContent); err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(int64(len(f.downloadContent)))
	}
	return nil
}

// dockerCall records one invocation of the fake dockerExec, for assertions on
// exactly what was sent to `docker exec` — this is how the tests prove no
// remote-mutating command ever reaches a container outside the expected,
// deliberately-local DROP/CREATE step.
type dockerCall struct {
	container string
	args      []string
	env       []string
	stdin     string
}

type fakeDockerExec struct {
	mu    sync.Mutex
	calls []dockerCall

	targetHasTables bool
	tableNames      []string
	siteURL         string
	failOn          func(args []string) bool
}

func (f *fakeDockerExec) exec(_ context.Context, container string, args, env []string, stdin io.Reader, stdout io.Writer) error {
	var in []byte
	if stdin != nil {
		in, _ = io.ReadAll(stdin)
	}
	f.mu.Lock()
	f.calls = append(f.calls, dockerCall{
		container: container,
		args:      append([]string{}, args...),
		env:       append([]string{}, env...),
		stdin:     string(in),
	})
	f.mu.Unlock()

	if f.failOn != nil && f.failOn(args) {
		return errors.New("gesimuleerde dockerExec-fout")
	}

	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "SHOW TABLES FROM"):
		if f.targetHasTables {
			io.WriteString(stdout, "wp_options\nwp_posts\n")
		}
	case len(args) > 0 && args[0] == "mysqldump":
		io.WriteString(stdout, "-- fake mysqldump output --\n")
	case strings.Contains(joined, "SELECT option_value"):
		io.WriteString(stdout, f.siteURL+"\n")
	case strings.Contains(joined, "SHOW TABLES") && !strings.Contains(joined, "FROM"):
		io.WriteString(stdout, strings.Join(f.tableNames, "\n"))
	}
	return nil
}

func (f *fakeDockerExec) snapshot() []dockerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dockerCall{}, f.calls...)
}

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newDBCloneService(t *testing.T, ssh dbSSH, docker *fakeDockerExec, projectPad string) (*DBCloneService, *ProjectService) {
	t.Helper()
	ps := NewProjectService(nil)
	ps.projects = []domain.Project{{
		ID:          "p1",
		Path:        projectPad,
		DisplayName: "web-vanluykennl",
		Config: domain.ProjectConfig{
			SSH: &domain.SSHTarget{User: "vanluykennl"},
		},
	}}
	svc := NewDBCloneService(ps, nil)
	svc.kinsta = fakeKinstaSSH{ep: EnvSSHEndpoint{Host: "1.2.3.4", Port: 12345, EnvName: "live"}}
	svc.ssh = ssh
	svc.secrets = &fakeSecrets{}
	svc.dockerExec = docker.exec
	svc.now = func() time.Time { return time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC) }
	return svc, ps
}

func writeEnvFile(t *testing.T, dir string) {
	t.Helper()
	content := "DB_NAME=dev_vanluykennl\nDB_USER=root\nDB_PASSWORD=secret\nDB_HOST=mysql\nAPP_DOMAIN=vanluykennl.test\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeTablePrefix(t *testing.T) {
	cases := map[string]string{
		"wp_":                  "wp_",
		"wp_5_":                "wp_5_",
		"":                     "wp_",
		"wp_'; DROP TABLE x--": "wp_",
		"wp_ OR 1=1":           "wp_",
	}
	for in, want := range cases {
		if got := sanitizeTablePrefix(in); got != want {
			t.Errorf("sanitizeTablePrefix(%q) = %q, wil %q", in, got, want)
		}
	}
}

func TestDBCloneProbe(t *testing.T) {
	ssh := &fakeDBSSHWithFixedOutput{out: strings.Join([]string{
		"RDM-SITEURL:https://vanluyken.nl",
		"RDM-PREFIX:wp_",
		"RDM-MULTISITE:no",
		"RDM-DBBYTES:1048576",
		"RDM-TMPFREEKB:2097152",
	}, "\n")}
	svc, _ := newDBCloneService(t, ssh, &fakeDockerExec{}, t.TempDir())

	probe, err := svc.Probe("p1", "env-1")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.SiteURL != "https://vanluyken.nl" || probe.TablePrefix != "wp_" {
		t.Errorf("probe = %+v", probe)
	}
	if probe.DBSizeBytes != 1048576 {
		t.Errorf("DBSizeBytes = %d", probe.DBSizeBytes)
	}
}

// fakeDBSSHWithFixedOutput always returns the same stdout, regardless of cmd —
// simpler than fakeDBSSH for probe-only tests.
type fakeDBSSHWithFixedOutput struct {
	out string
	err error
}

func (f *fakeDBSSHWithFixedOutput) RunCommand(context.Context, sshadapter.Target, string) (string, error) {
	return f.out, f.err
}
func (f *fakeDBSSHWithFixedOutput) Download(context.Context, sshadapter.Target, string, io.Writer, func(int64)) error {
	return nil
}

func TestDBCloneRejectsConcurrentRunForSameSlot(t *testing.T) {
	docker := &fakeDockerExec{}
	svc, _ := newDBCloneService(t, &fakeDBSSH{}, docker, t.TempDir())

	if !svc.claim("p1@env-1") {
		t.Fatal("test setup: claim zou moeten lukken")
	}
	defer svc.release("p1@env-1")

	_, err := svc.Clone("p1", "env-1", domain.DBCloneRequest{LocalDBName: "dev_vanluykennl", LocalDBHost: "mysql"})
	if err == nil || !strings.Contains(err.Error(), "loopt al een kloon-actie") {
		t.Fatalf("verwachtte een 'al bezig'-fout, kreeg: %v", err)
	}
}

func TestDBCloneRejectsInvalidLocalDBName(t *testing.T) {
	svc, _ := newDBCloneService(t, &fakeDBSSH{}, &fakeDockerExec{}, t.TempDir())

	cases := []string{"mysql", "MySQL", "information_schema", "Robert'); DROP TABLE--", "met spatie", "HoofdLetters"}
	for _, name := range cases {
		_, err := svc.Clone("p1", "env-1", domain.DBCloneRequest{LocalDBName: name, LocalDBHost: "mysql"})
		if err == nil {
			t.Errorf("LocalDBName %q had geweigerd moeten worden", name)
		}
	}
}

func TestDBCloneRejectsUnknownDBHost(t *testing.T) {
	svc, _ := newDBCloneService(t, &fakeDBSSH{}, &fakeDockerExec{}, t.TempDir())
	_, err := svc.Clone("p1", "env-1", domain.DBCloneRequest{LocalDBName: "dev_vanluykennl", LocalDBHost: "postgres"})
	if err == nil || !strings.Contains(err.Error(), "onbekende database-host") {
		t.Fatalf("verwachtte een fout over onbekende database-host, kreeg: %v", err)
	}
}

func TestDBCloneFullPipelineNeverMutatesRemoteAndSkipsBackupWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir)

	dump := gzipBytes(t, "-- fake production dump, already url-rewritten by wp search-replace --\n")
	ssh := &fakeDBSSH{downloadContent: dump}
	docker := &fakeDockerExec{
		targetHasTables: false, // doel-DB bestaat nog niet -> geen backup
		tableNames:      []string{"wp_options", "wp_posts", "wp_users"},
		siteURL:         "https://vanluykennl.test",
	}
	svc, _ := newDBCloneService(t, ssh, docker, dir)

	result, err := svc.Clone("p1", "env-1", domain.DBCloneRequest{
		ProdSiteURL: "https://vanluyken.nl",
		LocalURL:    "https://vanluykennl.test",
		LocalDBName: "dev_vanluykennl",
		LocalDBHost: "mysql",
		TablePrefix: "wp_",
	})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if result.BackupPath != "" {
		t.Errorf("BackupPath = %q, wil leeg (doel-DB bestond nog niet)", result.BackupPath)
	}
	if result.SiteURLAfter != "https://vanluykennl.test" {
		t.Errorf("SiteURLAfter = %q", result.SiteURLAfter)
	}
	if result.TablesImported != 3 {
		t.Errorf("TablesImported = %d, wil 3", result.TablesImported)
	}
	if result.MultisiteFixApplied {
		t.Error("MultisiteFixApplied zou false moeten zijn (single-site request)")
	}

	// De belangrijkste garantie: geen enkel SSH-commando naar de server mag
	// muteren, en elke wp search-replace-aanroep moet --export= bevatten.
	for _, cmd := range ssh.calls {
		for _, verboden := range []string{"wp db import", "wp option update", " UPDATE ", " DELETE ", "DROP DATABASE", "TRUNCATE", "INSERT INTO"} {
			if strings.Contains(cmd, verboden) {
				t.Errorf("SSH-commando bevat verboden %q: %s", verboden, cmd)
			}
		}
		if strings.Contains(cmd, "wp search-replace") && !strings.Contains(cmd, "--export=") {
			t.Errorf("wp search-replace zonder --export=: %s", cmd)
		}
	}

	// Op de lokale docker-container is precies één DROP/CREATE toegestaan: die
	// van de eigen doel-database. Niets anders mag UPDATE/DELETE bevatten
	// (behalve de bewust-lokale multisite-fix, die hier niet gebeurt).
	for _, call := range docker.snapshot() {
		joined := strings.Join(call.args, " ")
		if strings.Contains(joined, "DROP DATABASE") && !strings.Contains(joined, "DROP DATABASE IF EXISTS dev_vanluykennl") {
			t.Errorf("onverwachte DROP DATABASE: %v", call.args)
		}
		if strings.Contains(joined, " DELETE ") {
			t.Errorf("onverwachte DELETE op de lokale container: %v", call.args)
		}
	}
}

func TestDBCloneBacksUpExistingLocalDatabase(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir)

	dump := gzipBytes(t, "-- fake dump --\n")
	ssh := &fakeDBSSH{downloadContent: dump}
	docker := &fakeDockerExec{
		targetHasTables: true, // doel-DB bestaat al en heeft tabellen -> backup verwacht
		tableNames:      []string{"wp_options"},
		siteURL:         "https://vanluykennl.test",
	}
	svc, _ := newDBCloneService(t, ssh, docker, dir)

	result, err := svc.Clone("p1", "env-1", domain.DBCloneRequest{
		ProdSiteURL: "https://vanluyken.nl",
		LocalURL:    "https://vanluykennl.test",
		LocalDBName: "dev_vanluykennl",
		LocalDBHost: "mysql",
	})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if result.BackupPath == "" {
		t.Fatal("BackupPath is leeg, wil een echte backup omdat de doel-DB al tabellen had")
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Errorf("backupbestand niet gevonden: %v", err)
	}
	if _, err := os.Stat(result.BackupPath + ".json"); err != nil {
		t.Errorf("sidecar-json niet gevonden: %v", err)
	}
}

func TestRestoreBackupRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir)
	svc, _ := newDBCloneService(t, &fakeDBSSH{}, &fakeDockerExec{}, dir)

	err := svc.RestoreBackup("p1", "/etc/../etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "ongeldig backup-pad") {
		t.Fatalf("verwachtte een ongeldig-pad-fout, kreeg: %v", err)
	}

	backupDir, _ := dbCloneBackupDir("p1")
	traversal := filepath.Join(backupDir, "..", "..", "evil.sql.gz")
	err = svc.RestoreBackup("p1", traversal)
	if err == nil || !strings.Contains(err.Error(), "ongeldig backup-pad") {
		t.Fatalf("verwachtte een ongeldig-pad-fout voor traversal, kreeg: %v", err)
	}
}

func TestRestoreBackupReimportsKnownBackup(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir)
	docker := &fakeDockerExec{}
	svc, _ := newDBCloneService(t, &fakeDBSSH{}, docker, dir)

	backupDir, _ := dbCloneBackupDir("p1")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dump := gzipBytes(t, "-- backup content --\n")
	backupPath := filepath.Join(backupDir, "backup-20260101-000000.sql.gz")
	if err := os.WriteFile(backupPath, dump, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath+".json", []byte(`{"dbName":"dev_vanluykennl","takenAt":"2026-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.RestoreBackup("p1", backupPath); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	found := false
	for _, call := range docker.snapshot() {
		if strings.Contains(strings.Join(call.args, " "), "CREATE DATABASE dev_vanluykennl") {
			found = true
		}
	}
	if !found {
		t.Error("RestoreBackup heeft de doel-database niet opnieuw aangemaakt")
	}
}
