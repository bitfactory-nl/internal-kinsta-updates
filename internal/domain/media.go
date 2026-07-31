package domain

import "time"

// MediaCategory groups a media finding by what kind of claim it makes. The three
// differ fundamentally in how much they can be trusted, so they are never merged
// into one "cleanup" list.
type MediaCategory string

const (
	// MediaOrphanFile is a file on disk that the media library does not know.
	// Hard fact.
	MediaOrphanFile MediaCategory = "orphan_file"
	// MediaMissingFile is a library entry whose file is gone. Hard fact, unless an
	// offload plugin moved the file off the server.
	MediaMissingFile MediaCategory = "missing_file"
	// MediaUnreferenced is a library entry with no reference found anywhere.
	// Heuristic: absence of evidence, not evidence of absence.
	MediaUnreferenced MediaCategory = "unreferenced"
)

// Hard reports whether a category states a verifiable fact rather than a guess.
func (c MediaCategory) Hard() bool {
	return c == MediaOrphanFile || c == MediaMissingFile
}

// MediaFileClass says what produced a file, which is what makes a folder size
// explainable: generated thumbnails and plugin caches are not media.
type MediaFileClass string

const (
	MediaClassOriginal     MediaFileClass = "original"
	MediaClassGenerated    MediaFileClass = "generated"     // -300x200 en soortgelijke formaten
	MediaClassScaled       MediaFileClass = "scaled"        // -scaled naast een bewaard origineel
	MediaClassNextGen      MediaFileClass = "nextgen"       // .webp/.avif naast een bekend bestand
	MediaClassEditorBackup MediaFileClass = "editor_backup" // -e<timestamp> uit de afbeeldingseditor
	MediaClassSystem       MediaFileClass = "system"        // caches, exports, backup-archieven
	MediaClassUnknown      MediaFileClass = "unknown"
)

// MediaEvidence names a place where a reference to an attachment was found, so the
// UI can show why something counts as in use.
type MediaEvidence string

const (
	EvidenceContent      MediaEvidence = "content"
	EvidenceMeta         MediaEvidence = "meta"
	EvidenceACF          MediaEvidence = "acf"
	EvidenceOptions      MediaEvidence = "options"
	EvidenceTermMeta     MediaEvidence = "termmeta"
	EvidenceUserMeta     MediaEvidence = "usermeta"
	EvidenceThemeCode    MediaEvidence = "theme"
	EvidenceExtraTable   MediaEvidence = "extra_table"
	EvidenceRevisionOnly MediaEvidence = "revision_only" // alleen in een oude revisie
	EvidenceFilenameOnly MediaEvidence = "filename_only" // losse bestandsnaam, zwak bewijs
)

// MediaFileRow is one file or attachment in a category listing. Paths are relative
// to the uploads directory; ModifiedAt is a unix timestamp because it comes
// straight from the server's filesystem.
type MediaFileRow struct {
	Path         string          `json:"path"`
	Bytes        int64           `json:"bytes"`
	ModifiedAt   int64           `json:"modifiedAt"`
	Class        MediaFileClass  `json:"class"`
	Category     MediaCategory   `json:"category,omitempty"`
	AttachmentID int             `json:"attachmentId,omitempty"`
	Title        string          `json:"title,omitempty"`
	MimeType     string          `json:"mimeType,omitempty"`
	Evidence     []MediaEvidence `json:"evidence,omitempty"`
}

// MediaClassTotals aggregates one file class.
type MediaClassTotals struct {
	Class MediaFileClass `json:"class"`
	Files int            `json:"files"`
	Bytes int64          `json:"bytes"`
}

// MediaPeriodBucket aggregates one uploads subfolder, usually a year/month pair
// ("2024/05") or a named bucket for everything outside that layout.
type MediaPeriodBucket struct {
	Period string `json:"period"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// MediaCategoryResult is the outcome for one category. Samples is capped; the full
// list lives in the scan's detail file.
type MediaCategoryResult struct {
	Category  MediaCategory  `json:"category"`
	Hard      bool           `json:"hard"`
	Files     int            `json:"files"`
	Bytes     int64          `json:"bytes"`
	Samples   []MediaFileRow `json:"samples"`
	Truncated bool           `json:"truncated"`
}

// MediaScanScope records what the scan looked at and what it could not see. It is
// shown alongside every result: without it, "no reference found" reads as
// "unused", which is exactly the mistake that deletes something in use.
type MediaScanScope struct {
	// Folders limits a scan to these prefixes inside uploads (empty = everything).
	// A scoped scan says nothing about the folders it skipped, which is why it is
	// recorded alongside the numbers rather than inferred from them.
	Folders           []string `json:"folders,omitempty"`
	UploadsPath       string   `json:"uploadsPath"`
	UploadsURL        string   `json:"uploadsUrl"`
	Multisite         bool     `json:"multisite"`
	TablesScanned     []string `json:"tablesScanned"`
	ThemeFilesScanned int      `json:"themeFilesScanned"`
	RevisionsAsProof  bool     `json:"revisionsAsProof"`
	OffloadDetected   bool     `json:"offloadDetected"`
	Degraded          bool     `json:"degraded"`  // zonder plugins/thema gebootstrapt
	Truncated         bool     `json:"truncated"` // eigen tijdsbudget geraakt
	Notes             []string `json:"notes,omitempty"`
}

// MediaScanSummary is one completed scan of one environment.
type MediaScanSummary struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	ProjectName string    `json:"projectName"`
	Environment string    `json:"environment"`
	ScannedAt   time.Time `json:"scannedAt"`
	DurationMS  int64     `json:"durationMs"`

	TotalFiles      int   `json:"totalFiles"`
	TotalBytes      int64 `json:"totalBytes"`
	DiskUsageBytes  int64 `json:"diskUsageBytes"` // uit du, als controle op de som
	AttachmentCount int   `json:"attachmentCount"`
	ReferencedCount int   `json:"referencedCount"`

	ByClass    []MediaClassTotals    `json:"byClass"`
	ByPeriod   []MediaPeriodBucket   `json:"byPeriod"`
	Largest    []MediaFileRow        `json:"largest"`
	Categories []MediaCategoryResult `json:"categories"`

	Scope MediaScanScope `json:"scope"`
}
