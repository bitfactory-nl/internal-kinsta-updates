package wpfleet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/docker"
	"github.com/rdm/sites-tool/internal/domain"
)

var (
	ErrDockerDown   = errors.New("Docker draait niet — start Docker Desktop")
	ErrImageMissing = errors.New("wp-fleet-image ontbreekt")
	ErrNoSites      = errors.New("geen WordPress-sites met Application Password — voeg er een toe in de tool")
)

// Test seams.
var (
	execCommandContext = exec.CommandContext
	dockerBin          = docker.DockerBin
)

type SecretFunc func(id string) (string, error)

type LaunchConfig struct {
	Sites    []domain.WPSite
	Image    string
	ReadOnly bool
	Secret   SecretFunc
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	BaseEnv  []string // meestal os.Environ()
}

// Launch start wp-fleet-mcp in docker en geeft de exitcode van docker terug.
// Stdout is het MCP-kanaal: Launch schrijft zelf alleen naar Stderr.
func Launch(ctx context.Context, c LaunchConfig) (int, error) {
	sites, env := sitesMetSecret(c)
	if len(sites) == 0 {
		// wp-fleet-mcp eist minstens één site in sites.json.
		return 1, ErrNoSites
	}

	if err := checkDocker(ctx, c.Image); err != nil {
		return 1, err
	}

	dir, err := os.MkdirTemp("", "wp-fleet-*")
	if err != nil {
		return 1, fmt.Errorf("tijdelijke map: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return 1, fmt.Errorf("tijdelijke map: %w", err)
	}

	data, err := BuildSitesJSON(sites)
	if err != nil {
		return 1, err
	}
	sitesPath := filepath.Join(dir, "sites.json")
	if err := os.WriteFile(sitesPath, data, 0o600); err != nil {
		return 1, fmt.Errorf("sites.json: %w", err)
	}

	names := make([]string, 0, len(sites))
	for _, s := range sites {
		names = append(names, EnvName(s.ID))
	}
	cmd := execCommandContext(ctx, dockerBin(), DockerArgs(RunOptions{
		Image: c.Image, ReadOnly: c.ReadOnly, SitesJSONPath: sitesPath, EnvNames: names,
	})...)
	cmd.Env = append(append([]string{}, c.BaseEnv...), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("docker starten: %w", err)
	}
	stop := forwardSignals(cmd)
	defer stop()

	err = cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return 1, fmt.Errorf("docker: %w", err)
	}
	return 0, nil
}

// sitesMetSecret houdt alleen sites met een wachtwoord over en geeft de
// bijbehorende NAAM=waarde-regels voor de procesomgeving terug. De fout van
// Secret komt bewust niet in de melding: die kan uitvoer van `security` bevatten.
func sitesMetSecret(c LaunchConfig) ([]domain.WPSite, []string) {
	var sites []domain.WPSite
	var env []string
	for _, s := range c.Sites {
		pw, err := c.Secret(s.ID)
		if err != nil || pw == "" {
			fmt.Fprintf(c.Stderr, "wp-fleet: site %s overgeslagen — geen Application Password in de Keychain\n", s.ID)
			continue
		}
		sites = append(sites, s)
		env = append(env, EnvName(s.ID)+"="+pw)
	}
	return sites, env
}

func checkDocker(ctx context.Context, image string) error {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := execCommandContext(cctx, dockerBin(), "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		return ErrDockerDown
	}
	if err := execCommandContext(cctx, dockerBin(), "image", "inspect", image).Run(); err != nil {
		return fmt.Errorf("%w: %s — bouw het met `docker build -t %s .` in de wp-fleet-repo", ErrImageMissing, image, image)
	}
	return nil
}

// forwardSignals stuurt SIGINT/SIGTERM door naar docker, zodat Claude Code de
// container netjes kan stoppen.
func forwardSignals(cmd *exec.Cmd) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}
