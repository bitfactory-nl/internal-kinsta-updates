package gitread

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/rdm/sites-tool/internal/domain"
)

// Status returns a fast git status using CLI (avoids go-git walking node_modules etc.)
func Status(repoPath string) (domain.GitStatus, error) {
	// Quick check: is this a git repo?
	if err := git2(repoPath, "rev-parse", "--git-dir"); err != nil {
		return domain.GitStatus{
			IsRepo:     false,
			Staged:     []domain.FileChange{},
			Unstaged:   []domain.FileChange{},
			Untracked:  []string{},
			Conflicted: []string{},
		}, nil
	}

	// Branch name
	branch := gitOut(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "HEAD" {
		// detached HEAD — use short hash
		branch = gitOut(repoPath, "rev-parse", "--short", "HEAD")
	}

	// Upstream tracking branch
	upstream := gitOut(repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")

	// Ahead / behind counts
	ahead, behind := 0, 0
	if upstream != "" && !strings.HasPrefix(upstream, "fatal") {
		ab := gitOut(repoPath, "rev-list", "--left-right", "--count", "HEAD...@{u}")
		parts := strings.Fields(ab)
		if len(parts) == 2 {
			ahead, _ = strconv.Atoi(parts[0])
			behind, _ = strconv.Atoi(parts[1])
		}
	}

	// Dirty check: staged + unstaged counts via --porcelain
	staged := make([]domain.FileChange, 0)
	unstaged := make([]domain.FileChange, 0)
	untracked := make([]string, 0)
	conflicted := make([]string, 0)

	porcelain := gitOut(repoPath, "status", "--porcelain", "-z")
	if porcelain != "" {
		entries := strings.Split(porcelain, "\x00")
		for _, e := range entries {
			if len(e) < 3 {
				continue
			}
			x, y, path := rune(e[0]), rune(e[1]), strings.TrimSpace(e[2:])
			switch {
			case x == '?' && y == '?':
				untracked = append(untracked, path)
			case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
				conflicted = append(conflicted, path)
			default:
				if x != ' ' && x != '?' {
					staged = append(staged, domain.FileChange{Path: path, Kind: statusKind(x)})
				}
				if y != ' ' && y != '?' {
					unstaged = append(unstaged, domain.FileChange{Path: path, Kind: statusKind(y)})
				}
			}
		}
	}

	// Submodules check
	hasSubmodules := gitOut(repoPath, "config", "--file", ".gitmodules", "--list") != ""

	return domain.GitStatus{
		IsRepo:        true,
		Branch:        branch,
		Upstream:      upstream,
		Ahead:         ahead,
		Behind:        behind,
		Staged:        staged,
		Unstaged:      unstaged,
		Untracked:     untracked,
		Conflicted:    conflicted,
		HasSubmodules: hasSubmodules,
	}, nil
}

// Branches returns all local and remote branches using go-git (read-only, fast).
func Branches(repoPath string) ([]domain.Branch, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	head, _ := repo.Head()

	refs, err := repo.References()
	if err != nil {
		return nil, fmt.Errorf("references: %w", err)
	}

	var branches []domain.Branch
	_ = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name()
		if !name.IsBranch() && !name.IsRemote() {
			return nil
		}
		b := domain.Branch{
			Name:       name.Short(),
			FullRef:    name.String(),
			IsRemote:   name.IsRemote(),
			HeadCommit: ref.Hash().String(),
		}
		if head != nil && name == head.Name() {
			b.IsCurrent = true
		}
		branches = append(branches, b)
		return nil
	})

	return branches, nil
}

// RecentCommits returns the last n commits on the current branch using go-git.
func RecentCommits(repoPath string, limit int) ([]domain.Commit, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}

	iter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}

	var commits []domain.Commit
	count := 0
	_ = iter.ForEach(func(c *object.Commit) error {
		if count >= limit {
			return fmt.Errorf("stop")
		}
		subject := c.Message
		if idx := strings.Index(subject, "\n"); idx >= 0 {
			subject = subject[:idx]
		}
		var parents []string
		for _, p := range c.ParentHashes {
			parents = append(parents, p.String())
		}
		commits = append(commits, domain.Commit{
			Hash:       c.Hash.String(),
			ShortHash:  c.Hash.String()[:7],
			Author:     domain.Person{Name: c.Author.Name, Email: c.Author.Email},
			Committer:  domain.Person{Name: c.Committer.Name, Email: c.Committer.Email},
			Message:    strings.TrimSpace(c.Message),
			Subject:    subject,
			Parents:    parents,
			AuthoredAt: c.Author.When,
		})
		count++
		return nil
	})

	return commits, nil
}

// git2 runs git and returns error if exit code != 0.
func git2(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

// gitOut runs git and returns trimmed stdout; empty string on error.
func gitOut(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	_ = cmd.Run()
	return strings.TrimSpace(buf.String())
}

func statusKind(code rune) domain.ChangeKind {
	switch code {
	case 'A':
		return domain.ChangeAdded
	case 'D':
		return domain.ChangeDeleted
	case 'R':
		return domain.ChangeRenamed
	default:
		return domain.ChangeModified
	}
}

// unused — kept for future go-git usage
var _ = time.Now
