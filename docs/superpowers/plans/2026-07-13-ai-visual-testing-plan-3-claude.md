# AI Visual Testing — Plan 3: adapters/claude (Anthropic) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een Go-adapter `internal/adapters/claude` die de Anthropic Messages API aanroept voor drie taken: flows schrijven (natuurlijke taal → stappen), screenshots vergelijken (gecategoriseerde findings + ernst) en self-heal (accessibility-tree → CSS-selector), met de model-routing uit Plan 1 aangesloten.

**Architecture:** Eén thin `Client` met een `HTTPDoer`-seam (interface met alleen `Do`), zodat alle tests met vaste nep-responses draaien — geen netwerk, geen API-key in CI. Structured output via Anthropic **tool-use** (geforceerd `tool_choice`): elke taak definieert een JSON-schema, en de client haalt het `tool_use.input` uit het antwoord. De drie publieke methodes (`Author`, `Compare`, `Heal`) kiezen hun model via `domain.ChooseModelTier` (Plan 1) en parsen naar domain-types (`domain.Step`, `domain.Finding`).

**Tech Stack:** Go stdlib (`net/http`, `encoding/json`, `encoding/base64`). Geen externe dependencies. Anthropic Messages API (`POST https://api.anthropic.com/v1/messages`).

**Depends on:** Plan 1 (`domain.Step`, `domain.Finding`, `domain.ModelTier`/`RoutingInput`/`ChooseModelTier`, `StepType.Valid`). **Branch:** blijf op `feature/ai-visual-testing`.

**Spec:** [../specs/2026-07-13-ai-visual-testing-design.md](../specs/2026-07-13-ai-visual-testing-design.md) · **Roadmap:** [2026-07-13-ai-visual-testing-roadmap.md](2026-07-13-ai-visual-testing-roadmap.md)

---

## File Structure

- Create: `internal/adapters/claude/models.go` — API-request/response-types + `tierToModel`.
- Create: `internal/adapters/claude/client.go` — `Client`, `HTTPDoer`, low-level `toolCall`.
- Create: `internal/adapters/claude/client_test.go` — `tierToModel` + `toolCall` (headers/body/parse/errors) via nep-`HTTPDoer`.
- Create: `internal/adapters/claude/author.go` + `author_test.go` — NL → `[]domain.Step`.
- Create: `internal/adapters/claude/compare.go` + `compare_test.go` — screenshots → `[]domain.Finding`.
- Create: `internal/adapters/claude/heal.go` + `heal_test.go` — a11y-tree → selector.

Geen service-wiring/bindings hier (Plan 4). Geen echte API-calls in CI.

---

## Task 1: API-types, tierToModel, en low-level toolCall

**Files:**
- Create: `internal/adapters/claude/models.go`
- Create: `internal/adapters/claude/client.go`
- Test: `internal/adapters/claude/client_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/adapters/claude/client_test.go`:
```go
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
		{domain.ModelTier("weird"), ModelSonnet}, // default
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
	// Verify request headers and that the chosen model + tool_choice made it into the body.
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
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/adapters/claude/ -v`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Schrijf `internal/adapters/claude/models.go`**

```go
// Package claude calls the Anthropic Messages API for authoring flow steps,
// comparing screenshots, and self-healing selectors.
package claude

import "github.com/rdm/sites-tool/internal/domain"

// Current Anthropic model ids per tier. Centralised so an upgrade is one edit.
const (
	ModelHaiku  = "claude-haiku-4-5-20251001"
	ModelSonnet = "claude-sonnet-5"
	ModelOpus   = "claude-opus-4-8"
)

// tierToModel maps a logical tier (from domain.ChooseModelTier) to a model id.
func tierToModel(t domain.ModelTier) string {
	switch t {
	case domain.TierHaiku:
		return ModelHaiku
	case domain.TierOpus:
		return ModelOpus
	default:
		return ModelSonnet
	}
}

// --- Anthropic Messages API wire types (minimal subset) ---

type imageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png"
	Data      string `json:"data"`
}

type contentBlock struct {
	Type   string       `json:"type"` // "text" | "image"
	Text   string       `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type toolChoice struct {
	Type string `json:"type"` // "tool"
	Name string `json:"name"`
}

