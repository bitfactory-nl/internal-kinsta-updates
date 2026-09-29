package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/config"
)

type nepClaude struct {
	calls  [][]string
	addErr error
}

func (n *nepClaude) run(_ context.Context, args ...string) (string, error) {
	n.calls = append(n.calls, args)
	if len(args) >= 2 && args[1] == "add" {
		return "", n.addErr
	}
	return "", nil
}

// schrijfClaudeConfig zet een tijdelijke ~/.claude.json neer; de echte van de
// gebruiker wordt nooit gelezen omdat HOME in nieuweFleet een tempdir is.
func schrijfClaudeConfig(t *testing.T, inhoud string) {
	t.Helper()
	home, _ := os.UserHomeDir()
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(inhoud), 0o600); err != nil {
		t.Fatal(err)
	}
}

func nieuweFleet(t *testing.T, exe string) (*WPFleetService, *nepClaude, *config.Global) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	sites, _, _ := nieuweWPSiteService(t)
	cfg := &config.Global{WPFleet: config.WPFleetGlobal{Image: config.DefaultWPFleetImage}}
	nc := &nepClaude{}
	svc := NewWPFleetService(cfg, sites, nc.run, func() (string, error) { return exe, nil })
	return svc, nc, cfg
}

const appExe = "/Applications/rdm-sites-tool.app/Contents/MacOS/rdm-sites-tool"

func TestFleetRegisterVoertRemoveEnAddUit(t *testing.T) {
	svc, nc, _ := nieuweFleet(t, appExe)
	schrijfClaudeConfig(t, `{"mcpServers":{"wp-fleet":{"type":"stdio","command":"`+appExe+`","args":["mcp-launch"]}}}`)
	st, err := svc.Register()
	if err != nil {
		t.Fatal(err)
	}
	var add []string
	for _, c := range nc.calls {
		if c[1] == "add" {
			add = c
		}
	}
	want := []string{"mcp", "add", "wp-fleet", "-s", "user", "--", appExe, "mcp-launch"}
	if strings.Join(add, " ") != strings.Join(want, " ") {
		t.Fatalf("add = %v, want %v", add, want)
	}
	if nc.calls[0][1] != "remove" {
		t.Fatalf("eerste aanroep moet remove zijn: %v", nc.calls)
	}
	if !st.StableLocation || !st.Registered || !st.PointsToThisApp {
		t.Fatalf("status na registratie: %+v", st)
	}
}

func TestFleetRegisterClaudeOntbreekt(t *testing.T) {
	svc, nc, _ := nieuweFleet(t, appExe)
	nc.addErr = errors.New("executable file not found")
	_, err := svc.Register()
	if err == nil || !strings.Contains(err.Error(), "claude mcp add wp-fleet -s user -- "+appExe+" mcp-launch") {
		t.Fatalf("fout moet het handmatige commando noemen: %v", err)
	}
}

func TestFleetStatus(t *testing.T) {
	svc, nc, _ := nieuweFleet(t, "/Users/x/Projects/RDM/bin/rdm-sites-tool.dev.app/Contents/MacOS/rdm-sites-tool")
	st, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Registered || st.StableLocation {
		t.Fatalf("zonder ~/.claude.json: %+v", st)
	}
	schrijfClaudeConfig(t, `{"oauthAccount":{"token":"x"},"mcpServers":{"wp-fleet":{"command":"docker","args":["run","-i","--rm","--env-file","/x/.env","wp-fleet-mcp"]}}}`)
	st, err = svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Registered || st.PointsToThisApp {
		t.Fatalf("oude registratie hoort niet naar deze app te wijzen: %+v", st)
	}
	if len(nc.calls) != 0 {
		t.Fatalf("Status mag de claude-CLI niet aanroepen (health-check): %v", nc.calls)
	}
}

func TestFleetStatusWijstNaarDezeApp(t *testing.T) {
	svc, _, _ := nieuweFleet(t, appExe)
	schrijfClaudeConfig(t, `{"mcpServers":{"ander":{"command":"x"},"wp-fleet":{"command":"`+appExe+`","args":["mcp-launch"]}}}`)
	st, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Registered || !st.PointsToThisApp {
		t.Fatalf("status %+v", st)
	}
}

func TestFleetStatusZonderWPFleet(t *testing.T) {
	svc, _, _ := nieuweFleet(t, appExe)
	schrijfClaudeConfig(t, `{"mcpServers":{"ander":{"command":"x"}}}`)
	st, err := svc.Status()
	if err != nil || st.Registered {
		t.Fatalf("status %+v, err %v", st, err)
	}
}

func TestFleetStatusOngeldigeJSON(t *testing.T) {
	svc, _, _ := nieuweFleet(t, appExe)
	schrijfClaudeConfig(t, `{"mcpServers":`)
	if _, err := svc.Status(); err == nil {
		t.Fatal("verwacht fout bij ongeldige ~/.claude.json")
	}
}

func TestFleetSaveSettings(t *testing.T) {
	svc, _, cfg := nieuweFleet(t, appExe)
	if err := svc.SaveSettings("ghcr.io/org/wp-fleet:1.2.0", true); err != nil {
		t.Fatal(err)
	}
	if cfg.WPFleet.Image != "ghcr.io/org/wp-fleet:1.2.0" || !cfg.WPFleet.ReadOnly {
		t.Fatalf("cfg %+v", cfg.WPFleet)
	}
	g, _ := config.LoadGlobal()
	if g.WPFleet.Image != "ghcr.io/org/wp-fleet:1.2.0" {
		t.Fatalf("niet opgeslagen: %+v", g.WPFleet)
	}
	if err := svc.SaveSettings("  ", false); err == nil {
		t.Fatal("lege image moet geweigerd worden")
	}
}
