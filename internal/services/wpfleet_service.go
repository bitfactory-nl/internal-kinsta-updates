package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// claudeConfigPath wijst naar ~/.claude.json, waar Claude Code de
	// user-scope MCP-servers bewaart.
	claudeConfigPath func() string
}

func NewWPFleetService(cfg *config.Global, sites *WPSiteService, run ClaudeRunner, executable func() (string, error)) *WPFleetService {
	return &WPFleetService{cfg: cfg, sites: sites, run: run, executable: executable,
		claudeConfigPath: defaultClaudeConfigPath}
}

func defaultClaudeConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

// readWPFleetRegistration leest alleen command en args van de wp-fleet-server
// uit ~/.claude.json. Het bestand bevat ook tokens van andere servers, dus de
// struct decodeert bewust niets anders. Ontbreekt het bestand, dan is er geen
// registratie; onleesbare JSON is een fout.
func readWPFleetRegistration(path string) (command string, args []string, found bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("lees Claude Code-configuratie: %w", err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Niet %w: de fout van encoding/json kan een stukje van de inhoud bevatten.
		return "", nil, false, errors.New("Claude Code-configuratie (~/.claude.json) is geen geldige JSON")
	}
	srv, ok := cfg.MCPServers[wpFleetServerName]
	if !ok {
		return "", nil, false, nil
	}
	return srv.Command, srv.Args, true, nil
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
		StableLocation:       stableLocation(exe),
		Image:                s.cfg.WPFleet.Image,
		ReadOnly:             s.cfg.WPFleet.ReadOnly,
		Sites:                total,
		SitesWithoutPassword: zonder,
	}
	// Niet via `claude mcp get`: dat health-checkt de server en start zo
	// mcp-launch (Keychain + docker) bij elke statusvraag.
	command, args, found, err := readWPFleetRegistration(s.claudeConfigPath())
	if err != nil {
		return FleetStatus{}, err
	}
	st.Registered = found
	st.PointsToThisApp = found && command == exe && len(args) == 1 && args[0] == "mcp-launch"
	return st, nil
}

// stableLocation is waar: de app staat in /Applications of ~/Applications, en
// verhuist dus niet bij een nieuwe build of een opgeruimde Downloads-map.
func stableLocation(exe string) bool {
	if strings.HasPrefix(exe, "/Applications/") {
		return true
	}
	home, err := os.UserHomeDir()
	return err == nil && home != "" && strings.HasPrefix(exe, filepath.Join(home, "Applications")+"/")
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
		handmatig := "claude mcp add " + wpFleetServerName + " -s user -- " + strconv.Quote(exe) + " mcp-launch"
		return FleetStatus{}, fmt.Errorf("registreren mislukt (%v %s) — voer zelf uit: %s",
			err, strings.TrimSpace(out), handmatig)
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
