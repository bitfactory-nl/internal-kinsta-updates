package wordfence

import "testing"

const sampleFeed = `{
  "abc-123": {
    "id": "abc-123",
    "title": "Contact Form 7 <= 5.3.1 - File Upload",
    "cve": "CVE-2020-1234",
    "cvss": {"score": "7.5", "rating": "High"},
    "published": "2021-01-05T00:00:00.000Z",
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

func TestParseFeedEmpty(t *testing.T) {
	vulns, err := ParseFeed([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseFeed empty: %v", err)
	}
	if len(vulns) != 0 {
		t.Errorf("want 0, got %d", len(vulns))
	}
}
