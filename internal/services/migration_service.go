package services

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// mediaPullTimeout bounds a whole pull. A full uploads folder of a large site
// is genuinely gigabytes over a single SSH pipe, so this is generous.
const mediaPullTimeout = 90 * time.Minute

// uploadsListTimeout bounds the cheap folder listing.
const uploadsListTimeout = 60 * time.Second

// wortelBestandenMap is the pseudo-folder name used in the UI for the loose
// files that sit directly in uploads, outside any subdirectory.
const wortelBestandenMap = "(losse bestanden in uploads)"

// migrationSSH is the subset of the SSH adapter MigrationService needs.
type migrationSSH interface {
	RunCommand(ctx context.Context, t sshadapter.Target, cmd string) (string, error)
	DownloadCommand(ctx context.Context, t sshadapter.Target, cmd string, w io.Writer, onProgress func(int64)) error
}

// MigrationService handles pulling a production environment down to a local
// one: the URL mapping used during a migration, and copying wp-content/uploads
// from the server into the local checkout. It only ever reads from production.
type MigrationService struct {
	projects *ProjectService
	kinsta   kinstaSSHSource
	ssh      migrationSSH
	secrets  secretStore
	emitter  eventEmitter

	bezigMu sync.Mutex
	bezig   map[string]bool
}

func NewMigrationService(projects *ProjectService, kinsta *KinstaService) *MigrationService {
	return &MigrationService{
		projects: projects,
		kinsta:   kinsta,
		ssh:      sshadapter.NewClient(),
		secrets:  keychainSecrets{},
		bezig:    map[string]bool{},
	}
}

// SetApp wires the Wails event emitter used for pull progress.
func (s *MigrationService) SetApp(app *application.App) {
	s.emitter = app.Event
}

func (s *MigrationService) claim(slot string) bool {
	s.bezigMu.Lock()
	defer s.bezigMu.Unlock()
	if s.bezig[slot] {
		return false
	}
	s.bezig[slot] = true
	return true
}

func (s *MigrationService) release(slot string) {
	s.bezigMu.Lock()
	delete(s.bezig, slot)
	s.bezigMu.Unlock()
}

func (s *MigrationService) emit(projectID string, p domain.MediaPullProgress) {
	if s.emitter == nil {
		return
	}
	s.emitter.Emit(fmt.Sprintf("migration:%s:media", projectID), p)
}

func (s *MigrationService) project(id string) (domain.Project, error) {
	p, ok := s.projects.Get(id)
	if !ok {
		return domain.Project{}, fmt.Errorf("project %q niet gevonden", id)
	}
	return p, nil
}

// target resolves the SSH connection for one environment, mirroring the same
// small piece of glue MediaService and DBCloneService each own.
func (s *MigrationService) target(projectID, envID string) (mediaTarget, domain.Project, error) {
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

// GetSettings returns the stored migration mapping. When .rdm.yml has none
// yet, sensible values are derived from the project's own .env and
// deploy_conf.json so the form opens filled in rather than blank — but nothing
// is written until the user saves.
func (s *MigrationService) GetSettings(projectID string) (domain.MigrationCfg, error) {
	p, err := s.project(projectID)
	if err != nil {
		return domain.MigrationCfg{}, err
	}
	if p.Config.Migration != nil {
		return *p.Config.Migration, nil
	}

	env, err := config.LoadProjectEnv(p.Path)
	if err != nil {
		return domain.MigrationCfg{}, err
	}
	cfg := domain.MigrationCfg{
		Multisite: envBool(env["MULTISITE"]),
		ProdURL:   p.Deploy.Link.Prod,
		LocalURL:  p.Deploy.Link.Local,
	}
	if cfg.LocalURL == "" {
		if appDomain := strings.TrimSpace(env["APP_DOMAIN"]); appDomain != "" {
			cfg.LocalURL = "https://" + appDomain
		}
	}
	if cfg.ProdURL != "" {
		cfg.ProdDomain = bareDomain(cfg.ProdURL)
	}
	cfg.LocalDomain = strings.TrimSpace(env["DOMAIN_CURRENT_SITE"])
	if cfg.LocalDomain == "" && cfg.LocalURL != "" {
		cfg.LocalDomain = bareDomain(cfg.LocalURL)
	}
	return cfg, nil
}

// SaveSettings writes the migration mapping to .rdm.yml. That file lives in the
// customer's repo, so this only writes it — committing is the user's own call,
// same as with deploy_conf.json's link.local.
func (s *MigrationService) SaveSettings(projectID string, cfg domain.MigrationCfg) error {
	p, err := s.project(projectID)
	if err != nil {
		return err
	}

	cfg.ProdURL = strings.TrimSpace(cfg.ProdURL)
	cfg.LocalURL = strings.TrimSpace(cfg.LocalURL)
	cfg.ProdDomain = strings.TrimSpace(cfg.ProdDomain)
	cfg.LocalDomain = strings.TrimSpace(cfg.LocalDomain)

	// Lege paren zijn niets waard en zouden een search-replace op een lege
	// string worden; die filteren we eruit in plaats van ze op te slaan.
	schoon := make([]domain.DomainPair, 0, len(cfg.ExtraDomains))
	for _, dp := range cfg.ExtraDomains {
		prod, lokaal := strings.TrimSpace(dp.Prod), strings.TrimSpace(dp.Local)
		if prod == "" || lokaal == "" {
			continue
		}
		schoon = append(schoon, domain.DomainPair{Prod: prod, Local: lokaal})
	}
	cfg.ExtraDomains = schoon

	projectCfg := p.Config
	projectCfg.Migration = &cfg
	if err := config.SaveProject(p.Path, projectCfg); err != nil {
		return fmt.Errorf("opslaan .rdm.yml: %w", err)
	}
	s.projects.UpdateProjectConfig(projectID, projectCfg)
	return nil
}

// ListUploadFolders lists the top-level folders inside wp-content/uploads on
// the server with their sizes, so the user can choose what to pull.
func (s *MigrationService) ListUploadFolders(projectID, envID string) ([]domain.UploadFolder, error) {
	tgt, _, err := s.target(projectID, envID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), uploadsListTimeout)
	defer cancel()

	out, err := s.ssh.RunCommand(ctx, tgt.SSH, buildUploadsListCommand(tgt.Webroot))
	if melding := eersteGroep(reMediaErr, out); melding != "" {
		return nil, fmt.Errorf("op de server: %s", melding)
	}
	folders := parseUploadFolders(out)
	if err != nil && len(folders) == 0 {
		return nil, fmt.Errorf("mappen ophalen van %s: %w", tgt.SSH.Host, err)
	}
	if n := parseUploadRootFileCount(out); n > 0 {
		folders = append(folders, domain.UploadFolder{Name: wortelBestandenMap})
	}
	return folders, nil
}

