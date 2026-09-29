package app

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rdm/sites-tool/internal/adapters/wpfleet"
	"github.com/rdm/sites-tool/internal/config"
)

// RunMCPLaunch is het subcommando dat Claude Code start als wp-fleet-server.
// Stdout is het MCP-kanaal; alles wat de tool zelf meldt gaat naar stderr.
// Bewust niet via LoadConfig: de Keychain-migratie daar kan om toestemming
// vragen, en dat hoort niet in een proces zonder venster.
func RunMCPLaunch(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	g, err := config.LoadGlobal()
	if err != nil {
		fmt.Fprintf(stderr, "wp-fleet: %v\n", err)
		return 1
	}
	sites, err := config.LoadWPSites(config.WPSitesPath())
	if err != nil {
		fmt.Fprintf(stderr, "wp-fleet: %v\n", err)
		return 1
	}
	code, err := wpfleet.Launch(ctx, wpfleet.LaunchConfig{
		Sites:    sites,
		Image:    g.WPFleet.Image,
		ReadOnly: g.WPFleet.ReadOnly,
		Secret: func(id string) (string, error) {
			return config.ResolveSecret(config.KeychainPrefix + config.WPSecretAccount(id))
		},
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		BaseEnv: os.Environ(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "wp-fleet: %v\n", err)
	}
	return code
}
