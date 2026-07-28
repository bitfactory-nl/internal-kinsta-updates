package wordfence

import (
	"testing"
	"time"
)

const sampleFeed = `{
  "abc-123": {
    "id": "abc-123",
    "title": "Contact Form 7 <= 5.3.1 - File Upload",
    "cve": "CVE-2020-1234",
    "cve_link": "https://www.cve.org/CVERecord?id=CVE-2020-1234",
    "cvss": {"vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H", "score": 7.5, "rating": "High"},
    "researchers": ["Jane Doe", "John Smith"],
    "published": "2021-01-05T00:00:00.000Z",
    "updated": "2024-01-22 19:56:02",
    "software": [
      {
        "type": "plugin",
        "slug": "contact-form-7",
        "affected_versions": {
          "* - 5.3.1": {
            "from_version": "*",
            "from_inclusive": true,
            "to_version": "5.3.1",
            "to_inclusive": true
          }
        },
        "patched": true,
        "patched_versions": ["5.3.2"]
      }
    ]
  }
}`

func TestParseFeed(t *testing.T) {
	vulns, err := ParseFeed([]byte(sampleFeed))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(vulns) != 1 {
		t.Fatalf("want 1 vuln, got %d", len(vulns))
	}
	v := vulns[0]
	if v.CVE != "CVE-2020-1234" || v.CVSSScore != 7.5 {
		t.Errorf("meta mismatch: %+v", v)
	}
	if v.CVELink != "https://www.cve.org/CVERecord?id=CVE-2020-1234" {
		t.Errorf("cveLink mismatch: %+v", v)
	}
	if v.CVSSVector != "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H" {
		t.Errorf("cvssVector mismatch: %+v", v)
	}
	if len(v.Researchers) != 2 || v.Researchers[0] != "Jane Doe" || v.Researchers[1] != "John Smith" {
		t.Errorf("researchers mismatch: %+v", v)
	}
	// The fixture uses the RFC3339 shape as a compatibility fallback; real
	// feed data uses "YYYY-MM-DD HH:MM:SS" (see TestParseFeedRealDateFormat).
	wantPublished := time.Date(2021, 1, 5, 0, 0, 0, 0, time.UTC)
	if !v.Published.Equal(wantPublished) {
		t.Errorf("published = %v, want %v", v.Published, wantPublished)
	}
	wantUpdated := time.Date(2024, 1, 22, 19, 56, 2, 0, time.UTC)
	if !v.Updated.Equal(wantUpdated) {
		t.Errorf("updated = %v, want %v", v.Updated, wantUpdated)
	}
	if len(v.Software) != 1 {
		t.Fatalf("want 1 software, got %d", len(v.Software))
	}
	s := v.Software[0]
	if s.Type != "plugin" || s.Slug != "contact-form-7" {
		t.Errorf("software mismatch: %+v", s)
	}
	if s.AffectedFrom != "*" || s.AffectedTo != "5.3.1" || !s.ToInclusive {
		t.Errorf("range mismatch: %+v", s)
	}
	if len(s.PatchedVersions) != 1 || s.PatchedVersions[0] != "5.3.2" {
		t.Errorf("patched mismatch: %+v", s)
	}
}

// The real production feed (~/.config/rdm/wordfence-production.json) sends
// published/updated as "YYYY-MM-DD HH:MM:SS" — no "T", no zone — which is NOT
// RFC3339. Verified directly against the cached feed. ParseFeed must handle
// this shape (with an RFC3339 fallback for the older fixture format above).
func TestParseFeedRealDateFormat(t *testing.T) {
	feed := `{"x":{"id":"x","published":"2022-09-09 00:00:00","updated":"2024-01-22 19:56:02"}}`
	vulns, err := ParseFeed([]byte(feed))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(vulns) != 1 {
		t.Fatalf("want 1 vuln, got %d", len(vulns))
	}
	v := vulns[0]
	wantPublished := time.Date(2022, 9, 9, 0, 0, 0, 0, time.UTC)
	wantUpdated := time.Date(2024, 1, 22, 19, 56, 2, 0, time.UTC)
	if !v.Published.Equal(wantPublished) {
		t.Errorf("published = %v, want %v", v.Published, wantPublished)
	}
	if !v.Updated.Equal(wantUpdated) {
		t.Errorf("updated = %v, want %v", v.Updated, wantUpdated)
	}
}

