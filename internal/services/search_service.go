package services

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
)

// SearchHit represents a single grep match in a file.
type SearchHit struct {
	ProjectID   string `json:"projectId"`
	DisplayName string `json:"displayName"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Content     string `json:"content"`
}

type SearchService struct {
	project *ProjectService
}

func NewSearchService(project *ProjectService) *SearchService {
	return &SearchService{project: project}
}

// GrepAll searches all git-tracked files across all repos for the given query.
func (s *SearchService) GrepAll(query string) []SearchHit {
	if strings.TrimSpace(query) == "" {
		return make([]SearchHit, 0)
	}

	projects := s.project.List()
	var mu sync.Mutex
	var wg sync.WaitGroup
	all := make([]SearchHit, 0)

	for _, p := range projects {
		if !p.Git.IsRepo {
			continue
		}
		wg.Add(1)
		go func(path, id, name string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// git grep -n -i -F "query" -- tracked files only
			lines, err := gitcli.RunLines(ctx, path, "grep", "-n", "-i", "-F", query)
			if err != nil {
				return
			}

			var hits []SearchHit
			for _, l := range lines {
				// format: filepath:linenum:content
				parts := strings.SplitN(l, ":", 3)
				if len(parts) < 3 {
					continue
				}
				lineNo := 0
				for _, ch := range parts[1] {
					if ch >= '0' && ch <= '9' {
						lineNo = lineNo*10 + int(ch-'0')
					}
				}
				hits = append(hits, SearchHit{
					ProjectID:   id,
					DisplayName: name,
					File:        parts[0],
					Line:        lineNo,
					Content:     parts[2],
				})
			}

			if len(hits) > 0 {
				mu.Lock()
				all = append(all, hits...)
				mu.Unlock()
			}
		}(p.Path, p.ID, p.DisplayName)
	}

	wg.Wait()
	return all
}
