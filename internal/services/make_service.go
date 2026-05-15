package services

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MakeTarget is a parsed Makefile target.
type MakeTarget struct {
	Name string `json:"name"`
}

// MakeResult holds the stdout/stderr output of a make invocation.
type MakeResult struct {
	Target  string `json:"target"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
}

type MakeService struct {
	project *ProjectService
}

func NewMakeService(project *ProjectService) *MakeService {
	return &MakeService{project: project}
}

func (s *MakeService) pathFor(id string) (string, error) {
	for _, p := range s.project.List() {
		if p.ID == id {
			return p.Path, nil
		}
	}
	return "", fmt.Errorf("project not found: %s", id)
}

// HasMakefile returns true if a Makefile exists in the project root.
func (s *MakeService) HasMakefile(projectID string) (bool, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(path, "Makefile"))
	return err == nil, nil
}

// GetTargets parses and returns the .PHONY targets from the Makefile.
func (s *MakeService) GetTargets(projectID string) ([]MakeTarget, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(path, "Makefile"))
	if os.IsNotExist(err) {
		return make([]MakeTarget, 0), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Makefile: %w", err)
	}

	// Parse .PHONY lines for declared targets
	phonyRe := regexp.MustCompile(`^\.PHONY:\s+(.+)`)
	targets := make([]MakeTarget, 0)
	seen := map[string]bool{}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if m := phonyRe.FindStringSubmatch(line); m != nil {
			for _, t := range strings.Fields(m[1]) {
				if !seen[t] {
					seen[t] = true
					targets = append(targets, MakeTarget{Name: t})
				}
			}
		}
	}
	return targets, nil
}

// Run executes `make <target>` in the project directory and returns combined output.
func (s *MakeService) Run(projectID, target string) (*MakeResult, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "make", target)
	cmd.Dir = path
	out, err := cmd.CombinedOutput()

	result := &MakeResult{
		Target:  target,
		Output:  string(out),
		Success: err == nil,
	}
	return result, nil
}
