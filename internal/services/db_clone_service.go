package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	dockeradapter "github.com/rdm/sites-tool/internal/adapters/docker"
	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// dbCloneTimeout bounds the whole clone round trip: export, download and
// import of a customer database can genuinely take minutes for a large site.
const dbCloneTimeout = 20 * time.Minute

// dbProbeTimeout bounds the much cheaper connection check.
const dbProbeTimeout = 45 * time.Second

// dbCloneBackupsToKeep caps how many local backups pile up per project.
const dbCloneBackupsToKeep = 3

// dbSSH is the subset of the SSH adapter DBCloneService needs.
type dbSSH interface {
	RunCommand(ctx context.Context, t sshadapter.Target, cmd string) (string, error)
	Download(ctx context.Context, t sshadapter.Target, remotePath string, w io.Writer, onProgress func(int64)) error
}

var reValidDBName = regexp.MustCompile(`^[a-z0-9_]+$`)

var reservedDBNames = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
}

func validateLocalDBName(name string) error {
	if !reValidDBName.MatchString(name) {
		return fmt.Errorf("ongeldige lokale databasenaam %q: alleen kleine letters, cijfers en underscores toegestaan", name)
	}
	if reservedDBNames[strings.ToLower(name)] {
		return fmt.Errorf("%q is een gereserveerde MySQL-systeemdatabase en kan niet als doel gebruikt worden", name)
	}
	return nil
}

// containerForHost maps a project's .env DB_HOST to the local docker-compose
// container that actually runs that MySQL instance.
func containerForHost(host string) (string, error) {
	switch host {
	case "mysql":
		return "bitf-mysql", nil
	case "mysql84":
		return "bitf-mysql84", nil
	default:
		return "", fmt.Errorf("onbekende database-host %q", host)
	}
}

// portForHost maps a project's .env DB_HOST to the port that container
// publishes on 127.0.0.1, for opening a direct connection from the host.
func portForHost(host string) (int, error) {
	switch host {
	case "mysql":
		return 3306, nil
	case "mysql84":
		return 3307, nil
	default:
		return 0, fmt.Errorf("onbekende database-host %q", host)
	}
}

func envOrDefault(env map[string]string, key, def string) string {
	if v := strings.TrimSpace(env[key]); v != "" {
		return v
	}
	return def
}

// DBCloneService clones a Kinsta project's production database into the
// developer's own local docker MySQL, rewriting URLs on the way. The
// production database is only ever read from — every SSH command it sends is
// built by db_clone_commands.go, which is covered by a guard test that fails
// if any of those commands could mutate the remote database.
type DBCloneService struct {
	projects *ProjectService
	kinsta   kinstaSSHSource
	ssh      dbSSH
	secrets  secretStore
	emitter  eventEmitter

	dockerExec func(ctx context.Context, container string, args, env []string, stdin io.Reader, stdout io.Writer) error
	now        func() time.Time

	bezigMu sync.Mutex
	bezig   map[string]bool
}

func NewDBCloneService(projects *ProjectService, kinsta *KinstaService) *DBCloneService {
	return &DBCloneService{
		projects:   projects,
		kinsta:     kinsta,
		ssh:        sshadapter.NewClient(),
		secrets:    keychainSecrets{},
		dockerExec: dockeradapter.Exec,
		now:        time.Now,
		bezig:      map[string]bool{},
	}
}

// SetApp wires the Wails event emitter used for clone progress.
func (s *DBCloneService) SetApp(app *application.App) {
	s.emitter = app.Event
}

func (s *DBCloneService) claim(slot string) bool {
	s.bezigMu.Lock()
	defer s.bezigMu.Unlock()
	if s.bezig[slot] {
		return false
	}
	s.bezig[slot] = true
	return true
}

func (s *DBCloneService) release(slot string) {
	s.bezigMu.Lock()
	delete(s.bezig, slot)
	s.bezigMu.Unlock()
}

func (s *DBCloneService) emit(projectID string, p domain.DBCloneProgress) {
	if s.emitter == nil {
		return
	}
	s.emitter.Emit(fmt.Sprintf("db:%s:progress", projectID), p)
}

