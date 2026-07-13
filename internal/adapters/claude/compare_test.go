package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestCompare(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body: `{"content":[{"type":"tool_use","name":"emit_findings","input":{"findings":[
			{"category":"layout-break","severity":"hoog","where":"header","description":"overlap"},
			{"category":"content-only","severity":"laag","where":"body","description":"andere tekst"}
		]}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 1024}
	findings, err := c.Compare(context.Background(), []byte("PNGBASE"), []byte("PNGUPD"), "stap 0 /", false)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(findings) != 2 || findings[0].Category != domain.CatLayoutBreak || findings[0].Severity != domain.SeverityHigh {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	body := string(f.lastBody)
	if strings.Count(body, `"type":"image"`) != 2 {
		t.Errorf("expected 2 image blocks, body: %s", body)
	}
	if !strings.Contains(body, ModelSonnet) {
		t.Errorf("expected sonnet, body: %s", body)
	}
}

func TestCompareHighImpactEscalatesToOpus(t *testing.T) {
	f := &fakeDoer{status: 200, body: `{"content":[{"type":"tool_use","name":"emit_findings","input":{"findings":[]}}]}`}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 1024}
	if _, err := c.Compare(context.Background(), []byte("a"), []byte("b"), "ctx", true); err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !strings.Contains(string(f.lastBody), ModelOpus) {
		t.Errorf("high impact should use opus, body: %s", f.lastBody)
	}
}
