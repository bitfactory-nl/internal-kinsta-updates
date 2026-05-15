package gitcli

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func ctxDefault() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func ctxLong() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// Fetch runs git fetch --all --prune.
func Fetch(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "fetch", "--all", "--prune")
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}

// Pull runs git pull.
func Pull(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "pull")
	if err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	return nil
}

// Push runs git push, optionally with --force-with-lease.
func Push(ctx context.Context, dir, remote, branch string, force bool) error {
	args := []string{"push", remote, branch}
	if force {
		args = append(args, "--force-with-lease")
	}
	_, err := Run(ctx, dir, args...)
	if err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// StageFiles runs git add -- <paths>.
func StageFiles(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, paths...)
	_, err := Run(ctx, dir, args...)
	if err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	return nil
}

// StageAll runs git add -A.
func StageAll(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "add", "-A")
	if err != nil {
		return fmt.Errorf("git add -A: %w", err)
	}
	return nil
}

// UnstageFiles runs git restore --staged -- <paths>.
func UnstageFiles(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"restore", "--staged", "--"}, paths...)
	_, err := Run(ctx, dir, args...)
	if err != nil {
		return fmt.Errorf("git restore --staged: %w", err)
	}
	return nil
}

// DiscardFile runs git restore -- <path> to discard working-tree changes for a tracked file.
func DiscardFile(ctx context.Context, dir, path string) error {
	_, err := Run(ctx, dir, "restore", "--", path)
	if err != nil {
		return fmt.Errorf("git restore %s: %w", path, err)
	}
	return nil
}

// Commit creates a commit with the given message, optionally amending the previous one.
func Commit(ctx context.Context, dir, message string, amend bool) error {
	args := []string{"commit", "-m", message}
	if amend {
		args = append(args, "--amend")
	}
	_, err := Run(ctx, dir, args...)
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// CheckoutBranch runs git checkout <name>.
func CheckoutBranch(ctx context.Context, dir, name string) error {
	_, err := Run(ctx, dir, "checkout", name)
	if err != nil {
		return fmt.Errorf("git checkout %s: %w", name, err)
	}
	return nil
}

// CreateBranch runs git checkout -b <name> [from].
func CreateBranch(ctx context.Context, dir, name, from string) error {
	args := []string{"checkout", "-b", name}
	if from != "" {
		args = append(args, from)
	}
	_, err := Run(ctx, dir, args...)
	if err != nil {
		return fmt.Errorf("git checkout -b %s: %w", name, err)
	}
	return nil
}

// DeleteBranch deletes a branch; force=true uses -D, otherwise -d.
func DeleteBranch(ctx context.Context, dir, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := Run(ctx, dir, "branch", flag, name)
	if err != nil {
		return fmt.Errorf("git branch %s %s: %w", flag, name, err)
	}
	return nil
}

// MergeBranch runs git merge <name>.
func MergeBranch(ctx context.Context, dir, name string) error {
	_, err := Run(ctx, dir, "merge", name)
	if err != nil {
		return fmt.Errorf("git merge %s: %w", name, err)
	}
	return nil
}

// StashSave runs git stash push -m <message>, or git stash if message is empty.
func StashSave(ctx context.Context, dir, message string) error {
	var args []string
	if strings.TrimSpace(message) == "" {
		args = []string{"stash"}
	} else {
		args = []string{"stash", "push", "-m", message}
	}
	_, err := Run(ctx, dir, args...)
	if err != nil {
		return fmt.Errorf("git stash save: %w", err)
	}
	return nil
}

// StashPop applies and removes stash@{index}.
func StashPop(ctx context.Context, dir string, index int) error {
	ref := fmt.Sprintf("stash@{%d}", index)
	_, err := Run(ctx, dir, "stash", "pop", ref)
	if err != nil {
		return fmt.Errorf("git stash pop %s: %w", ref, err)
	}
	return nil
}

// StashDrop removes stash@{index} without applying it.
func StashDrop(ctx context.Context, dir string, index int) error {
	ref := fmt.Sprintf("stash@{%d}", index)
	_, err := Run(ctx, dir, "stash", "drop", ref)
	if err != nil {
		return fmt.Errorf("git stash drop %s: %w", ref, err)
	}
	return nil
}

// CherryPick applies the given commit hash to the current branch.
func CherryPick(ctx context.Context, dir, hash string) error {
	_, err := Run(ctx, dir, "cherry-pick", hash)
	if err != nil {
		return fmt.Errorf("git cherry-pick %s: %w", hash, err)
	}
	return nil
}

// AbortMerge runs git merge --abort.
func AbortMerge(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "merge", "--abort")
	if err != nil {
		return fmt.Errorf("git merge --abort: %w", err)
	}
	return nil
}

// AbortRebase runs git rebase --abort.
func AbortRebase(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("git rebase --abort: %w", err)
	}
	return nil
}

// ContinueRebase runs git rebase --continue.
func ContinueRebase(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "rebase", "--continue")
	if err != nil {
		return fmt.Errorf("git rebase --continue: %w", err)
	}
	return nil
}

// ensure ctxDefault and ctxLong are referenced (used by git_service.go via this package).
var _ = ctxDefault
var _ = ctxLong
