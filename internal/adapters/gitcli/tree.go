package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// RefExists reports whether ref resolves to a commit in the repository.
func RefExists(ctx context.Context, dir, ref string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// LsTreeDirs lists the names of directories directly under treePath at ref.
// A treePath that does not exist on the ref yields an empty slice.
func LsTreeDirs(ctx context.Context, dir, ref, treePath string) ([]string, error) {
	lines, err := RunLines(ctx, dir, "ls-tree", "-d", "--name-only", ref, treePath+"/")
	if err != nil {
		// The path not existing on this ref is a normal situation, not an error.
		if strings.Contains(err.Error(), "Not a valid object name") ||
			strings.Contains(err.Error(), "unknown revision") ||
			strings.Contains(err.Error(), "exists on disk, but not in") {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, path.Base(l))
	}
	return out, nil
}

// ShowFile returns the contents of path at ref (git show ref:path).
func ShowFile(ctx context.Context, dir, ref, filePath string) ([]byte, error) {
	out, err := Run(ctx, dir, "show", ref+":"+filePath)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// GrepTree runs git grep -E for pattern at ref, limited to pathspecs, and
// returns matched lines as "path:matched-line" (the ref prefix is stripped).
// No matches is not an error.
func GrepTree(ctx context.Context, dir, ref, pattern string, pathspecs ...string) ([]string, error) {
	args := []string{"grep", "-I", "--no-color", "-E", pattern, ref, "--"}
	args = append(args, pathspecs...)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil // no matches
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git grep: %s", msg)
	}

	prefix := ref + ":"
	var out []string
	for _, l := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if l == "" {
			continue
		}
		out = append(out, strings.TrimPrefix(l, prefix))
	}
	return out, nil
}
