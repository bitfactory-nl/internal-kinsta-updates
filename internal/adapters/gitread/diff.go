package gitread

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// CommitDiff returns FileDiff for each file changed in a commit.
// For the initial commit (no parents) it falls back to git show.
func CommitDiff(repoPath, hash string) ([]domain.FileDiff, error) {
	// Try diff-tree first; it fails gracefully for root commits.
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "-r", "-p", "--find-renames", hash)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		// Likely a root commit — fall back to git show.
		cmd2 := exec.Command("git", "show", hash, "--format=", "-p", "--find-renames")
		cmd2.Dir = repoPath
		out, err = cmd2.Output()
		if err != nil {
			return nil, fmt.Errorf("git commit diff %s: %w", hash, err)
		}
	}
	diffs := ParseUnifiedDiff(string(out))
	return diffs, nil
}

// WorkingDiff returns diffs for staged or unstaged changes.
// staged=true: git diff --cached HEAD
// staged=false: git diff
func WorkingDiff(repoPath string, staged bool) ([]domain.FileDiff, error) {
	var args []string
	if staged {
		args = []string{"diff", "--cached", "HEAD"}
	} else {
		args = []string{"diff"}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git working diff: %w", err)
	}
	return ParseUnifiedDiff(string(out)), nil
}

// ParseUnifiedDiff parses unified diff text (as produced by git diff) into []FileDiff.
func ParseUnifiedDiff(text string) []domain.FileDiff {
	lines := strings.Split(text, "\n")
	var diffs []domain.FileDiff
	var cur *domain.FileDiff
	var curHunk *domain.DiffHunk
	oldLineNo := 0
	newLineNo := 0

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if cur != nil {
				if curHunk != nil {
					cur.Hunks = append(cur.Hunks, *curHunk)
					curHunk = nil
				}
				diffs = append(diffs, *cur)
			}
			cur = &domain.FileDiff{}
			oldLineNo = 0
			newLineNo = 0

		case cur == nil:
			// Skip lines before the first "diff --git" header.
			continue

		case strings.HasPrefix(line, "Binary files"):
			cur.Binary = true

		case strings.HasPrefix(line, "--- "):
			path := strings.TrimPrefix(line, "--- ")
			path = strings.TrimPrefix(path, "a/")
			if path != "/dev/null" {
				cur.OldPath = path
			}

		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimPrefix(line, "+++ ")
			path = strings.TrimPrefix(path, "b/")
			if path != "/dev/null" {
				cur.Path = path
			}
			// If OldPath differs from Path it is a rename.
			if cur.OldPath != "" && cur.OldPath != cur.Path {
				// OldPath already set; leave both.
			} else {
				cur.OldPath = ""
			}

		case strings.HasPrefix(line, "@@ "):
			if curHunk != nil {
				cur.Hunks = append(cur.Hunks, *curHunk)
			}
			curHunk = parseHunkHeader(line)
			if curHunk != nil {
				oldLineNo = curHunk.OldStart
				newLineNo = curHunk.NewStart
			}

		case curHunk != nil && len(line) > 0:
			ch := line[0]
			content := line[1:]
			switch ch {
			case '+':
				curHunk.Lines = append(curHunk.Lines, domain.DiffLine{
					Kind:    domain.LineAdd,
					Content: content,
					NewNum:  newLineNo,
				})
				newLineNo++
			case '-':
				curHunk.Lines = append(curHunk.Lines, domain.DiffLine{
					Kind:    domain.LineDel,
					Content: content,
					OldNum:  oldLineNo,
				})
				oldLineNo++
			case ' ':
				curHunk.Lines = append(curHunk.Lines, domain.DiffLine{
					Kind:    domain.LineContext,
					Content: content,
					OldNum:  oldLineNo,
					NewNum:  newLineNo,
				})
				oldLineNo++
				newLineNo++
			}
		}
	}

	// Flush trailing hunk and file.
	if cur != nil {
		if curHunk != nil {
			cur.Hunks = append(cur.Hunks, *curHunk)
		}
		diffs = append(diffs, *cur)
	}
	return diffs
}

// parseHunkHeader parses a line like "@@ -1,4 +1,6 @@ optional context".
func parseHunkHeader(line string) *domain.DiffHunk {
	hunk := &domain.DiffHunk{Header: line}
	// Extract the range part between @@ and @@
	inner := line
	inner = strings.TrimPrefix(inner, "@@ ")
	end := strings.Index(inner, " @@")
	if end < 0 {
		return hunk
	}
	ranges := strings.Fields(inner[:end])
	if len(ranges) < 2 {
		return hunk
	}
	hunk.OldStart, hunk.OldLines = parseRange(ranges[0])
	hunk.NewStart, hunk.NewLines = parseRange(ranges[1])
	return hunk
}

// parseRange parses "-1,4" or "+1,4" into (start, count).
// Count defaults to 1 if absent.
func parseRange(s string) (start, count int) {
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	parts := strings.SplitN(s, ",", 2)
	start, _ = strconv.Atoi(parts[0])
	count = 1
	if len(parts) == 2 {
		count, _ = strconv.Atoi(parts[1])
	}
	return start, count
}