func (s *DBCloneService) project(id string) (domain.Project, error) {
	p, ok := s.projects.Get(id)
	if !ok {
		return domain.Project{}, fmt.Errorf("project %q niet gevonden", id)
	}
	return p, nil
}

// target resolves the SSH connection for one environment. Deliberately
// duplicated from MediaService.target rather than shared — MediaService and
// PluginService each own this same small amount of glue, which is the
// existing style in this package.
func (s *DBCloneService) target(projectID, envID string) (mediaTarget, domain.Project, error) {
	p, err := s.project(projectID)
	if err != nil {
		return mediaTarget{}, domain.Project{}, err
	}
	ep, err := s.kinsta.EnvironmentSSH(projectID, envID)
	if err != nil {
		return mediaTarget{}, p, err
	}
	var wachtwoord string
	if p.Config.SSH != nil && p.Config.SSH.Password != "" {
		if wachtwoord, err = s.secrets.Get(p.Config.SSH.Password); err != nil {
			return mediaTarget{}, p, fmt.Errorf("wachtwoord uit de keychain halen: %w", err)
		}
	}
	tgt, err := mediaSSHTarget(p, ep, wachtwoord)
	return tgt, p, err
}

// LocalDefaults reads the project's own .env file — no SSH involved — to
// prefill the target fields in the UI.
func (s *DBCloneService) LocalDefaults(projectID string) (domain.LocalEnvDefaults, error) {
	p, err := s.project(projectID)
	if err != nil {
		return domain.LocalEnvDefaults{}, err
	}
	env, err := config.LoadProjectEnv(p.Path)
	if err != nil {
		return domain.LocalEnvDefaults{}, err
	}
	def := domain.LocalEnvDefaults{
		DBName: env["DB_NAME"],
		DBHost: env["DB_HOST"],
	}
	if appDomain := strings.TrimSpace(env["APP_DOMAIN"]); appDomain != "" {
		def.URL = "https://" + appDomain
	}
	return def, nil
}

