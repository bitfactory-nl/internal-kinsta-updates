package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/rdm/sites-tool/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// selfRepo is de repository waar de tool zelf woont. Bewust een constante en
// geen instelling: dit is de thuisbasis van de app, geen keuze van de
// gebruiker.
const selfRepo = "bitfactory-nl/internal-kinsta-updates"

// errGeenUpdateToken betekent dat er nergens een GitHub-token staat. De repo is
// privé, dus zonder token is zelfs controleren onmogelijk.
var errGeenUpdateToken = errors.New("geen GitHub-token: vul er een in bij Instellingen om updates te kunnen ophalen")

// releaseFetcher is het deel van de GitHub-release-API dat deze service nodig
// heeft; *github.ReleaseClient voldoet eraan.
type releaseFetcher interface {
	LatestRelease(ctx context.Context) (github.Release, error)
	DownloadAsset(ctx context.Context, assetID int64, w io.Writer, onProgress func(done, total int64)) error
}

// UpdateService controleert of er een nieuwere release van de tool zelf is en
// installeert die op verzoek.
type UpdateService struct {
	cfg        *config.Global
	statePath  string
	logDir     string
	bundlePath string // pad van de draaiende .app, leeg buiten een bundle
	current    string // versie van deze build
	newClient  func(token, repo string) releaseFetcher

	initialDelay time.Duration
	interval     time.Duration

	app     *application.App
	emitter eventEmitter

	mu        sync.Mutex
	available *domain.AvailableUpdate
	asset     github.ReleaseAsset
	lastError string
	stop      chan struct{}
	running   bool
}

// NewUpdateService bouwt de service op basis van de draaiende binary: de versie
// komt uit de ldflags-stempel, de bundle uit het pad van het uitvoerbare
// bestand.
func NewUpdateService(cfg *config.Global) *UpdateService {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return &UpdateService{
		cfg:          cfg,
		statePath:    DefaultUpdateStatePath(),
		logDir:       DefaultUpdateLogDir(),
		bundlePath:   bundlePathFor(exe),
		current:      version.Version,
		newClient:    func(token, repo string) releaseFetcher { return github.NewReleaseClient(token, repo) },
		initialDelay: initialUpdateCheckDelay,
		interval:     updateCheckInterval,
	}
}

// SetApp injects the Wails app reference (called after app creation).
func (s *UpdateService) SetApp(app *application.App) {
	s.app = app
	s.emitter = app.Event
}

// DefaultUpdateLogDir is ~/.config/rdm/logs.
func DefaultUpdateLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "rdm", "logs")
	}
	return filepath.Join(home, ".config", "rdm", "logs")
}

// bundlePathFor levert het pad van de .app-bundle waarin exe staat, of "" als
// exe daar niet in zit — een los gebouwd binair bestand uit bin/ bijvoorbeeld.
// Zonder bundle is er niets te vervangen en staat zelf-update uit.
func bundlePathFor(exe string) string {
	if exe == "" {
		return ""
	}
	macos := filepath.Dir(exe)
	if filepath.Base(macos) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(macos)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	app := filepath.Dir(contents)
	if filepath.Ext(app) != ".app" {
		return ""
	}
	return app
}

// enabled meldt of deze build zichzelf mag bijwerken.
func (s *UpdateService) enabled() bool {
	return s.bundlePath != "" && s.current != "" && s.current != "dev"
}

