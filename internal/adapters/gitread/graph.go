package gitread

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

const (
	recordSep = "\x1e"
	unitSep   = "\x1f"
)

// CommitHistory fetches commits using git log CLI and builds a DAG graph with
// lane/column/edge data for rendering a visual commit graph.
func CommitHistory(repoPath string, limit int) ([]domain.GraphCommit, error) {
	if limit <= 0 {
		limit = 200
	}

	format := recordSep + "%H" + unitSep + "%h" + unitSep + "%an" + unitSep + "%aI" + unitSep + "%P" + unitSep + "%D" + unitSep + "%s"
	args := []string{
		"log",
		"--pretty=format:" + format,
		"--all",
		fmt.Sprintf("-n%d", limit),
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git history: %w", err)
	}

	text := strings.TrimSpace(string(raw))
	if text == "" {
		return []domain.GraphCommit{}, nil
	}

	// Split on record separator; first element may be empty due to leading \x1e
	records := strings.Split(text, recordSep)
	var commits []domain.GraphCommit
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, unitSep, 7)
		if len(fields) < 7 {
			continue
		}

		hash := strings.TrimSpace(fields[0])
		shortHash := strings.TrimSpace(fields[1])
		author := strings.TrimSpace(fields[2])
		dateStr := strings.TrimSpace(fields[3])
		parentsRaw := strings.TrimSpace(fields[4])
		refsRaw := strings.TrimSpace(fields[5])
		subject := strings.TrimSpace(fields[6])

		authorDate, _ := time.Parse(time.RFC3339, dateStr)

		var parents []string
		if parentsRaw != "" {
			for _, p := range strings.Fields(parentsRaw) {
				if p != "" {
					parents = append(parents, p)
				}
			}
		}

		refs := parseRefs(refsRaw)

		commits = append(commits, domain.GraphCommit{
			Hash:       hash,
			ShortHash:  shortHash,
			Subject:    subject,
			Author:     author,
			AuthorDate: authorDate,
			Parents:    parents,
			Refs:       refs,
		})
	}

	assignLanes(commits)
	return commits, nil
}

// parseRefs parses the %D decoration field into a clean slice of ref names.
// Input example: "HEAD -> main, origin/main, tag: v1.0"
func parseRefs(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ", ")
	refs := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// "HEAD -> main" — keep both HEAD and the branch name
		if strings.HasPrefix(p, "HEAD -> ") {
			refs = append(refs, "HEAD")
			refs = append(refs, strings.TrimPrefix(p, "HEAD -> "))
			continue
		}
		// "tag: v1.0" — strip prefix
		if strings.HasPrefix(p, "tag: ") {
			refs = append(refs, strings.TrimPrefix(p, "tag: "))
			continue
		}
		refs = append(refs, p)
	}
	return refs
}

// assignLanes computes Lane, Columns, and Edges for each commit in the slice
// (ordered newest-first, as git log returns them).
func assignLanes(commits []domain.GraphCommit) {
	// openLanes[i] holds the hash of the commit expected to arrive in lane i.
	// Empty string means the lane is free.
	openLanes := []string{}

	for i := range commits {
		gc := &commits[i]

		// 1. Find or allocate a lane for this commit.
		lane := laneFor(openLanes, gc.Hash)
		if lane == -1 {
			lane = allocLane(&openLanes, gc.Hash)
		}
		gc.Lane = lane

		// 2. This commit occupies its lane; replace it with its first parent (or free it).
		if len(gc.Parents) > 0 {
			openLanes[lane] = gc.Parents[0]
		} else {
			openLanes[lane] = ""
		}

		// 3. Additional parents: assign lanes if not already open.
		for _, parent := range gc.Parents[1:] {
			if laneFor(openLanes, parent) == -1 {
				allocLane(&openLanes, parent)
			}
		}

		// 4. Compute columns (highest occupied lane index + 1).
		cols := 0
		for l, h := range openLanes {
			if h != "" {
				cols = l + 1
			}
		}
		// The commit's own lane counts even if it was just freed.
		if lane+1 > cols {
			cols = lane + 1
		}
		gc.Columns = cols

		// 5. Build edges: one edge per occupied lane after update (straight lines),
		//    plus a diagonal edge for each additional parent merging into this commit.
		edges := make([]domain.GraphEdge, 0)
		for l, h := range openLanes {
			if h != "" {
				edges = append(edges, domain.GraphEdge{
					FromLane:  l,
					ToLane:    l,
					ColorLane: l,
				})
			}
		}
		// Diagonal edges: each parent beyond the first was already in openLanes or
		// just allocated; we emit a merging edge from this commit's lane to the
		// parent's lane so the renderer can draw the curve.
		for _, parent := range gc.Parents[1:] {
			parentLane := laneFor(openLanes, parent)
			if parentLane != -1 && parentLane != lane {
				edges = append(edges, domain.GraphEdge{
					FromLane:  lane,
					ToLane:    parentLane,
					ColorLane: parentLane,
				})
			}
		}
		gc.Edges = edges
	}
}

// laneFor returns the index of the lane holding hash, or -1 if not found.
func laneFor(lanes []string, hash string) int {
	for i, h := range lanes {
		if h == hash {
			return i
		}
	}
	return -1
}

// allocLane places hash into the first free slot (empty string) or appends a new lane.
// Returns the allocated lane index.
func allocLane(lanes *[]string, hash string) int {
	for i, h := range *lanes {
		if h == "" {
			(*lanes)[i] = hash
			return i
		}
	}
	*lanes = append(*lanes, hash)
	return len(*lanes) - 1
}
