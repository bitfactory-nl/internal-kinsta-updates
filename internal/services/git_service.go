package services

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/gitread"
	"github.com/rdm/sites-tool/internal/domain"
)

// GitService exposes git operations scoped to a project by ID.
type GitService struct {
	project *ProjectService
}

// NewGitService creates a GitService backed by the given ProjectService.
func NewGitService(project *ProjectService) *GitService {
	return &GitService{project: project}
}

// pathFor resolves a projectID to its filesystem path.
func (s *GitService) pathFor(id string) (string, error) {
	for _, p := range s.project.List() {
		if p.ID == id {
			return p.Path, nil
		}
	}
	return "", fmt.Errorf("project %q not found", id)
}

// ctxDefault returns a 30-second context suitable for most operations.
func (s *GitService) ctxDefault() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// ctxLong returns a 60-second context suitable for push/pull.
func (s *GitService) ctxLong() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// GetHistory returns the commit graph for a project.
func (s *GitService) GetHistory(projectID string, limit int) ([]domain.GraphCommit, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git history: %w", err)
	}
	commits, err := gitread.CommitHistory(path, limit)
	if err != nil {
		return nil, fmt.Errorf("git history: %w", err)
	}
	return commits, nil
}

// GetCommitDiff returns file diffs for a specific commit.
func (s *GitService) GetCommitDiff(projectID, hash string) ([]domain.FileDiff, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git commit diff: %w", err)
	}
	diffs, err := gitread.CommitDiff(path, hash)
	if err != nil {
		return nil, fmt.Errorf("git commit diff: %w", err)
	}
	return diffs, nil
}

// GetWorkingDiff returns staged or unstaged diffs for the working tree.
func (s *GitService) GetWorkingDiff(projectID string, staged bool) ([]domain.FileDiff, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git working diff: %w", err)
	}
	diffs, err := gitread.WorkingDiff(path, staged)
	if err != nil {
		return nil, fmt.Errorf("git working diff: %w", err)
	}
	return diffs, nil
}

// GetStatus returns the current git status for a project.
func (s *GitService) GetStatus(projectID string) (domain.GitStatus, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return domain.GitStatus{}, fmt.Errorf("git status: %w", err)
	}
	status, err := gitread.Status(path)
	if err != nil {
		return domain.GitStatus{}, fmt.Errorf("git status: %w", err)
	}
	return status, nil
}

// StageFiles stages the given file paths.
func (s *GitService) StageFiles(projectID string, paths []string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git stage files: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.StageFiles(ctx, path, paths)
}

// UnstageFiles removes the given files from the staging area.
func (s *GitService) UnstageFiles(projectID string, paths []string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git unstage files: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.UnstageFiles(ctx, path, paths)
}

// StageAll stages all changes (git add -A).
func (s *GitService) StageAll(projectID string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git stage all: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.StageAll(ctx, path)
}

// DiscardFile discards working-tree changes for a tracked file.
func (s *GitService) DiscardFile(projectID, filePath string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git discard file: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.DiscardFile(ctx, path, filePath)
}

// Commit creates a commit with the given message, optionally amending.
func (s *GitService) Commit(projectID, message string, amend bool) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.Commit(ctx, path, message, amend)
}

// Fetch runs git fetch --all --prune.
func (s *GitService) Fetch(projectID string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	ctx, cancel := s.ctxLong()
	defer cancel()
	return gitcli.Fetch(ctx, path)
}

// Pull runs git pull.
func (s *GitService) Pull(projectID string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	ctx, cancel := s.ctxLong()
	defer cancel()
	return gitcli.Pull(ctx, path)
}

// Push pushes the current branch to its upstream remote.
// It resolves the remote and branch from git status.
func (s *GitService) Push(projectID string, force bool) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	status, err := gitread.Status(path)
	if err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	remote := "origin"
	branch := status.Branch
	// Upstream may be "origin/main" — derive remote from it.
	if status.Upstream != "" {
		parts := strings.SplitN(status.Upstream, "/", 2)
		if len(parts) == 2 {
			remote = parts[0]
			branch = parts[1]
		}
	}

	ctx, cancel := s.ctxLong()
	defer cancel()
	return gitcli.Push(ctx, path, remote, branch, force)
}

