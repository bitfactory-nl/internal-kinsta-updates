// internal/adapters/wordfence/parse.go
package wordfence

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

type rawVuln struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CVE       string `json:"cve"`
	CVSS      struct {
		Score  string `json:"score"`
		Rating string `json:"rating"`
	} `json:"cvss"`
	Published string        `json:"published"`
	Software  []rawSoftware `json:"software"`
}

type rawSoftware struct {
	Type             string                    `json:"type"`
	Slug             string                    `json:"slug"`
	AffectedVersions map[string]rawVersionSpan `json:"affected_versions"`
	Patched          bool                      `json:"patched"`
	PatchedVersions  []string                  `json:"patched_versions"`
}

type rawVersionSpan struct {
	FromVersion   string `json:"from_version"`
	FromInclusive bool   `json:"from_inclusive"`
	ToVersion     string `json:"to_version"`
	ToInclusive   bool   `json:"to_inclusive"`
}

// ParseFeed converts the Wordfence v3 production feed (a JSON object keyed by
// vulnerability id) into domain.Vulnerability values. Non-plugin software is
// preserved but callers filter on Type == "plugin".
func ParseFeed(data []byte) ([]domain.Vulnerability, error) {
	var raw map[string]rawVuln
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.Vulnerability, 0, len(raw))
	for id, rv := range raw {
		v := domain.Vulnerability{
			ID:       firstNonEmpty(rv.ID, id),
			Title:    rv.Title,
			CVE:      rv.CVE,
			Severity: rv.CVSS.Rating,
		}
		if f, err := strconv.ParseFloat(rv.CVSS.Score, 64); err == nil {
			v.CVSSScore = f
		}
		if t, err := time.Parse(time.RFC3339, rv.Published); err == nil {
			v.Published = t
		}
		for _, rs := range rv.Software {
			for _, span := range rs.AffectedVersions {
				v.Software = append(v.Software, domain.AffectedSoftware{
					Type:            rs.Type,
					Slug:            rs.Slug,
					AffectedFrom:    span.FromVersion,
					FromInclusive:   span.FromInclusive,
					AffectedTo:      span.ToVersion,
					ToInclusive:     span.ToInclusive,
					PatchedVersions: rs.PatchedVersions,
				})
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
