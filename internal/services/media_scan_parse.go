package services

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// The scan payload is wrapped in sentinels because WP-CLI, plugins and PHP
// notices all write to the same stdout; anything outside the markers is noise.
const (
	mediaSentinelStart = "<<<RDM-MEDIA-1>>>"
	mediaSentinelEnd   = "<<<END-RDM-MEDIA-1>>>"
)

// mediaScanPayload mirrors the JSON that media_scan.php emits.
type mediaScanPayload struct {
	Error string `json:"error"`

	Folders         []string `json:"folders"`
	UploadsPath     string   `json:"uploadsPath"`
	UploadsURL      string   `json:"uploadsUrl"`
	Multisite       bool     `json:"multisite"`
	TotalFiles      int      `json:"totalFiles"`
	TotalBytes      int64    `json:"totalBytes"`
	AttachmentCount int      `json:"attachmentCount"`
	ReferencedCount int      `json:"referencedCount"`

	ByClass     []domain.MediaClassTotals    `json:"byClass"`
	ByPeriod    []domain.MediaPeriodBucket   `json:"byPeriod"`
	ByExtension []domain.MediaExtTotals      `json:"byExtension"`
	Largest     []domain.MediaFileRow        `json:"largest"`
	Categories  []domain.MediaCategoryResult `json:"categories"`
	Detail      []domain.MediaFileRow        `json:"detail"`

	TablesScanned     []string       `json:"tablesScanned"`
	RowsScanned       map[string]int `json:"rowsScanned"`
	ThemeFilesScanned int            `json:"themeFilesScanned"`
	ReferenceScanRan  bool           `json:"referenceScanRan"`
	OffloadDetected   bool           `json:"offloadDetected"`
	Truncated         bool           `json:"truncated"`
	DurationMS        int64          `json:"durationMs"`
	Notes             []string       `json:"notes"`
}

// pakSentinel haalt het ingepakte blok tussen twee markeringen uit ruwe SSH-uitvoer.
// Alles wat de server eromheen printte (WP-CLI-notices, plugin-waarschuwingen) wordt
// weggegooid; ontbreekt het blok, dan gaat er een stukje uitvoer mee in de fout zodat
// de oorzaak zichtbaar blijft.
func pakSentinel(stdout, startMark, eindMark string) ([]byte, error) {
	start := strings.Index(stdout, startMark)
	end := strings.Index(stdout, eindMark)
	if start < 0 || end < start {
		return nil, fmt.Errorf("geen resultaat in de uitvoer: %s", knipUitvoer(stdout))
	}
	blob := strings.TrimSpace(stdout[start+len(startMark) : end])

	gz, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(blob), ""))
	if err != nil {
		return nil, fmt.Errorf("resultaat niet leesbaar (base64): %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("resultaat niet leesbaar (gzip): %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("resultaat uitpakken: %w", err)
	}
	return raw, nil
}

// parseMediaScanOutput pulls the scan payload out of raw SSH stdout.
func parseMediaScanOutput(stdout string) (mediaScanPayload, error) {
	raw, err := pakSentinel(stdout, mediaSentinelStart, mediaSentinelEnd)
	if err != nil {
		return mediaScanPayload{}, err
	}

	var p mediaScanPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return mediaScanPayload{}, fmt.Errorf("scanresultaat parsen: %w", err)
	}
	if p.Error != "" {
		return p, fmt.Errorf("scan op de server: %s", p.Error)
	}
	return p, nil
}

// knipUitvoer keeps an error message readable when a site dumps pages of notices.
func knipUitvoer(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(leeg)"
	}
	const max = 400
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// summary turns a payload into a stored scan. The detail rows are returned
// separately: they go to the scan's detail file, not into the summary.
func (p mediaScanPayload) summary(scanID, projectID, projectName, env string, at time.Time, duBytes int64) (domain.MediaScanSummary, []domain.MediaFileRow) {
	notes := p.Notes
	if !p.ReferenceScanRan {
		notes = append(notes, "referentiescan leverde niets op; \"geen referentie gevonden\" is daarom niet bepaald")
	}

	sum := domain.MediaScanSummary{
		ID:              scanID,
		ProjectID:       projectID,
		ProjectName:     projectName,
		Environment:     env,
		ScannedAt:       at,
		DurationMS:      p.DurationMS,
		TotalFiles:      p.TotalFiles,
		TotalBytes:      p.TotalBytes,
		DiskUsageBytes:  duBytes,
		AttachmentCount: p.AttachmentCount,
		ReferencedCount: p.ReferencedCount,
		ByClass:         p.ByClass,
		ByPeriod:        p.ByPeriod,
		Largest:         p.Largest,
		Categories:      p.Categories,
		Scope: domain.MediaScanScope{
			Folders:               p.Folders,
			UploadsPath:           p.UploadsPath,
			UploadsURL:            p.UploadsURL,
			Multisite:             p.Multisite,
			TablesScanned:         p.TablesScanned,
			RowsScanned:           p.RowsScanned,
			ReferenceScanComplete: p.ReferenceScanRan,
			ThemeFilesScanned:     p.ThemeFilesScanned,
			RevisionsAsProof:      false,
			OffloadDetected:       p.OffloadDetected,
			Truncated:             p.Truncated,
			Notes:                 notes,
		},
	}
	// Categorieën zonder harde grond mogen nooit als feit doorgaan; de PHP-kant
	// zet dit al, maar de UI leunt erop, dus hier vastgezet.
	for i := range sum.Categories {
		sum.Categories[i].Hard = sum.Categories[i].Category.Hard()
	}
	return sum, p.Detail
}