type apiRequest struct {
	Model      string      `json:"model"`
	MaxTokens  int         `json:"max_tokens"`
	System     string      `json:"system,omitempty"`
	Messages   []message   `json:"messages"`
	Tools      []tool      `json:"tools,omitempty"`
	ToolChoice *toolChoice `json:"tool_choice,omitempty"`
}

type apiResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
```

> NOTE: `models.go` uses `json.RawMessage`, so add `import "encoding/json"` — make the import block:
> ```go
> import (
> 	"encoding/json"
>
> 	"github.com/rdm/sites-tool/internal/domain"
> )
> ```

- [ ] **Step 4: Schrijf `internal/adapters/claude/client.go`**

```go
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rdm/sites-tool/internal/domain"
)

const (
	apiURL        = "https://api.anthropic.com/v1/messages"
	apiVersion    = "2023-06-01"
	defaultMaxTok = 1024
)

// HTTPDoer is the subset of *http.Client the adapter needs (test seam).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client calls the Anthropic Messages API.
type Client struct {
	APIKey    string
	HTTP      HTTPDoer
	ModelFor  func(domain.ModelTier) string
	MaxTokens int
}

// NewClient returns a Client using the default HTTP client and tier mapping.
func NewClient(apiKey string) *Client {
	return &Client{APIKey: apiKey, HTTP: http.DefaultClient, ModelFor: tierToModel, MaxTokens: defaultMaxTok}
}

// toolCall sends a forced-tool request and returns the tool_use input JSON.
func (c *Client) toolCall(ctx context.Context, tier domain.ModelTier, system string, content []contentBlock, t tool) (json.RawMessage, error) {
	reqBody := apiRequest{
		Model:      c.ModelFor(tier),
		MaxTokens:  c.MaxTokens,
		System:     system,
		Messages:   []message{{Role: "user", Content: content}},
		Tools:      []tool{t},
		ToolChoice: &toolChoice{Type: "tool", Name: t.Name},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(data))
	}
	var ar apiResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if ar.Error != nil {
		return nil, fmt.Errorf("anthropic error: %s", ar.Error.Message)
	}
	for _, b := range ar.Content {
		if b.Type == "tool_use" {
			return b.Input, nil
		}
	}
	return nil, fmt.Errorf("geen tool_use-blok in antwoord")
}
```

- [ ] **Step 5: Run de test — moet slagen**

Run: `go test ./internal/adapters/claude/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/claude/models.go internal/adapters/claude/client.go internal/adapters/claude/client_test.go
git commit -m "feat(claude): Anthropic client with forced tool-use and test seam"
```

---

## Task 2: Author (natuurlijke taal → stappen)

**Files:**
- Create: `internal/adapters/claude/author.go`
- Test: `internal/adapters/claude/author_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/adapters/claude/author_test.go`:
```go
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
	// authoring uses the sonnet tier
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
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/adapters/claude/ -run TestAuthor -v`
Expected: FAIL — undefined `Author`.

- [ ] **Step 3: Schrijf `internal/adapters/claude/author.go`**

```go
package claude

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rdm/sites-tool/internal/domain"
)

const authorSystem = `Je zet een testscenario in natuurlijke taal om in concrete stappen voor ` +
	`een browsertest van een WordPress-site. Gebruik alleen de acties: navigate, click, type, ` +
	`login, wait, assert. Voor click/type/assert beschrijf je 'target' als de zichtbare ` +
	`tekst of het label. Voor 'type' vul je ook 'value'. Laat 'selector' leeg. ` +
	`Geef uitsluitend het gereedschap terug.`

var authorTool = tool{
	Name:        "emit_steps",
	Description: "Geef de flow-stappen terug.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"steps":{"type":"array","items":{"type":"object",
			"properties":{
				"action":{"type":"string","enum":["navigate","click","type","login","wait","assert"]},
				"target":{"type":"string"},
				"value":{"type":"string"},
				"selector":{"type":"string"}
			},"required":["action"]}}},
		"required":["steps"]}`),
}

// Author converts a natural-language scenario into validated flow steps.
func (c *Client) Author(ctx context.Context, description string) ([]domain.Step, error) {
	tier := domain.ChooseModelTier(domain.RoutingInput{Task: domain.TaskAuthor})
	in, err := c.toolCall(ctx, tier, authorSystem,
		[]contentBlock{{Type: "text", Text: description}}, authorTool)
	if err != nil {
		return nil, err
	}
	var out struct {
		Steps []domain.Step `json:"steps"`
	}
	if err := json.Unmarshal(in, &out); err != nil {
		return nil, fmt.Errorf("parse steps: %w", err)
	}
	for i, s := range out.Steps {
		if !s.Action.Valid() {
			return nil, fmt.Errorf("stap %d: ongeldige actie %q", i, s.Action)
		}
	}
	return out.Steps, nil
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/adapters/claude/ -run TestAuthor -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/claude/author.go internal/adapters/claude/author_test.go
git commit -m "feat(claude): author flow steps from natural language"
```

