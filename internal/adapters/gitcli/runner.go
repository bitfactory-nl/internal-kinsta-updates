package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// newGitCmd bouwt een git-commando met veilige defaults:
//   - de globale --no-optional-locks vlag, zodat read-commando's (status e.d.)
//     geen index.lock nemen voor de optionele stat-cache-refresh;
//   - nette afbraak bij context-cancel/-timeout: eerst SIGTERM met een korte
//     gratieperiode en pas daarna SIGKILL, zodat git zijn lockbestanden kan
//     opruimen. Zonder dit liet een verlopen timeout verweesde
//     .git/index.lock-bestanden achter in gescande projectrepo's.
func newGitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks"}, args...)...)
	cmd.Dir = dir
	// Eigen procesgroep: git spawnt kind-processen (git-remote-http, ssh,
	// hook-scripts). Het cancel-signaal moet de hele boom bereiken, anders
	// blijven die na een timeout als wezen doordraaien.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// Run executes a git command in dir and returns trimmed stdout, or an error
// that includes stderr for diagnostics.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := newGitCmd(ctx, dir, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunLines executes a git command and returns stdout split into non-empty lines.
func RunLines(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	raw := strings.Split(out, "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}
