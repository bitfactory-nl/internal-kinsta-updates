package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Run executes a git command in dir and returns trimmed stdout, or an error
// that includes stderr for diagnostics.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
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
