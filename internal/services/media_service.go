package services

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/browser"
	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

//go:embed media_scan.php
var mediaScanScript string

// mediaScanTimeout bounds the whole SSH round trip. The analyzer keeps a shorter
// budget of its own (mediaPHPBudget), so this is a backstop rather than the normal
// end of a slow scan.
const mediaScanTimeout = 25 * time.Minute

// mediaProbeTimeout bounds the much cheaper connection check.
const mediaProbeTimeout = 45 * time.Second

// kinstaSSHSource resolves an environment's SSH endpoint (test seam).
type kinstaSSHSource interface {
	EnvironmentSSH(projectID, envID string) (EnvSSHEndpoint, error)
}

// secretStore keeps SSH passwords out of config files (test seam). Set takes an
// account name, Get takes the reference stored in .rdm/config.yml.
type secretStore interface {
	Set(account, secret string) error
	Get(ref string) (string, error)
}

// keychainSecrets is the production secretStore: the macOS keychain.
type keychainSecrets struct{}

func (keychainSecrets) Set(account, secret string) error {
	return config.KeychainSet(account, secret)
}

func (keychainSecrets) Get(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	return config.ResolveSecret(ref)
}

// MediaService analyses wp-content/uploads on a Kinsta environment: what is there,
// how big it is, and which media have no reference left. It is deliberately
// read-only — there is no method here that changes anything on the server.
type MediaService struct {
	projects *ProjectService
	kinsta   kinstaSSHSource
	store    *MediaScanStore
	ssh      sshRunner
	secrets  secretStore
	crawler  siteCrawler
	now      func() time.Time

	bezigMu sync.Mutex
	bezig   map[string]bool

	crawlMu    sync.Mutex
	crawlCache map[string]map[string]bool
}

func NewMediaService(projects *ProjectService, kinsta *KinstaService, store *MediaScanStore) *MediaService {
	return &MediaService{
		projects: projects,
		kinsta:   kinsta,
		store:    store,
		ssh:      sshadapter.NewClient(),
		secrets:  keychainSecrets{},
		crawler:  browser.NewCrawler(CrawlScriptPath()),
		now:      time.Now,
		bezig:    map[string]bool{},
	}
}

// claim reserves one environment for a scan; a scan is heavy on a customer's
// container, so two at once is never wanted.
func (s *MediaService) claim(slot string) bool {
	s.bezigMu.Lock()
	defer s.bezigMu.Unlock()
	if s.bezig[slot] {
		return false
	}
	s.bezig[slot] = true
	return true
}

func (s *MediaService) release(slot string) {
	s.bezigMu.Lock()
	delete(s.bezig, slot)
	s.bezigMu.Unlock()
}

func (s *MediaService) project(id string) (domain.Project, error) {
	p, ok := s.projects.Get(id)
	if !ok {
		return domain.Project{}, fmt.Errorf("project %q niet gevonden", id)
	}
	return p, nil
}