// Probe checks what a clone would involve: the canonical site URL, table
// prefix, multisite status, and database size.
func (s *DBCloneService) Probe(projectID, envID string) (domain.DBProbe, error) {
	tgt, _, err := s.target(projectID, envID)
	if err != nil {
		return domain.DBProbe{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbProbeTimeout)
	defer cancel()

	out, err := s.ssh.RunCommand(ctx, tgt.SSH, buildDBProbeCommand(tgt.Webroot))
	probe := parseDBProbe(out)
	if err != nil && probe.SiteURL == "" {
		return domain.DBProbe{}, fmt.Errorf("verbinding met %s: %w", tgt.SSH.Host, err)
	}
	return probe, nil
}

// SaveLocalURL writes the local URL into deploy_conf.json's link.local
// (uncommitted — that's the caller's own choice) and refreshes the in-memory
// project cache so the UI sees it immediately.
func (s *DBCloneService) SaveLocalURL(projectID, url string) error {
	p, err := s.project(projectID)
	if err != nil {
		return err
	}
	if err := config.SaveDeployLinkLocal(p.Path, url); err != nil {
		return err
	}
	_, err = s.projects.RefreshOne(projectID)
	return err
}

// OpenInApp opens a local connection to the just-cloned database in the
// user's configured database program. No password ever reaches the frontend:
// this method resolves it itself from the project's .env.
func (s *DBCloneService) OpenInApp(projectID, localDBHost, localDBName, appName string) error {
	p, err := s.project(projectID)
	if err != nil {
		return err
	}
	env, err := config.LoadProjectEnv(p.Path)
	if err != nil {
		return err
	}
	port, err := portForHost(localDBHost)
	if err != nil {
		return err
	}
	dbUser := envOrDefault(env, "DB_USER", "root")
	dbPass := envOrDefault(env, "DB_PASSWORD", "secret")
	mysqlURL := BuildMySQLURL("127.0.0.1", port, dbUser, dbPass, localDBName)
	return OpenInApp(appName, mysqlURL)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("willekeurige naam genereren: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// backupSidecar is the small JSON file written next to a local backup, so
// RestoreBackup knows which database it belongs to.
type backupSidecar struct {
	DBName  string `json:"dbName"`
	TakenAt string `json:"takenAt"`
}

func dbCloneBackupDir(projectID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "rdm", "db-clones", projectID), nil
}

// Clone runs the full pipeline: back up the current local database, export a
// URL-rewritten dump from production (read-only), download it, import it
// locally, and — for multisite — fix up the two bare-domain columns that a
// full-URL search-replace cannot reach.
func (s *DBCloneService) Clone(projectID, envID string, req domain.DBCloneRequest) (domain.DBCloneResult, error) {
	slot := projectID + "@" + envID
	if !s.claim(slot) {
		return domain.DBCloneResult{}, fmt.Errorf("er loopt al een kloon-actie voor dit project")
	}
	defer s.release(slot)

	if err := validateLocalDBName(req.LocalDBName); err != nil {
		return domain.DBCloneResult{}, err
	}
	container, err := containerForHost(req.LocalDBHost)
	if err != nil {
		return domain.DBCloneResult{}, err
	}

	tgt, p, err := s.target(projectID, envID)
	if err != nil {
		return domain.DBCloneResult{}, err
	}
	env, err := config.LoadProjectEnv(p.Path)
	if err != nil {
		return domain.DBCloneResult{}, err
	}
	dbUser := envOrDefault(env, "DB_USER", "root")
	dbPass := envOrDefault(env, "DB_PASSWORD", "secret")
	mysqlEnv := []string{"MYSQL_PWD=" + dbPass}

	ctx, cancel := context.WithTimeout(context.Background(), dbCloneTimeout)
	defer cancel()

	result := domain.DBCloneResult{LocalDBName: req.LocalDBName, SiteURLBefore: req.ProdSiteURL}

	// --- Backup ---------------------------------------------------------
	s.emit(projectID, domain.DBCloneProgress{Phase: "backup", Detail: "controleren of er al een lokale database staat"})
	backupPath, warn, err := s.backupIfExists(ctx, container, dbUser, mysqlEnv, projectID, req.LocalDBName)
	if err != nil {
		return domain.DBCloneResult{}, fmt.Errorf("backup van de huidige lokale database mislukt: %w", err)
	}
	if warn != "" {
		result.Warnings = append(result.Warnings, warn)
	}
	result.BackupPath = backupPath

	// --- Export (remote, read-only) -------------------------------------
	s.emit(projectID, domain.DBCloneProgress{Phase: "export", Detail: "dump maken op de server (productie wordt niet gewijzigd)"})
	rnd, err := randomHex(8)
	if err != nil {
		return domain.DBCloneResult{}, err
	}
	remoteFile := "/tmp/rdm-db-" + rnd + ".sql"
	out, err := s.ssh.RunCommand(ctx, tgt.SSH, buildDBExportCommand(tgt.Webroot, req.ProdSiteURL, req.LocalURL, req.Multisite, remoteFile))
	dumpBytes := parseDBExportSize(out)
	if err != nil {
		return domain.DBCloneResult{}, s.failureWithBackupNote(projectID, backupPath, fmt.Errorf("export op de server mislukt: %w: %s", err, strings.TrimSpace(out)))
	}
	if dumpBytes == 0 {
		return domain.DBCloneResult{}, s.failureWithBackupNote(projectID, backupPath, fmt.Errorf("export op de server leverde geen bruikbare dump op: %s", strings.TrimSpace(out)))
	}
	result.DumpBytes = dumpBytes
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_, _ = s.ssh.RunCommand(cctx, tgt.SSH, buildRemoteCleanupCommand(remoteFile))
	}()

	// --- Download ---------------------------------------------------------
	s.emit(projectID, domain.DBCloneProgress{Phase: "download", Detail: "dump ophalen", Total: dumpBytes})
	tmpFile, err := os.CreateTemp("", "rdm-db-*.sql.gz")
	if err != nil {
		return domain.DBCloneResult{}, s.failureWithBackupNote(projectID, backupPath, err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	dlErr := s.ssh.Download(ctx, tgt.SSH, remoteFile+".gz", tmpFile, func(written int64) {
		s.emit(projectID, domain.DBCloneProgress{Phase: "download", Detail: "dump ophalen", Bytes: written, Total: dumpBytes})
	})
	closeErr := tmpFile.Close()
	if dlErr != nil {
		return domain.DBCloneResult{}, s.failureWithBackupNote(projectID, backupPath, fmt.Errorf("download mislukt: %w", dlErr))
	}
	if closeErr != nil {
		return domain.DBCloneResult{}, s.failureWithBackupNote(projectID, backupPath, closeErr)
	}

	// --- Import (local) -----------------------------------------------
	s.emit(projectID, domain.DBCloneProgress{Phase: "import", Detail: "database lokaal aanmaken en vullen"})
	if err := s.importLocal(ctx, container, dbUser, mysqlEnv, req.LocalDBName, tmpPath); err != nil {
		return domain.DBCloneResult{}, s.failureWithBackupNote(projectID, backupPath, fmt.Errorf("lokaal importeren mislukt: %w", err))
	}

	// --- Multisite fix (local only) -------------------------------------
	if req.Multisite {
		s.emit(projectID, domain.DBCloneProgress{Phase: "multisite-fix", Detail: "domeinen in wp_blogs/wp_site bijwerken (beta)"})
		prefix := req.TablePrefix
		if prefix == "" {
			prefix = "wp_"
		}
		sql := buildLocalMultisiteDomainFixSQL(prefix, bareDomain(req.ProdSiteURL), bareDomain(req.LocalURL))
		var stderr bytes.Buffer
		if err := s.dockerExec(ctx, container, []string{"mysql", "-u" + dbUser, "-e", sql, req.LocalDBName}, mysqlEnv, nil, &stderr); err != nil {
			result.Warnings = append(result.Warnings, "multisite-domeinfix (beta) is mislukt: "+err.Error())
		} else {
			result.MultisiteFixApplied = true
			result.Warnings = append(result.Warnings, "multisite-domeinfix is best-effort (beta); controleer de subsites handmatig")
		}
	}

	// --- Verify -----------------------------------------------------------
	s.emit(projectID, domain.DBCloneProgress{Phase: "verify", Detail: "resultaat controleren"})
	prefix := req.TablePrefix
	if prefix == "" {
		prefix = "wp_"
	}
	var siteurlOut bytes.Buffer
	_ = s.dockerExec(ctx, container, []string{"mysql", "-N", "-u" + dbUser, "-e",
		"SELECT option_value FROM " + prefix + "options WHERE option_name='siteurl'", req.LocalDBName}, mysqlEnv, nil, &siteurlOut)
	result.SiteURLAfter = strings.TrimSpace(siteurlOut.String())

	var tablesOut bytes.Buffer
	_ = s.dockerExec(ctx, container, []string{"mysql", "-N", "-u" + dbUser, "-e", "SHOW TABLES", req.LocalDBName}, mysqlEnv, nil, &tablesOut)
	result.TablesImported = len(strings.Fields(strings.TrimSpace(tablesOut.String())))

	s.emit(projectID, domain.DBCloneProgress{Phase: "done", Detail: "klaar"})
	return result, nil
}

// failureWithBackupNote emits an "error" progress event pointing at the
// backup before returning err, so the UI can surface the recovery path — the
// backup is never restored automatically.
func (s *DBCloneService) failureWithBackupNote(projectID, backupPath string, err error) error {
	detail := err.Error()
	if backupPath != "" {
		detail += " (backup staat op " + backupPath + ")"
	}
	s.emit(projectID, domain.DBCloneProgress{Phase: "error", Detail: detail})
	return err
}

// backupIfExists dumps the current local database before it gets overwritten,
// unless it doesn't exist yet or has no tables. Returns the backup path (or
// empty) and an optional warning to surface in the result.
func (s *DBCloneService) backupIfExists(ctx context.Context, container, dbUser string, env []string, projectID, dbName string) (string, string, error) {
	var tables bytes.Buffer
	err := s.dockerExec(ctx, container, []string{"mysql", "-N", "-u" + dbUser, "-e", "SHOW TABLES FROM " + dbName}, env, nil, &tables)
	if err != nil || strings.TrimSpace(tables.String()) == "" {
		return "", "geen backup gemaakt: de lokale database " + dbName + " bestond nog niet of was leeg", nil
	}

	dir, err := dbCloneBackupDir(projectID)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	stamp := s.now().Format("20060102-150405")
	backupPath := filepath.Join(dir, "backup-"+stamp+".sql.gz")

	f, err := os.Create(backupPath)
	if err != nil {
		return "", "", err
	}
	gz := gzip.NewWriter(f)
	dumpErr := s.dockerExec(ctx, container, []string{"mysqldump", "-u" + dbUser, dbName}, env, nil, gz)
	closeErr := gz.Close()
	fCloseErr := f.Close()
	if dumpErr != nil {
		os.Remove(backupPath)
		return "", "", dumpErr
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if fCloseErr != nil {
		return "", "", fCloseErr
	}

	sidecar, _ := json.Marshal(backupSidecar{DBName: dbName, TakenAt: s.now().Format(time.RFC3339)})
	_ = os.WriteFile(backupPath+".json", sidecar, 0o644)

	s.pruneOldBackups(dir)
	return backupPath, "", nil
}

func (s *DBCloneService) pruneOldBackups(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "backup-*.sql.gz"))
	if err != nil || len(matches) <= dbCloneBackupsToKeep {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-dbCloneBackupsToKeep] {
		os.Remove(old)
		os.Remove(old + ".json")
	}
}

// importLocal drops and recreates the target database, then streams the
// downloaded gzip dump into it. dbName was already validated by Clone with
// the ^[a-z0-9_]+$ regex, which rules out any SQL-injection through it — it
// is checked again here (defense in depth) before being interpolated into a
// SQL statement.
func (s *DBCloneService) importLocal(ctx context.Context, container, dbUser string, env []string, dbName, tmpPath string) error {
	if err := validateLocalDBName(dbName); err != nil {
		return err
	}
	create := "DROP DATABASE IF EXISTS " + dbName + "; CREATE DATABASE " + dbName + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_520_ci"
	var stderr bytes.Buffer
	if err := s.dockerExec(ctx, container, []string{"mysql", "-u" + dbUser, "-e", create}, env, nil, &stderr); err != nil {
		return err
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gedownloade dump uitpakken: %w", err)
	}
	defer gz.Close()

	return s.dockerExec(ctx, container, []string{"mysql", "-u" + dbUser, dbName}, env, gz, io.Discard)
}

// RestoreBackup re-imports a previously taken local backup. backupPath must
// live under this project's own backup directory — anything else, including
// any ".." traversal, is refused.
func (s *DBCloneService) RestoreBackup(projectID, backupPath string) error {
	dir, err := dbCloneBackupDir(projectID)
	if err != nil {
		return err
	}
	clean := filepath.Clean(backupPath)
	if !strings.HasPrefix(clean, filepath.Clean(dir)+string(filepath.Separator)) {
		return fmt.Errorf("ongeldig backup-pad")
	}
	sidecarRaw, err := os.ReadFile(clean + ".json")
	if err != nil {
		return fmt.Errorf("herstelinformatie bij deze backup niet gevonden: %w", err)
	}
	var sidecar backupSidecar
	if err := json.Unmarshal(sidecarRaw, &sidecar); err != nil {
		return fmt.Errorf("herstelinformatie onleesbaar: %w", err)
	}

	p, err := s.project(projectID)
	if err != nil {
		return err
	}
	env, err := config.LoadProjectEnv(p.Path)
	if err != nil {
		return err
	}
	dbHost := envOrDefault(env, "DB_HOST", "mysql")
	container, err := containerForHost(dbHost)
	if err != nil {
		return err
	}
	dbUser := envOrDefault(env, "DB_USER", "root")
	dbPass := envOrDefault(env, "DB_PASSWORD", "secret")

	ctx, cancel := context.WithTimeout(context.Background(), dbCloneTimeout)
	defer cancel()
	return s.importLocal(ctx, container, dbUser, []string{"MYSQL_PWD=" + dbPass}, sidecar.DBName, clean)
}