// token levert het GitHub-token: de override uit de updates-sectie, en anders
// dat van de plugin-repo. Keychain-referenties worden hier opgelost.
func (s *UpdateService) token() (string, error) {
	ref := strings.TrimSpace(s.cfg.Updates.GithubToken)
	if ref == "" {
		ref = strings.TrimSpace(s.cfg.PluginRepo.GithubToken)
	}
	if ref == "" {
		return "", errGeenUpdateToken
	}
	token, err := config.ResolveSecret(ref)
	if err != nil {
		return "", fmt.Errorf("GitHub-token uit de keychain lezen: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return "", errGeenUpdateToken
	}
	return token, nil
}

// Status is wat de frontend nodig heeft voor de badge, de popup en de sectie in
// Instellingen.
func (s *UpdateService) Status() domain.UpdateStatus {
	st, _ := loadUpdateState(s.statePath)

	s.mu.Lock()
	defer s.mu.Unlock()
	return domain.UpdateStatus{
		CurrentVersion: s.current,
		Enabled:        s.enabled(),
		AutoCheck:      s.cfg.Updates.AutoCheckEnabled(),
		LastCheck:      st.LastCheck,
		LastError:      s.lastError,
		Available:      s.available,
	}
}

// Check haalt de laatste release op en vergelijkt die met de draaiende versie.
// Is er een nieuwere die niet is overgeslagen, dan gaat er een
// "updates:available"-event naar de frontend. Een mislukte check levert een
// fout op en zet LastError, maar stuurt geen event: een popup over een
// netwerkfout onderbreekt het werk zonder dat er iets te kiezen valt.
func (s *UpdateService) Check() (domain.UpdateStatus, error) {
	if !s.enabled() {
		return s.Status(), nil
	}

	token, err := s.token()
	if err != nil {
		s.setError(err)
		return s.Status(), err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rel, err := s.newClient(token, selfRepo).LatestRelease(ctx)
	if err != nil {
		s.setError(err)
		return s.Status(), err
	}

	st, _ := loadUpdateState(s.statePath)
	st.LastCheck = time.Now()
	saveErr := saveUpdateState(s.statePath, st)
	if saveErr != nil {
		// Niet fataal voor de check zelf, maar wel zichtbaar in Instellingen.
		s.setError(fmt.Errorf("update-state opslaan: %w", saveErr))
	}

	if !version.IsNewer(rel.TagName, s.current) {
		s.mu.Lock()
		s.available = nil
		if saveErr == nil {
			s.lastError = ""
		}
		s.mu.Unlock()
		return s.Status(), nil
	}

	changes := parseChangelog(rel.Body)
	if changes == nil {
		// Een lege lijst en geen null naar de frontend, die erover itereert.
		changes = []domain.ChangeEntry{}
	}
	upd := &domain.AvailableUpdate{
		Version:   rel.TagName,
		Changes:   changes,
		Skipped:   st.SkippedVersion == rel.TagName,
		SizeBytes: rel.Asset.Size,
	}

	s.mu.Lock()
	s.available = upd
	s.asset = rel.Asset
	if saveErr == nil {
		s.lastError = ""
	}
	emitter := s.emitter
	s.mu.Unlock()

	if !upd.Skipped && emitter != nil {
		emitter.Emit("updates:available", upd)
	}
	return s.Status(), nil
}

// Skip legt vast dat de gebruiker deze versie heeft weggeklikt: de popup komt
// niet terug, de badge in de rail blijft staan.
func (s *UpdateService) Skip(v string) error {
	st, err := loadUpdateState(s.statePath)
	if err != nil {
		return err
	}
	st.SkippedVersion = v
	if err := saveUpdateState(s.statePath, st); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.available != nil && s.available.Version == v {
		kopie := *s.available
		kopie.Skipped = true
		s.available = &kopie
	}
	return nil
}

// WhatsNew levert de changelog van de update die net is geïnstalleerd, en
// alleen bij de eerste start daarna. De vergelijking gaat via de state: wijkt
// LastRunVersion af van de draaiende versie en hoort de bewaarde changelog bij
// die versie, dan is dit de eerste start na een update. Daarna wordt
// LastRunVersion bijgewerkt, zodat het venster één keer verschijnt.
func (s *UpdateService) WhatsNew() *domain.AvailableUpdate {
	st, err := loadUpdateState(s.statePath)
	if err != nil {
		return nil
	}
	if st.LastRunVersion == s.current {
		return nil
	}

	var uit *domain.AvailableUpdate
	if st.LastRunVersion != "" && st.InstalledVersion == s.current {
		uit = &domain.AvailableUpdate{Version: s.current, Changes: st.InstalledChanges}
	}

	st.LastRunVersion = s.current
	_ = saveUpdateState(s.statePath, st)
	return uit
}

// InstallLog geeft de inhoud van het laatste update-logbestand, voor de link in
// Instellingen en het "wat is er nieuw"-venster.
func (s *UpdateService) InstallLog() (string, error) {
	st, err := loadUpdateState(s.statePath)
	if err != nil {
		return "", err
	}
	if st.InstallLog == "" {
		return "", nil
	}
	data, err := os.ReadFile(st.InstallLog)
	if err != nil {
		return "", fmt.Errorf("update-log lezen: %w", err)
	}
	return string(data), nil
}

// setError bewaart een foutmelding voor de sectie in Instellingen.
func (s *UpdateService) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err.Error()
}

// emitProgress stuurt een voortgangsstap naar de frontend.
func (s *UpdateService) emitProgress(p domain.UpdateProgress) {
	s.mu.Lock()
	emitter := s.emitter
	s.mu.Unlock()
	if emitter != nil {
		emitter.Emit("updates:progress", p)
	}
}
