package claude

import (
	"context"
	"strings"
	"testing"
)

func TestHeal(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body:   `{"content":[{"type":"tool_use","name":"emit_selector","input":{"selector":"button.accept"}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 1024}
	sel, err := c.Heal(context.Background(), "- button \"Accepteren\"", "Cookies accepteren")
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if sel != "button.accept" {
		t.Errorf("selector = %q", sel)
	}
	if !strings.Contains(string(f.lastBody), ModelSonnet) {
		t.Errorf("expected sonnet, body: %s", f.lastBody)
	}
}

func TestHealEmptySelectorErrors(t *testing.T) {
	f := &fakeDoer{status: 200, body: `{"content":[{"type":"tool_use","name":"emit_selector","input":{"selector":""}}]}`}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 1024}
	if _, err := c.Heal(context.Background(), "tree", "target"); err == nil {
		t.Fatal("expected error on empty selector")
	}
}
