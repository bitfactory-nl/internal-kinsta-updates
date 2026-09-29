package services

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/claudecode"
	"github.com/rdm/sites-tool/internal/config"
)

const wpFleetServerName = "wp-fleet"

type ClaudeRunner func(ctx context.Context, args ...string) (string, error)

// DefaultClaudeRunner draait de Claude Code CLI; Bin() vindt hem ook als de app
// vanuit Finder is gestart.
func DefaultClaudeRunner(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, claudecode.Bin(), args...).CombinedOutput()
	return string(out), err
}

type FleetStatus struct {
	Registered           bool   `json:"registered"`
	PointsToThisApp      bool   `json:"pointsToThisApp"`
	StableLocation       bool   `json:"stableLocation"`
	Executable           string `json:"executable"`
	Image                string `json:"image"`
	ReadOnly             bool   `json:"readOnly"`
	Sites                int    `json:"sites"`
	SitesWithoutPassword int    `json:"sitesWithoutPassword"`
}

type WPFleetService struct {
	cfg        *config.Global
	sites      *WPSiteService
	run        ClaudeRunner
	executable func() (string, error)
}

func NewWPFleetService(cfg *config.Global, sites *WPSiteService, run ClaudeRunner, executable func() (string, error)) *WPFleetService {
	return &WPFleetService{cfg: cfg, sites: sites, run: run, executable: executable}
}

func (s *WPFleetService) Status() (FleetStatus, error) {
	exe, err := s.executable()
	if err != nil {
		return FleetStatus{}, fmt.Errorf("pad van de app: %w", err)
	}
	total, zonder, err := s.sites.Stats()
	if err != nil {
		return FleetStatus{}, err
	}
	st := FleetStatus{
		Executable:           exe,
		StableLocation:       strings.HasPrefix(exe, "/Applications/"),
		Image:                s.cfg.WPFleet.Image,
		ReadOnly:             s.cfg.WPFleet.ReadOnly,
		Sites:                total,
		SitesWithoutPassword: zonder,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := s.run(ctx, "mcp", "get", wpFleetServerName); err == nil {
		st.Registered = true
		st.PointsToThisApp = strings.Contains(out, exe) && strings.Contains(out, "mcp-launch")
	}
	return st, nil
}

// Register vervangt een bestaande wp-fleet-registratie (bijv. de oude
// docker run met --env-file) door deze app als launcher.
func (s *WPFleetService) Register() (FleetStatus, error) {
	exe, err := s.executable()
	if err != nil {
		return FleetStatus{}, fmt.Errorf("pad van de app: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Bestaat hij niet, dan faalt remove; dat is prima.
	_, _ = s.run(ctx, "mcp", "remove", wpFleetServerName, "-s", "user")
	args := []string{"mcp", "add", wpFleetServerName, "-s", "user", "--", exe, "mcp-launch"}
	if out, err := s.run(ctx, args...); err != nil {
		return FleetStatus{}, fmt.Errorf("registreren mislukt (%v %s) — voer zelf uit: claude %s",
			err, strings.TrimSpace(out), strings.Join(args, " "))
	}
	return s.Status()
}

func (s *WPFleetService) Unregister() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := s.run(ctx, "mcp", "remove", wpFleetServerName, "-s", "user"); err != nil {
		return fmt.Errorf("registratie verwijderen mislukt: %v %s", err, strings.TrimSpace(out))
	}
	return nil
}

func (s *WPFleetService) SaveSettings(image string, readOnly bool) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return errors.New("image-referentie is verplicht")
	}
	next := *s.cfg
	next.WPFleet = config.WPFleetGlobal{Image: image, ReadOnly: readOnly}
	if err := config.SaveGlobal(next); err != nil {
		return err
	}
	*s.cfg = next
	return nil
}
