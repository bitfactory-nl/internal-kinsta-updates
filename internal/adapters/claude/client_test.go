package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// fakeDoer is a test HTTPDoer: captures the request body and returns a canned response.
type fakeDoer struct {
	lastReq  *http.Request
	lastBody []byte
	status   int
	body     string
	err      error
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		f.lastBody, _ = io.ReadAll(req.Body)
	}
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

func TestTierToModel(t *testing.T) {
	cases := []struct {
		in   domain.ModelTier
		want string
	}{
		{domain.TierHaiku, ModelHaiku},
		{domain.TierSonnet, ModelSonnet},
		{domain.TierOpus, ModelOpus},
		{domain.ModelTier("weird"), ModelSonnet},
	}
	for _, c := range cases {
		if got := tierToModel(c.in); got != c.want {
			t.Errorf("tierToModel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToolCallSuccess(t *testing.T) {
	f := &fakeDoer{
		status: 200,
		body:   `{"content":[{"type":"tool_use","name":"emit","input":{"hello":"world"}}]}`,
	}
	c := &Client{APIKey: "sk-test", HTTP: f, ModelFor: tierToModel, MaxTokens: 512}
	in, err := c.toolCall(context.Background(), domain.TierSonnet, "sys",
		[]contentBlock{{Type: "text", Text: "hi"}},
		tool{Name: "emit", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatalf("toolCall: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(in, &got); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if got["hello"] != "world" {
		t.Errorf("input = %v", got)
	}
	if f.lastReq.Header.Get("x-api-key") != "sk-test" {
		t.Error("missing x-api-key header")
	}
	if f.lastReq.Header.Get("anthropic-version") == "" {
		t.Error("missing anthropic-version header")
	}
	body := string(f.lastBody)
	if !strings.Contains(body, ModelSonnet) {
		t.Errorf("model not in body: %s", body)
	}
	if !strings.Contains(body, `"tool_choice"`) {
		t.Errorf("tool_choice not forced: %s", body)
	}
}

func TestClientOverrideForcesModel(t *testing.T) {
	f := &fakeDoer{status: 200, body: `{"content":[{"type":"tool_use","name":"emit_steps","input":{"steps":[]}}]}`}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 1024, Override: domain.TierOpus}
	if _, err := c.Author(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(f.lastBody), ModelOpus) {
		t.Errorf("override should force opus, body: %s", f.lastBody)
	}
}

func TestToolCallHTTPError(t *testing.T) {
	f := &fakeDoer{status: 400, body: `{"error":{"type":"invalid_request_error","message":"bad"}}`}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 512}
	if _, err := c.toolCall(context.Background(), domain.TierSonnet, "", nil, tool{Name: "x"}); err == nil {
		t.Fatal("expected error on 400")
	}
}

func TestToolCallNoToolUse(t *testing.T) {
	f := &fakeDoer{status: 200, body: `{"content":[{"type":"text"}]}`}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 512}
	if _, err := c.toolCall(context.Background(), domain.TierSonnet, "", nil, tool{Name: "x"}); err == nil {
		t.Fatal("expected error when no tool_use block present")
	}
}

func TestToolCallTransportError(t *testing.T) {
	f := &fakeDoer{err: errors.New("boom")}
	c := &Client{APIKey: "k", HTTP: f, ModelFor: tierToModel, MaxTokens: 512}
	if _, err := c.toolCall(context.Background(), domain.TierSonnet, "", nil, tool{Name: "x"}); err == nil {
		t.Fatal("expected transport error")
	}
}