func TestParseFeedResearchers(t *testing.T) {
	cases := map[string]struct {
		researchersJSON string
		want            []string
	}{
		"filled": {`["Jeremie Amsellem", "Anna Oster"]`, []string{"Jeremie Amsellem", "Anna Oster"}},
		"empty":  {`[]`, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			feed := `{"x":{"id":"x","researchers":` + c.researchersJSON + `}}`
			vulns, err := ParseFeed([]byte(feed))
			if err != nil {
				t.Fatalf("ParseFeed: %v", err)
			}
			if len(vulns) != 1 {
				t.Fatalf("want 1 vuln, got %d", len(vulns))
			}
			got := vulns[0].Researchers
			if len(got) != len(c.want) {
				t.Fatalf("researchers = %+v, want %+v", got, c.want)
			}
			for i, name := range c.want {
				if got[i] != name {
					t.Errorf("researchers[%d] = %q, want %q", i, got[i], name)
				}
			}
		})
	}
}

// A record with no CVE assigned yet sends "cve": null and "cve_link": null
// (confirmed against the cached production feed — ~2831 of 37805 records).
func TestParseFeedCVENull(t *testing.T) {
	feed := `{"x":{"id":"x","title":"Some Plugin - Unauthenticated XSS","cve":null,"cve_link":null,"cvss":{"score":5.5,"rating":"Medium"}}}`
	vulns, err := ParseFeed([]byte(feed))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(vulns) != 1 {
		t.Fatalf("want 1 vuln, got %d", len(vulns))
	}
	v := vulns[0]
	if v.CVE != "" || v.CVELink != "" {
		t.Errorf("expected empty CVE/CVELink for null input, got %+v", v)
	}
}

func TestParseFeedMissingUpdated(t *testing.T) {
	feed := `{"x":{"id":"x","published":"2022-09-09 00:00:00"}}`
	vulns, err := ParseFeed([]byte(feed))
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(vulns) != 1 {
		t.Fatalf("want 1 vuln, got %d", len(vulns))
	}
	v := vulns[0]
	if !v.Updated.IsZero() {
		t.Errorf("expected zero Updated when the field is absent, got %v", v.Updated)
	}
	if v.Published.IsZero() {
		t.Errorf("expected Published to still parse, got zero")
	}
}

// The real Wordfence feed sends cvss.score as a JSON number, but be tolerant
// of a quoted string or null so a single odd record can't fail the whole feed.
func TestParseFeedScoreFormats(t *testing.T) {
	cases := map[string]struct {
		scoreJSON string
		want      float64
	}{
		"number": {"7.5", 7.5},
		"string": {`"9.8"`, 9.8},
		"null":   {"null", 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			feed := `{"x":{"id":"x","cvss":{"score":` + c.scoreJSON + `,"rating":"X"}}}`
			vulns, err := ParseFeed([]byte(feed))
			if err != nil {
				t.Fatalf("ParseFeed: %v", err)
			}
			if len(vulns) != 1 {
				t.Fatalf("want 1 vuln, got %d", len(vulns))
			}
			if vulns[0].CVSSScore != c.want {
				t.Errorf("score = %v, want %v", vulns[0].CVSSScore, c.want)
			}
		})
	}
}

func TestParseFeedEmpty(t *testing.T) {
	vulns, err := ParseFeed([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseFeed empty: %v", err)
	}
	if len(vulns) != 0 {
		t.Errorf("want 0, got %d", len(vulns))
	}
}