// LocalUploadsPath is where a pull writes: the uploads folder of the local
// checkout. That path is covered by the customer repo's .gitignore
// (/public/wp-content/*), so pulled media can never end up in a commit.
func (s *MigrationService) LocalUploadsPath(projectID string) (string, error) {
	p, err := s.project(projectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(p.Path, "public", uploadsPad), nil
}

// PullMedia copies the selected uploads folders from production into the local
// checkout, overwriting what is already there so the local copy matches
// production exactly.
//
// Each folder is streamed as a gzipped tar straight out of the SSH session and
// unpacked on the fly, so nothing large is ever written to the customer's
// server and no full copy has to fit in memory here either.
func (s *MigrationService) PullMedia(projectID, envID string, folders []string) (domain.MediaPullResult, error) {
	if len(folders) == 0 {
		return domain.MediaPullResult{}, fmt.Errorf("geen mappen gekozen om te pullen")
	}

	slot := projectID + "@" + envID
	if !s.claim(slot) {
		return domain.MediaPullResult{}, fmt.Errorf("er loopt al een media-pull voor deze omgeving")
	}
	defer s.release(slot)

	tgt, _, err := s.target(projectID, envID)
	if err != nil {
		return domain.MediaPullResult{}, err
	}
	doelWortel, err := s.LocalUploadsPath(projectID)
	if err != nil {
		return domain.MediaPullResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mediaPullTimeout)
	defer cancel()

	result := domain.MediaPullResult{LocalPath: doelWortel}
	for i, folder := range folders {
		s.emit(projectID, domain.MediaPullProgress{
			Phase: "pull", Folder: folder, Detail: "map ophalen en uitpakken",
			FolderIndex: i + 1, FolderTotal: len(folders),
		})

		cmd := buildUploadsTarCommand(tgt.Webroot, folder)
		if folder == wortelBestandenMap {
			cmd = buildUploadsRootFilesTarCommand(tgt.Webroot)
		}

		res, err := s.pullEenMap(ctx, tgt, cmd, doelWortel, projectID, folder, i, len(folders), result.BytesWritten)
		if err != nil {
			s.emit(projectID, domain.MediaPullProgress{Phase: "error", Folder: folder, Detail: err.Error()})
			return result, fmt.Errorf("map %q ophalen: %w", folder, err)
		}
		result.Folders = append(result.Folders, folder)
		result.FilesWritten += res.Files
		result.BytesWritten += res.Bytes
		for _, sk := range res.Skipped {
			result.Warnings = append(result.Warnings, "overgeslagen: "+sk)
		}
	}

	s.emit(projectID, domain.MediaPullProgress{
		Phase: "done", Detail: "klaar",
		Files: result.FilesWritten, Bytes: result.BytesWritten,
	})
	return result, nil
}

// pullEenMap streams and unpacks one folder. The SSH stdout is piped straight
// into the tar reader, so the transfer and the extraction run at the same time
// instead of needing a full local copy of the archive first.
func (s *MigrationService) pullEenMap(
	ctx context.Context, tgt mediaTarget, cmd, doelWortel, projectID, folder string,
	index, totaal int, alGeschreven int64,
) (extractResultaat, error) {
	pr, pw := io.Pipe()

	type uit struct {
		res extractResultaat
		err error
	}
	klaar := make(chan uit, 1)
	go func() {
		bestanden := 0
		res, err := pakUitOnder(pr, doelWortel, func(_ string, geschreven int64) {
			bestanden++
			// Niet elk bestand een event: een uploads-map heeft er tienduizenden
			// en de UI heeft aan een update per 25 meer dan genoeg.
			if bestanden%25 != 0 {
				return
			}
			s.emit(projectID, domain.MediaPullProgress{
				Phase: "pull", Folder: folder, Detail: "uitpakken",
				Bytes: alGeschreven + geschreven, Files: bestanden,
				FolderIndex: index + 1, FolderTotal: totaal,
			})
		})
		// De lezer sluiten met de fout erin, zodat DownloadCommand stopt met
		// schrijven zodra het uitpakken faalt in plaats van door te pompen.
		_ = pr.CloseWithError(err)
		klaar <- uit{res, err}
	}()

	dlErr := s.ssh.DownloadCommand(ctx, tgt.SSH, cmd, pw, nil)
	_ = pw.CloseWithError(dlErr)

	u := <-klaar
	if u.err != nil {
		return u.res, u.err
	}
	if dlErr != nil {
		return u.res, dlErr
	}
	return u.res, nil
}
