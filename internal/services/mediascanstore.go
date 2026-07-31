package services

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

const (
	mediaSummaryFile = "summary.json"
	mediaDetailFile  = "detail.ndjson.gz"
)

// MediaScanStore persists media scans under baseDir/<projectID>/<scanID>/. The
// summary stays small enough to hand to the UI whole; the per-file rows go into a
// gzipped NDJSON file that the UI reads a page at a time, so a site with hundreds
// of thousands of files never crosses the bridge in one go.
type MediaScanStore struct {
	baseDir string
}

func NewMediaScanStore(baseDir string) *MediaScanStore {
	return &MediaScanStore{baseDir: baseDir}
}

// DefaultMediaScanDir is ~/.config/rdm/media-scans.
func DefaultMediaScanDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "media-scans")
}

func (s *MediaScanStore) scanDir(projectID, scanID string) string {
	return filepath.Join(s.baseDir, projectID, scanID)
}

// Save writes the summary and, when there are any, the detail rows.
func (s *MediaScanStore) Save(sum domain.MediaScanSummary, detail []domain.MediaFileRow) error {
	dir := s.scanDir(sum.ProjectID, sum.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir mediascan: %w", err)
	}
	data, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal samenvatting: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, mediaSummaryFile), data, 0o644); err != nil {
		return fmt.Errorf("schrijf samenvatting: %w", err)
	}
	if len(detail) == 0 {
		return nil
	}
	return s.writeDetail(filepath.Join(dir, mediaDetailFile), detail)
}

func (s *MediaScanStore) writeDetail(path string, rows []domain.MediaFileRow) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open detailbestand: %w", err)
	}
	defer f.Close()

	zw := gzip.NewWriter(f)
	enc := json.NewEncoder(zw)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			zw.Close()
			return fmt.Errorf("schrijf detailregel: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("sluit gzip: %w", err)
	}
	return f.Close()
}

// Get reads one scan's summary.
func (s *MediaScanStore) Get(projectID, scanID string) (domain.MediaScanSummary, error) {
	data, err := os.ReadFile(filepath.Join(s.scanDir(projectID, scanID), mediaSummaryFile))
	if err != nil {
		return domain.MediaScanSummary{}, fmt.Errorf("lees mediascan: %w", err)
	}
	var sum domain.MediaScanSummary
	if err := json.Unmarshal(data, &sum); err != nil {
		return domain.MediaScanSummary{}, fmt.Errorf("parse mediascan: %w", err)
	}
	return sum, nil
}

// List returns a project's scans, newest first. No scans yields an empty slice.
func (s *MediaScanStore) List(projectID string) ([]domain.MediaScanSummary, error) {
	entries, err := os.ReadDir(filepath.Join(s.baseDir, projectID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lees scanmap: %w", err)
	}
	var scans []domain.MediaScanSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sum, err := s.Get(projectID, e.Name())
		if err != nil {
			continue // een halve scan mag de lijst niet breken
		}
		scans = append(scans, sum)
	}
	sort.Slice(scans, func(i, j int) bool { return scans[i].ScannedAt.After(scans[j].ScannedAt) })
	return scans, nil
}

// Latest returns the newest scan for a project, or nil when there is none — the
// normal state for a project that was never scanned.
func (s *MediaScanStore) Latest(projectID string) (*domain.MediaScanSummary, error) {
	scans, err := s.List(projectID)
	if err != nil || len(scans) == 0 {
		return nil, err
	}
	return &scans[0], nil
}

// Detail returns a window of a scan's per-file rows, optionally limited to one
// category and/or one folder prefix. The detail file holds everything in one stream,
// so filtering has to happen while reading: counting rows that the caller filters out
// would make the offset meaningless. An offset past the end yields an empty slice
// rather than an error, so the UI can page until it runs dry.
func (s *MediaScanStore) Detail(projectID, scanID string, category domain.MediaCategory, prefix string, offset, limit int) ([]domain.MediaFileRow, error) {
	if limit <= 0 {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(s.scanDir(projectID, scanID), mediaDetailFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open detailbestand: %w", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip lezen: %w", err)
	}
	defer zr.Close()

	rows := make([]domain.MediaFileRow, 0, limit)
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	gezien := 0
	for regel := 0; sc.Scan(); regel++ {
		var row domain.MediaFileRow
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("parse detailregel %d: %w", regel, err)
		}
		if category != "" && row.Category != category {
			continue
		}
		if prefix != "" && !hoortBijMap(row.Path, prefix) {
			continue
		}
		gezien++
		if gezien <= offset {
			continue
		}
		rows = append(rows, row)
		if len(rows) == limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("lees detailbestand: %w", err)
	}
	return rows, nil
}

// hoortBijMap zegt of een pad in de opgegeven map zit. "." is de hoofdmap van
// uploads: daar horen alleen bestanden zonder submap in.
func hoortBijMap(pad, prefix string) bool {
	if prefix == "." {
		return !strings.Contains(pad, "/")
	}
	return strings.HasPrefix(pad, strings.TrimSuffix(prefix, "/")+"/")
}

// RowsForCategories streams a scan's detail file and returns the rows of the given
// categories, keyed by path. The quarantine flow uses this to verify that a requested
// file really was placed in a safe category by that scan — a check that has to run
// against what was stored, not against what a caller claims.
func (s *MediaScanStore) RowsForCategories(projectID, scanID string, cats ...domain.MediaCategory) (map[string]domain.MediaFileRow, error) {
	gewenst := make(map[domain.MediaCategory]bool, len(cats))
	for _, c := range cats {
		gewenst[c] = true
	}

	f, err := os.Open(filepath.Join(s.scanDir(projectID, scanID), mediaDetailFile))
	if os.IsNotExist(err) {
		return map[string]domain.MediaFileRow{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open detailbestand: %w", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip lezen: %w", err)
	}
	defer zr.Close()

	uit := map[string]domain.MediaFileRow{}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var row domain.MediaFileRow
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("parse detailregel: %w", err)
		}
		if gewenst[row.Category] {
			uit[row.Path] = row
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("lees detailbestand: %w", err)
	}
	return uit, nil
}