---

## Task 3: Compare (screenshots → gecategoriseerde findings)

**Files:**
- Create: `internal/adapters/claude/compare.go`
- Test: `internal/adapters/claude/compare_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/adapters/claude/compare_test.go`:
```go
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
	// both screenshots must be included as base64 image blocks
	if strings.Count(body, `"type":"image"`) != 2 {
		t.Errorf("expected 2 image blocks, body: %s", body)
	}
	// default compare tier is sonnet
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
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/adapters/claude/ -run TestCompare -v`
Expected: FAIL — undefined `Compare`.

- [ ] **Step 3: Schrijf `internal/adapters/claude/compare.go`**

```go
package claude

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/rdm/sites-tool/internal/domain"
)

const compareSystem = `Je vergelijkt twee screenshots van dezelfde pagina op twee omgevingen ` +
	`(release vs update). De omgevingen hebben ANDERE inhoud/data (andere teksten, afbeeldingen, ` +
	`aantallen) — dat is normaal en GEEN fout. Rapporteer alleen echte afwijkingen en ` +
	`categoriseer elk verschil:
- layout-break: kapotte/overlappende layout, verschoven of afgebroken secties
- missing-element: een element/sectie ontbreekt op de update-kant
- styling: kleur/font/spacing-afwijking
- content-only: puur inhoud/data-verschil (bijna altijd ernst 'laag', negeerbaar)
Ernst 'hoog' voor layout-break/missing-element die de pagina breken, anders 'laag'. ` +
	`Geef uitsluitend het gereedschap terug.`

var compareTool = tool{
	Name:        "emit_findings",
	Description: "Geef de gecategoriseerde verschillen terug.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"findings":{"type":"array","items":{"type":"object",
			"properties":{
				"category":{"type":"string","enum":["layout-break","missing-element","styling","content-only"]},
				"severity":{"type":"string","enum":["hoog","laag"]},
				"where":{"type":"string"},
				"description":{"type":"string"}
			},"required":["category","severity","description"]}}},
		"required":["findings"]}`),
}

// Compare returns categorised visual differences between two PNG screenshots.
// stepDesc gives Claude context; highImpact escalates the model tier to opus.
func (c *Client) Compare(ctx context.Context, baselinePNG, updatePNG []byte, stepDesc string, highImpact bool) ([]domain.Finding, error) {
	tier := domain.ChooseModelTier(domain.RoutingInput{Task: domain.TaskCompare, HighImpact: highImpact})
	content := []contentBlock{
		{Type: "text", Text: "Context: " + stepDesc + "\nEerste afbeelding = release (baseline), tweede = update."},
		{Type: "image", Source: &imageSource{Type: "base64", MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(baselinePNG)}},
		{Type: "image", Source: &imageSource{Type: "base64", MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(updatePNG)}},
	}
	in, err := c.toolCall(ctx, tier, compareSystem, content, compareTool)
	if err != nil {
		return nil, err
	}
	var out struct {
		Findings []domain.Finding `json:"findings"`
	}
	if err := json.Unmarshal(in, &out); err != nil {
		return nil, fmt.Errorf("parse findings: %w", err)
	}
	return out.Findings, nil
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/adapters/claude/ -run TestCompare -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/claude/compare.go internal/adapters/claude/compare_test.go
git commit -m "feat(claude): compare screenshots into categorised findings"
```

---

## Task 4: Heal (accessibility-tree → selector)

**Files:**
- Create: `internal/adapters/claude/heal.go`
- Test: `internal/adapters/claude/heal_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/adapters/claude/heal_test.go`:
```go
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
	// heal uses the sonnet tier
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
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/adapters/claude/ -run TestHeal -v`
Expected: FAIL — undefined `Heal`.

- [ ] **Step 3: Schrijf `internal/adapters/claude/heal.go`**

```go
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
)

