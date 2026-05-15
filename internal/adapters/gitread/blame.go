package gitread

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// Blame returns per-line blame information for a file using git blame --porcelain.
func Blame(repoPath, filePath string) ([]domain.BlameLine, error) {
	cmd := exec.Command("git", "blame", "--porcelain", filePath)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git blame %s: %w", filePath, err)
	}
	return parsePorcelain(string(out)), nil
}

// parsePorcelain parses git blame --porcelain output into BlameLine slices.
//
// Format per block:
//
//	<hash> <orig-line> <final-line> [<num-lines>]
//	author <name>
//	author-mail <email>
//	author-time <unix-timestamp>
//	... (other metadata lines)
//	filename <filename>
//	\t<line content>
func parsePorcelain(text string) []domain.BlameLine {
	lines := strings.Split(text, "\n")

	// Cache commit metadata so repeat hashes reuse the same info.
	type commitInfo struct {
		Author domain.Person
		Date   time.Time
	}
	cache := map[string]*commitInfo{}

	var result []domain.BlameLine
	var curHash string
	var finalLine int
	var info *commitInfo

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}

		// Header line: 40-char hex hash followed by line numbers.
		if len(line) >= 40 && isHex(line[:40]) && (len(line) == 40 || line[40] == ' ') {
			fields := strings.Fields(line)
			curHash = fields[0]
			if len(fields) >= 3 {
				finalLine, _ = strconv.Atoi(fields[2])
			}
			// Retrieve or create cache entry.
			if existing, ok := cache[curHash]; ok {
				info = existing
			} else {
				info = &commitInfo{}
				cache[curHash] = info
			}
			continue
		}

		// Metadata lines.
		switch {
		case strings.HasPrefix(line, "author "):
			if info != nil {
				info.Author.Name = strings.TrimPrefix(line, "author ")
			}
		case strings.HasPrefix(line, "author-mail "):
			if info != nil {
				email := strings.TrimPrefix(line, "author-mail ")
				email = strings.Trim(email, "<>")
				info.Author.Email = email
			}
		case strings.HasPrefix(line, "author-time "):
			if info != nil {
				ts, _ := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
				info.Date = time.Unix(ts, 0).UTC()
			}
		case strings.HasPrefix(line, "\t"):
			// Actual line content — flush a BlameLine.
			content := strings.TrimPrefix(line, "\t")
			bl := domain.BlameLine{
				LineNo:  finalLine,
				Commit:  curHash,
				Content: content,
			}
			if info != nil {
				bl.Author = info.Author
				bl.Date = info.Date
			}
			result = append(result, bl)
		}
	}
	return result
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
