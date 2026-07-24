// internal/domain/wordfence.go
package domain

import "time"

// Vulnerability is one Wordfence vulnerability record (plugin-relevant subset).
type Vulnerability struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	CVE       string             `json:"cve"`
	CVSSScore float64            `json:"cvssScore"`
	Severity  string             `json:"severity"`
	Published time.Time          `json:"published"`
	Software  []AffectedSoftware `json:"software"`
}

// AffectedSoftware is one affected plugin/theme entry with a version range.
type AffectedSoftware struct {
	Type            string   `json:"type"` // plugin | theme | core
	Slug            string   `json:"slug"`
	AffectedFrom    string   `json:"affectedFrom"` // "*" or version; "" = unbounded
	FromInclusive   bool     `json:"fromInclusive"`
	AffectedTo      string   `json:"affectedTo"` // "*" or version; "" = unbounded
	ToInclusive     bool     `json:"toInclusive"`
	PatchedVersions []string `json:"patchedVersions"`
}
