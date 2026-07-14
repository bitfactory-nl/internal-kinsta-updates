package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

// ReportStore persists client-report drafts on disk under
// baseDir/<projectID>/<period-slug>.json.
type ReportStore struct {
	baseDir string
}

// NewReportStore returns a ReportStore rooted at baseDir.
func NewReportStore(baseDir string) *ReportStore {
	return &ReportStore{baseDir: baseDir}
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugifyPeriod turns "Q3 2026" into "q3-2026".
func slugifyPeriod(period string) string {
	s := strings.ToLower(strings.TrimSpace(period))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// reportFilePath returns the on-disk path for a project+period report.
func reportFilePath(baseDir, projectID, period string) string {
	return filepath.Join(baseDir, projectID, slugifyPeriod(period)+".json")
}

// Save writes the report as <period-slug>.json under the project's directory.
func (s *ReportStore) Save(r domain.Report) error {
	path := reportFilePath(s.baseDir, r.ProjectID, r.Period)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir report dir: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// Get reads a single report. A missing report is not an error: it returns
// (domain.Report{}, nil).
func (s *ReportStore) Get(projectID, period string) (domain.Report, error) {
	path := reportFilePath(s.baseDir, projectID, period)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return domain.Report{}, nil
	}
	if err != nil {
		return domain.Report{}, fmt.Errorf("read report: %w", err)
	}
	var r domain.Report
	if err := json.Unmarshal(data, &r); err != nil {
		return domain.Report{}, fmt.Errorf("parse report: %w", err)
	}
	return r, nil
}

// List returns all stored reports for a project, newest-updated first. A
// missing project directory is not an error: it returns (nil, nil).
func (s *ReportStore) List(projectID string) ([]domain.Report, error) {
	dir := filepath.Join(s.baseDir, projectID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read project reports: %w", err)
	}
	var reports []domain.Report
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r domain.Report
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		reports = append(reports, r)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].UpdatedAt.After(reports[j].UpdatedAt) })
	return reports, nil
}