// GetBranches returns all local and remote branches.
func (s *GitService) GetBranches(projectID string) ([]domain.Branch, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git branches: %w", err)
	}
	branches, err := gitread.Branches(path)
	if err != nil {
		return nil, fmt.Errorf("git branches: %w", err)
	}
	return branches, nil
}

// CheckoutBranch checks out the named branch.
func (s *GitService) CheckoutBranch(projectID, name string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.CheckoutBranch(ctx, path, name)
}

// CreateBranch creates a new branch, optionally from a specific starting point.
func (s *GitService) CreateBranch(projectID, name, from string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git create branch: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.CreateBranch(ctx, path, name, from)
}

// DeleteBranch deletes a branch; force=true skips the safety check.
func (s *GitService) DeleteBranch(projectID, name string, force bool) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git delete branch: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.DeleteBranch(ctx, path, name, force)
}

// MergeBranch merges the named branch into the current branch.
func (s *GitService) MergeBranch(projectID, name string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git merge: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.MergeBranch(ctx, path, name)
}

// GetStashes returns all stash entries for a project.
func (s *GitService) GetStashes(projectID string) ([]domain.Stash, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git stashes: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()

	// Format: stash@{0}: On branch: message  OR  stash@{0}: WIP on branch: message
	out, err := gitcli.Run(ctx, path, "stash", "list", "--format=%gd|%ai|%gs")
	if err != nil {
		return nil, fmt.Errorf("git stashes: %w", err)
	}
	if out == "" {
		return []domain.Stash{}, nil
	}

	var stashes []domain.Stash
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}

		// parts[0]: stash@{0}
		// parts[1]: ISO date
		// parts[2]: "WIP on branch: message" or "On branch: message"
		ref := parts[0]
		dateStr := parts[1]
		subject := parts[2]

		index := 0
		if start := strings.Index(ref, "{"); start >= 0 {
			end := strings.Index(ref, "}")
			if end > start {
				index, _ = strconv.Atoi(ref[start+1 : end])
			}
		}

		var stashedAt time.Time
		stashedAt, _ = time.Parse("2006-01-02 15:04:05 -0700", dateStr)

		// Extract branch and message from subject.
		branch := ""
		message := subject
		if strings.HasPrefix(subject, "WIP on ") {
			rest := strings.TrimPrefix(subject, "WIP on ")
			if idx := strings.Index(rest, ": "); idx >= 0 {
				branch = rest[:idx]
				message = rest[idx+2:]
			} else {
				branch = rest
			}
		} else if strings.HasPrefix(subject, "On ") {
			rest := strings.TrimPrefix(subject, "On ")
			if idx := strings.Index(rest, ": "); idx >= 0 {
				branch = rest[:idx]
				message = rest[idx+2:]
			}
		}

		stashes = append(stashes, domain.Stash{
			Index:     index,
			Message:   message,
			Branch:    branch,
			StashedAt: stashedAt,
		})
	}
	return stashes, nil
}

// StashSave creates a new stash entry.
func (s *GitService) StashSave(projectID, message string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git stash save: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.StashSave(ctx, path, message)
}

// StashPop applies and removes stash@{index}.
func (s *GitService) StashPop(projectID string, index int) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git stash pop: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.StashPop(ctx, path, index)
}

// StashDrop removes stash@{index} without applying it.
func (s *GitService) StashDrop(projectID string, index int) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git stash drop: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.StashDrop(ctx, path, index)
}

// GetTags returns all tags sorted by version/refname descending.
func (s *GitService) GetTags(projectID string) ([]domain.Tag, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git tags: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()

	format := "%(refname:short)\t%(creatordate:iso)\t%(*objectname)\t%(objectname)\t%(subject)\t%(objecttype)"
	out, err := gitcli.Run(ctx, path, "tag", "-l", "--sort=-version:refname", "--format="+format)
	if err != nil {
		return nil, fmt.Errorf("git tags: %w", err)
	}
	if out == "" {
		return []domain.Tag{}, nil
	}

	var tags []domain.Tag
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 6)
		if len(fields) < 6 {
			continue
		}
		name := fields[0]
		dateStr := fields[1]
		// fields[2] = dereferenced object hash (non-empty for annotated tags)
		pointedHash := fields[2]
		directHash := fields[3]
		subject := fields[4]
		objType := fields[5]

		taggedAt, _ := time.Parse("2006-01-02 15:04:05 -0700", dateStr)

		annotated := objType == "tag"
		commitHash := directHash
		if annotated && pointedHash != "" {
			commitHash = pointedHash
		}

		tags = append(tags, domain.Tag{
			Name:      name,
			Annotated: annotated,
			Message:   subject,
			Commit:    commitHash,
			TaggedAt:  taggedAt,
		})
	}
	return tags, nil
}