func (s *MediaService) target(projectID, envID string) (mediaTarget, domain.Project, error) {
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

// ProbeEnvironment checks whether a scan can work at all: does the key-based login
// succeed, which user are we, where is the site, does WP-CLI answer, and how big is
// uploads. Cheap enough to run before anything else.
func (s *MediaService) ProbeEnvironment(projectID, envID string) (MediaProbe, error) {
	tgt, _, err := s.target(projectID, envID)
	if err != nil {
		return MediaProbe{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), mediaProbeTimeout)
	defer cancel()

	out, err := s.ssh.RunCommand(ctx, tgt.SSH, buildMediaProbeCommand(tgt.Webroot))
	probe := mediaProbeUit(out)
	if err != nil && probe.User == "" {
		return MediaProbe{}, fmt.Errorf("verbinding met %s: %w", tgt.SSH.Host, err)
	}
	if probe.Webroot == "" {
		return probe, fmt.Errorf("geen wp-config.php gevonden onder %s; vul het pad handmatig in", probe.Home)
	}
	return probe, nil
}

// ScanEnvironment runs the analyzer on one environment and stores the result. When
// folders is non-empty the scan is limited to those prefixes inside uploads, which
// keeps both the file walk and the library index small — the way to look at one
// year without waiting for a whole media library.
func (s *MediaService) ScanEnvironment(projectID, envID string, folders []string) (domain.MediaScanSummary, error) {
	tgt, p, err := s.target(projectID, envID)
	if err != nil {
		return domain.MediaScanSummary{}, err
	}

	slot := projectID + "@" + envID
	if !s.claim(slot) {
		return domain.MediaScanSummary{}, fmt.Errorf("er loopt al een mediascan voor deze omgeving")
	}
	defer s.release(slot)

	ctx, cancel := context.WithTimeout(context.Background(), mediaScanTimeout)
	defer cancel()

	start := s.now()
	out, runErr := s.ssh.RunCommand(ctx, tgt.SSH, buildMediaScanCommand(tgt.Webroot, mediaScanScript, folders))

	// Eerst parsen, ook na een fout: RunCommand geeft de stdout mee terug en een
	// gedeeltelijk resultaat zegt meer dan alleen een exitcode.
	payload, perr := parseMediaScanOutput(out)
	if perr != nil {
		if melding := eersteGroep(reMediaErr, out); melding != "" {
			return domain.MediaScanSummary{}, fmt.Errorf("scan op de server: %s", melding)
		}
		if runErr != nil {
			return domain.MediaScanSummary{}, fmt.Errorf("mediascan op %s: %w", tgt.SSH.Host, runErr)
		}
		return domain.MediaScanSummary{}, perr
	}

	sum, detail := payload.summary(
		start.Format("20060102-150405"),
		projectID, p.DisplayName, tgt.EnvName,
		start, duBytesUit(out),
	)
	sum.DurationMS = s.now().Sub(start).Milliseconds()
	if runErr != nil {
		// Een niet-nul exitcode met een geldig resultaat komt voor: PHP-notices op
		// een klantsite zijn geen reden om de uitkomst weg te gooien.
		sum.Scope.Notes = append(sum.Scope.Notes, "de scan eindigde met een foutcode: "+runErr.Error())
	}

	if err := s.store.Save(sum, detail); err != nil {
		return sum, err
	}
	s.onthoudWebroot(p, eersteGroep(reMediaRoot, out))
	return sum, nil
}

// onthoudWebroot stores a freshly discovered webroot in .rdm/config.yml so the next scan
// does not have to search for it. Failing to save is not worth aborting a scan
// that already succeeded.
func (s *MediaService) onthoudWebroot(p domain.Project, root string) {
	root = strings.TrimSpace(root)
	if root == "" || p.Config.SSH == nil || p.Config.SSH.Path == root {
		return
	}
	cfg := p.Config
	ssh := *cfg.SSH
	ssh.Path = root
	cfg.SSH = &ssh
	if err := config.SaveProject(p.Path, cfg); err == nil {
		s.projects.UpdateProjectConfig(p.ID, cfg)
	}
}

// SSHAccess is what a project knows about reaching its own server. Kinsta's API
// supplies none of this, so it is entered once and then remembered. The password
// itself never leaves the keychain — only whether there is one.
type SSHAccess struct {
	User        string `json:"user"`
	Path        string `json:"path"`
	HasPassword bool   `json:"hasPassword"`
}

// GetSSHAccess returns the stored SSH username, webroot and whether a password is
// on file.
func (s *MediaService) GetSSHAccess(projectID string) (SSHAccess, error) {
	p, err := s.project(projectID)
	if err != nil {
		return SSHAccess{}, err
	}
	if p.Config.SSH == nil {
		return SSHAccess{}, nil
	}
	return SSHAccess{
		User:        p.Config.SSH.User,
		Path:        p.Config.SSH.Path,
		HasPassword: p.Config.SSH.Password != "",
	}, nil
}

// SaveSSHAccess stores the SSH username and optional webroot in .rdm/config.yml. An empty
// path means: find it on the server during the next scan. A non-empty password goes
// into the macOS keychain and only a reference to it is written to .rdm/config.yml —
// that file is committed in the customer's repo, so the secret can never live
// there. An empty password leaves any stored one untouched.
func (s *MediaService) SaveSSHAccess(projectID, user, path, password string) error {
	p, err := s.project(projectID)
	if err != nil {
		return err
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return fmt.Errorf("SSH-gebruiker mag niet leeg zijn")
	}
	cfg := p.Config
	ssh := domain.SSHTarget{}
	if cfg.SSH != nil {
		ssh = *cfg.SSH
	}
	ssh.User = user
	ssh.Path = strings.TrimSpace(path)

	if password != "" {
		account := "ssh:" + p.DisplayName
		if err := s.secrets.Set(account, password); err != nil {
			return fmt.Errorf("wachtwoord in de keychain zetten: %w", err)
		}
		ssh.Password = config.KeychainPrefix + account
	}

	cfg.SSH = &ssh
	if err := config.SaveProject(p.Path, cfg); err != nil {
		return fmt.Errorf("opslaan .rdm/config.yml: %w", err)
	}
	s.projects.UpdateProjectConfig(projectID, cfg)
	return nil
}

// LatestScan returns the newest stored scan, or nil when the project was never
// scanned.
func (s *MediaService) LatestScan(projectID string) (*domain.MediaScanSummary, error) {
	return s.store.Latest(projectID)
}

// ListScans returns the stored scans, newest first, so growth over time is visible.
func (s *MediaService) ListScans(projectID string) ([]domain.MediaScanSummary, error) {
	return s.store.List(projectID)
}

// ScanDetail returns a window of the stored rows, narrowed by category and/or folder.
// Both filters matter for paging: the detail file holds every category and every
// folder in one stream, so an offset only means something once the filter is applied.
// An empty category or prefix means "no restriction".
func (s *MediaService) ScanDetail(projectID, scanID string, category domain.MediaCategory, prefix string, offset, limit int) ([]domain.MediaFileRow, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rijen, err := s.store.Detail(projectID, scanID, category, prefix, offset, limit)
	if err != nil {
		return nil, err
	}
	// Is er een crawl gedraaid, dan telt "de browser heeft dit opgevraagd" als bewijs.
	// Dat hoort bij de rij te staan, niet in een apart hoekje: het is de sterkste
	// aanwijzing dat een bestand ondanks de databasescan in gebruik is.
	gezien := s.crawlPaden(projectID, scanID)
	if len(gezien) == 0 {
		return rijen, nil
	}
	for i := range rijen {
		if gezien[rijen[i].Path] {
			rijen[i].Evidence = append(rijen[i].Evidence, domain.EvidenceRendered)
		}
	}
	return rijen, nil
}
