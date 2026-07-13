package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestAuthor(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body: `{"content":[{"type":"tool_use","name":"emit_steps","input":{"steps":[
			{"action":"navigate","target":"/"},
			{"action":"click","target":"Cookies accepteren"},
			{"action":"type","target":"E-mail","value":"test@example.com"}
		]}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 1024}
	steps, err := c.Author(context.Background(), "Ga naar home, accepteer cookies, vul e-mail")
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	if len(steps) != 3 || steps[0].Action != domain.StepNavigate || steps[2].Value != "test@example.com" {
		t.Fatalf("unexpected steps: %+v", steps)
	}
	if !strings.Contains(string(f.lastBody), ModelSonnet) {
		t.Errorf("expected sonnet model, body: %s", f.lastBody)
	}
}

func TestAuthorRejectsInvalidAction(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body:   `{"content":[{"type":"tool_use","name":"emit_steps","input":{"steps":[{"action":"teleport","target":"/"}]}}]}`,
	}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 1024}
	if _, err := c.Author(context.Background(), "x"); err == nil {
		t.Fatal("expected error for invalid action")
	}
}