// GetBlame returns per-line blame for a file.
func (s *GitService) GetBlame(projectID, filePath string) ([]domain.BlameLine, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git blame: %w", err)
	}
	lines, err := gitread.Blame(path, filePath)
	if err != nil {
		return nil, fmt.Errorf("git blame: %w", err)
	}
	return lines, nil
}

// GetFileHistory returns the commit log for a specific file.
func (s *GitService) GetFileHistory(projectID, filePath string) ([]domain.Commit, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git file history: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()

	sep := "\x1f"
	format := "%H" + sep + "%h" + sep + "%an" + sep + "%aI" + sep + "%s"
	out, err := gitcli.Run(ctx, path, "log", "--pretty=format:"+format, "--", filePath)
	if err != nil {
		return nil, fmt.Errorf("git file history: %w", err)
	}
	if out == "" {
		return []domain.Commit{}, nil
	}

	var commits []domain.Commit
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, sep, 5)
		if len(fields) < 5 {
			continue
		}
		authoredAt, _ := time.Parse(time.RFC3339, fields[3])
		commits = append(commits, domain.Commit{
			Hash:       fields[0],
			ShortHash:  fields[1],
			Author:     domain.Person{Name: fields[2]},
			Subject:    fields[4],
			AuthoredAt: authoredAt,
		})
	}
	return commits, nil
}

// AbortMerge aborts an in-progress merge.
func (s *GitService) AbortMerge(projectID string) error {
	path, err := s.pathFor(projectID)
	if err != nil {
		return fmt.Errorf("git merge abort: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	return gitcli.AbortMerge(ctx, path)
}

// GetFiles returns all tracked files in the repository via git ls-files.
// UpdateBranch represents a detected automated WordPress update branch.
type UpdateBranch struct {
	Name      string `json:"name"`
	ShortName string `json:"shortName"` // without remote prefix
	DateStr   string `json:"dateStr"`   // raw date portion from branch name
	IsLocal   bool   `json:"isLocal"`   // true if also checked out locally
}

// updateBranchPrefixes are the branch name prefixes recognised as automated update branches.
var updateBranchPrefixes = []string{
	"automated/wp-updates-",
	"automated/updates-",
	"Updates - ",
}

// GetUpdateBranches returns remote branches matching known update branch patterns, sorted newest first.
func (s *GitService) GetUpdateBranches(projectID string) ([]UpdateBranch, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("update branches: %w", err)
	}

	branches, err := gitread.Branches(path)
	if err != nil {
		return nil, fmt.Errorf("update branches: %w", err)
	}

	localNames := map[string]bool{}
	for _, b := range branches {
		if !b.IsRemote {
			localNames[b.Name] = true
		}
	}

	result := make([]UpdateBranch, 0)
	seen := map[string]bool{}

	for _, b := range branches {
		if !b.IsRemote {
			continue
		}
		// strip "origin/" prefix
		short := b.Name
		if idx := strings.Index(short, "/"); idx >= 0 {
			short = short[idx+1:]
		}
		var dateStr string
		matched := false
		for _, prefix := range updateBranchPrefixes {
			if strings.HasPrefix(short, prefix) {
				dateStr = strings.TrimPrefix(short, prefix)
				matched = true
				break
			}
		}
		if !matched || seen[short] {
			continue
		}
		seen[short] = true
		result = append(result, UpdateBranch{
			Name:      b.Name,
			ShortName: short,
			DateStr:   dateStr,
			IsLocal:   localNames[short],
		})
	}

	// Sort newest first (lexicographic on dateStr works for ISO-style dates)
	sort.Slice(result, func(i, j int) bool {
		return result[i].DateStr > result[j].DateStr
	})
	return result, nil
}

func (s *GitService) GetFiles(projectID string) ([]string, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()
	lines, err := gitcli.RunLines(ctx, path, "ls-files")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}
