package services

import (
	"context"
	"sync"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
)

// BatchResult holds the outcome of a single project operation.
type BatchResult struct {
	ProjectID   string `json:"projectId"`
	DisplayName string `json:"displayName"`
	Op          string `json:"op"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

type BatchService struct {
	project *ProjectService
}

func NewBatchService(project *ProjectService) *BatchService {
	return &BatchService{project: project}
}

// FetchAll runs git fetch in parallel across all projects that are git repos.
func (s *BatchService) FetchAll() []BatchResult {
	return s.runAll("fetch", func(ctx context.Context, path string) error {
		return gitcli.Fetch(ctx, path)
	})
}

// PullAll runs git pull in parallel across all projects that are git repos.
func (s *BatchService) PullAll() []BatchResult {
	return s.runAll("pull", func(ctx context.Context, path string) error {
		return gitcli.Pull(ctx, path)
	})
}

func (s *BatchService) runAll(op string, fn func(context.Context, string) error) []BatchResult {
	projects := s.project.List()
	results := make([]BatchResult, len(projects))
	var wg sync.WaitGroup

	for i, p := range projects {
		if !p.Git.IsRepo {
			results[i] = BatchResult{
				ProjectID:   p.ID,
				DisplayName: p.DisplayName,
				Op:          op,
				Success:     false,
				Error:       "not a git repo",
			}
			continue
		}

		wg.Add(1)
		go func(idx int, path, id, name string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			err := fn(ctx, path)
			r := BatchResult{
				ProjectID:   id,
				DisplayName: name,
				Op:          op,
				Success:     err == nil,
			}
			if err != nil {
				r.Error = err.Error()
			}
			results[idx] = r
		}(i, p.Path, p.ID, p.DisplayName)
	}

	wg.Wait()
	return results
}