const healSystem = `Een teststap kon een element niet vinden. Op basis van de accessibility-tree ` +
	`(YAML) en de beschrijving van het gezochte element geef je één robuuste CSS-selector terug ` +
	`die dat element aanwijst. Geef uitsluitend het gereedschap terug.`

var healTool = tool{
	Name:        "emit_selector",
	Description: "Geef de CSS-selector terug.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"selector":{"type":"string"}},"required":["selector"]}`),
}

// Heal returns a CSS selector for target, derived from the page accessibility
// snapshot (YAML from Playwright's ariaSnapshot).
func (c *Client) Heal(ctx context.Context, ariaSnapshot, target string) (string, error) {
	tier := domain.ChooseModelTier(domain.RoutingInput{Task: domain.TaskHeal})
	in, err := c.toolCall(ctx, tier, healSystem,
		[]contentBlock{{Type: "text", Text: "Gezocht element: " + target + "\n\nAccessibility-tree:\n" + ariaSnapshot}},
		healTool)
	if err != nil {
		return "", err
	}
	var out struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(in, &out); err != nil {
		return "", fmt.Errorf("parse selector: %w", err)
	}
	if strings.TrimSpace(out.Selector) == "" {
		return "", fmt.Errorf("lege selector van Claude")
	}
	return out.Selector, nil
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/adapters/claude/ -run TestHeal -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/claude/heal.go internal/adapters/claude/heal_test.go
git commit -m "feat(claude): self-heal selectors from accessibility snapshot"
```

---

## Task 5: Volledige verificatie

- [ ] **Step 1: Alle claude-tests met race**

Run: `go test -race ./internal/adapters/claude/`
Expected: ok

- [ ] **Step 2: Vet + format**

Run: `go vet ./internal/adapters/claude/ && gofmt -l internal/adapters/claude/`
Expected: schoon + lege output.

- [ ] **Step 3: Bevestig dat alles bouwt**

Run: `go build ./internal/...`
Expected: geen fouten.

- [ ] **Step 4: Commit (indien nodig)**

```bash
git add -A && git commit -m "chore(claude): vet/format clean" || echo "niets te committen"
```

---

## Aannames & later

- **Model-ids** (`ModelHaiku`/`ModelSonnet`/`ModelOpus`) staan centraal in `models.go`; pas ze aan als Anthropic nieuwe versies uitbrengt.
- **API-key** wordt in Plan 4 uit de Keychain gehaald (`config.ResolveSecret`) en aan `NewClient` gegeven; hier niet.
- **`max_tokens`** staat op 1024 (default); Compare kan meer nodig hebben bij veel findings — in Plan 4 desnoods verhogen.
- **Echte API-verificatie** (één live call met een echte key) is optioneel en gebeurt handmatig; niet in CI.
- **Ambiguity-escalatie** (`RoutingInput.Ambiguous`): de triage→escalatie-lus wordt in Plan 4 (`test_service`) bedraad; `Compare` ondersteunt nu al `highImpact`.

## Self-Review — dekking t.o.v. spec

- **Authoring NL→stappen** → Task 2 (`Author`). ✓
- **Visuele vergelijking + categorisatie + ernst, inhoud/data genegeerd** → Task 3 (`Compare` + `compareSystem`-prompt). ✓
- **Self-heal (a11y-tree → selector)** → Task 4 (`Heal`). ✓
- **Model-routing aangesloten (H/S/O, escalatie)** → Task 1 (`tierToModel`) + elke methode via `domain.ChooseModelTier`; opus-escalatie getest in Task 3. ✓
- **Testbaar zonder netwerk/key** → Task 1 (`HTTPDoer`-seam + `fakeDoer`). ✓
- **Robuuste structured output** → geforceerde tool-use + parse van `tool_use.input`. ✓

**Placeholder-scan:** geen TBD/TODO; alle stappen bevatten volledige code. ✓
**Type-consistentie:** `Client`/`HTTPDoer`/`contentBlock`/`tool`/`toolChoice`/`apiRequest`/`apiResponse` en de methodes `Author`/`Compare`/`Heal` consistent; `tierToModel`-constanten (`ModelHaiku/Sonnet/Opus`) consistent gebruikt in code + tests. ✓
