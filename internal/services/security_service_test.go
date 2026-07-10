package services

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseComposerAudit(t *testing.T) {
	data := []byte(`{
		"advisories": {
			"vendor/pkg": [
				{"title": "RCE in pkg", "cve": "CVE-2026-0001", "link": "https://example.com/a", "severity": "critical"},
				{"title": "XSS in pkg", "cve": "CVE-2026-0002", "link": "https://example.com/b", "severity": "medium"}
			]
		}
	}`)

	findings := parseComposerAudit(data)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	for _, f := range findings {
		if f.Source != "composer" || f.Package != "vendor/pkg" {
			t.Errorf("unexpected finding: %+v", f)
		}
	}
	// "medium" normaliseert naar "moderate"
	sortFindings(findings)
	if findings[0].Severity != "critical" || findings[1].Severity != "moderate" {
		t.Errorf("unexpected severities: %s, %s", findings[0].Severity, findings[1].Severity)
	}
}

func TestParseComposerAuditEmpty(t *testing.T) {
	if got := parseComposerAudit([]byte(`{"advisories": {}}`)); len(got) != 0 {
		t.Errorf("expected no findings, got %d", len(got))
	}
	if got := parseComposerAudit([]byte(`not json`)); got != nil {
		t.Errorf("expected nil on invalid json, got %v", got)
	}
}

func TestParseNpmAudit(t *testing.T) {
	data := []byte(`{
		"vulnerabilities": {
			"lodash": {
				"severity": "high",
				"via": [{"title": "Prototype Pollution", "url": "https://example.com/npm"}]
			},
			"transitive-only": {
				"severity": "low",
				"via": ["lodash"]
			}
		}
	}`)

	findings := parseNpmAudit(data)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (transitive gefilterd), got %d", len(findings))
	}
	f := findings[0]
	if f.Package != "lodash" || f.Severity != "high" || f.Title != "Prototype Pollution" {
		t.Errorf("unexpected finding: %+v", f)
	}
}

func TestParseAuditZip(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	cw, _ := w.Create("composer-audit.json")
	cw.Write([]byte(`{"advisories": {"a/b": [{"title": "t", "cve": "c", "link": "l", "severity": "high"}]}}`))
	nw, _ := w.Create("npm-audit.json")
	nw.Write([]byte(`{"vulnerabilities": {}}`))
	w.Close()

	result := &SecurityScanResult{Findings: []SecurityFinding{}}
	if err := parseAuditZip(buf.Bytes(), result); err != nil {
		t.Fatalf("parseAuditZip: %v", err)
	}
	if !result.HasComposerReport || !result.HasNpmReport {
		t.Errorf("expected both reports present: %+v", result)
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Findings))
	}
}

func TestGithubRemoteRe(t *testing.T) {
	cases := map[string]string{
		"git@github.com:bitfactory-nl/some-project.git": "bitfactory-nl/some-project",
		"https://github.com/bitfactory-nl/some-project": "bitfactory-nl/some-project",
		"https://github.com/org/repo.git":               "org/repo",
	}
	for url, want := range cases {
		m := githubRemoteRe.FindStringSubmatch(url)
		if m == nil || m[1] != want {
			t.Errorf("url %q: expected %q, got %v", url, want, m)
		}
	}
	if githubRemoteRe.FindStringSubmatch("https://gitlab.com/org/repo.git") != nil {
		t.Error("gitlab url should not match")
	}
}
