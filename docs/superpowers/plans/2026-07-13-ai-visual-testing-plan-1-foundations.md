# AI Visual Testing — Plan 1: Fundamenten (Go, pure logic) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Leg de datatypes, config-uitbreiding en pure beslislogica vast waarop de latere plannen (sidecar, Claude-client, orchestratie, UI) bouwen.

**Architecture:** Alles in dit plan is pure Go zonder externe processen of netwerk: nieuwe domain-types, een uitbreiding van de projectconfig, het lezen/valideren van `.rdm/flows.yml`, de console/status-regressie-diff, en de model-routing-beslissing. Elk stuk is table-driven getest (`go test -race`).

**Tech Stack:** Go, `gopkg.in/yaml.v3` (al in gebruik), standaard `testing`. Geen nieuwe dependencies.

**Spec:** [../specs/2026-07-13-ai-visual-testing-design.md](../specs/2026-07-13-ai-visual-testing-design.md) · **Roadmap:** [2026-07-13-ai-visual-testing-roadmap.md](2026-07-13-ai-visual-testing-roadmap.md)

---

## File Structure

- Create: `internal/domain/testing.go` — alle test-engine datatypes + pure helpers (`Valid()`, `ResolveEnvURL`, `DiffRegressions`, `ChooseModelTier`).
- Create: `internal/domain/testing_test.go` — tests voor de domain-helpers.
- Modify: `internal/domain/project.go` — voeg `Testing *TestingCfg` + `TestingCfg`/`BasicAuth`/`TestAccount` toe.
- Create: `internal/config/flows.go` — `.rdm/flows.yml` lezen/schrijven/valideren.
- Create: `internal/config/flows_test.go` — tests voor de flow-file.
- Create: `internal/config/testing_access.go` — secrets resolven per omgeving.
- Create: `internal/config/testing_access_test.go` — tests voor de access-resolver.

Geen wijziging aan `app.go` of Wails-bindings in dit plan — die komen in Plan 4.

---

## Task 0: Feature-branch

De huidige branch is de default (`release/0.1.x`). Werk op een aparte branch.

- [ ] **Step 1: Maak en checkout de branch**

Run:
```bash
git checkout -b feature/ai-visual-testing
```
Expected: `Switched to a new branch 'feature/ai-visual-testing'`

---

## Task 1: Domain-types + Valid()-helpers

**Files:**
- Create: `internal/domain/testing.go`
- Test: `internal/domain/testing_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/domain/testing_test.go`:
```go
package domain

import "testing"

func TestStepTypeValid(t *testing.T) {
	cases := []struct {
		in   StepType
		want bool
	}{
		{StepNavigate, true},
		{StepClick, true},
		{StepInput, true},
		{StepLogin, true},
		{StepWait, true},
		{StepAssert, true},
		{StepType("bogus"), false},
		{StepType(""), false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Errorf("StepType(%q).Valid() = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEnvKeyValid(t *testing.T) {
	cases := []struct {
		in   EnvKey
		want bool
	}{
		{EnvLocal, true},
		{EnvAcc, true},
		{EnvProd, true},
		{EnvKey("staging"), false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Errorf("EnvKey(%q).Valid() = %v, want %v", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run de test — moet falen (compile-fout)**

Run: `go test ./internal/domain/ -run 'TestStepTypeValid|TestEnvKeyValid' -v`
Expected: FAIL — undefined: `StepNavigate`, `EnvLocal`, etc.

- [ ] **Step 3: Schrijf de implementatie**

Create `internal/domain/testing.go` (alleen `time` wordt hier gebruikt; `fmt`/`sort`/`strings` worden in Task 4/5 toegevoegd wanneer hun functies erbij komen):
```go
package domain

import "time"

// EnvKey identifies one of the three comparable environments.
type EnvKey string

const (
	EnvLocal EnvKey = "local"
	EnvAcc   EnvKey = "acc"
	EnvProd  EnvKey = "prod"
)

func (e EnvKey) Valid() bool {
	switch e {
	case EnvLocal, EnvAcc, EnvProd:
		return true
	}
	return false
}

// StepType is the kind of action a flow step performs.
type StepType string

const (
	StepNavigate StepType = "navigate"
	StepClick    StepType = "click"
	StepInput    StepType = "type"
	StepLogin    StepType = "login"
	StepWait     StepType = "wait"
	StepAssert   StepType = "assert"
)

func (s StepType) Valid() bool {
	switch s {
	case StepNavigate, StepClick, StepInput, StepLogin, StepWait, StepAssert:
		return true
	}
	return false
}

// Step is one action in a flow. Target holds a natural-language description
// (click/type/assert) or a path/URL (navigate). Selector is the last working
// Playwright selector, cached so replay is deterministic; self-heal updates it.
type Step struct {
	Action   StepType `yaml:"action" json:"action"`
	Target   string   `yaml:"target,omitempty" json:"target,omitempty"`
	Value    string   `yaml:"value,omitempty" json:"value,omitempty"`
	Selector string   `yaml:"selector,omitempty" json:"selector,omitempty"`
}

// Flow is a named happy-path scenario.
type Flow struct {
	Name  string `yaml:"name" json:"name"`
	Steps []Step `yaml:"steps" json:"steps"`
}

// Severity ranks a finding.
type Severity string

const (
	SeverityHigh Severity = "hoog"
	SeverityLow  Severity = "laag"
)

// FindingCategory classifies a visual difference.
type FindingCategory string

const (
	CatLayoutBreak    FindingCategory = "layout-break"
	CatMissingElement FindingCategory = "missing-element"
	CatStyling        FindingCategory = "styling"
	CatContentOnly    FindingCategory = "content-only"
)

// Finding is one categorised visual difference for a step.
type Finding struct {
	Category    FindingCategory `json:"category"`
	Severity    Severity        `json:"severity"`
	Where       string          `json:"where"`
	Description string          `json:"description"`
}

// RegressionKind distinguishes console vs HTTP-status regressions.
type RegressionKind string

const (
	RegConsole RegressionKind = "console"
	RegStatus  RegressionKind = "status"
)

// Regression is something broken on the update side relative to the release
// baseline, or a hard failure (5xx) that is always reported.
type Regression struct {
	Kind   RegressionKind `json:"kind"`
	Detail string         `json:"detail"`
	Hard   bool           `json:"hard"`
}

// PageObservation is what the sidecar captures for one side of one step.
type PageObservation struct {
	ConsoleErrors []string       `json:"consoleErrors"`
	StatusCodes   map[string]int `json:"statusCodes"` // requested URL -> HTTP status
}

// StepResult is the compared outcome for a single step.
type StepResult struct {
	Index            int          `json:"index"`
	Action           StepType     `json:"action"`
	ScreenshotBase   string       `json:"screenshotBase"`
	ScreenshotUpdate string       `json:"screenshotUpdate"`
	Findings         []Finding    `json:"findings"`
	Regressions      []Regression `json:"regressions"`
	HealNote         string       `json:"healNote,omitempty"`
	Error            string       `json:"error,omitempty"`
}

// TestRun is one full comparison of a flow across two environments.
type TestRun struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"projectId"`
	FlowName    string       `json:"flowName"`
	BaselineEnv EnvKey       `json:"baselineEnv"`
	UpdateEnv   EnvKey       `json:"updateEnv"`
	Models      []string     `json:"models"`
	StartedAt   time.Time    `json:"startedAt"`
	Steps       []StepResult `json:"steps"`
}
```

> De helpers `ResolveEnvURL` (Task 4b), `DiffRegressions` (Task 5) en `ChooseModelTier` (Task 6) worden aan ditzelfde bestand toegevoegd; die tasks breiden de import-block uit met `fmt`/`sort`/`strings` op het moment dat hun functies erbij komen. Zo compileert Task 1 zelfstandig zonder ongebruikte imports.

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/domain/ -run 'TestStepTypeValid|TestEnvKeyValid' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/testing.go internal/domain/testing_test.go
git commit -m "feat(testing): add domain types for AI visual testing"
```

---

## Task 2: Config-uitbreiding (TestingCfg)

**Files:**
- Modify: `internal/domain/project.go` (voeg types toe onderaan + veld in `ProjectConfig`)

- [ ] **Step 1: Schrijf de falende test**

Add to `internal/domain/testing_test.go`:
```go
func TestProjectConfigHasTesting(t *testing.T) {
	cfg := ProjectConfig{
		Testing: &TestingCfg{
			Environments: map[string]string{"local": "https://x.test"},
			BasicAuth:    map[string]BasicAuth{"acc": {User: "u", Pass: "keychain:p"}},
			TestAccount:  &TestAccount{User: "t", Pass: "keychain:q"},
		},
	}
	if cfg.Testing.Environments["local"] != "https://x.test" {
		t.Fatal("local env not stored")
	}
	if cfg.Testing.BasicAuth["acc"].User != "u" {
		t.Fatal("basic auth not stored")
	}
	if cfg.Testing.TestAccount.User != "t" {
		t.Fatal("test account not stored")
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/domain/ -run TestProjectConfigHasTesting -v`
Expected: FAIL — undefined: `TestingCfg`, `BasicAuth`, `TestAccount`, and `ProjectConfig` has no field `Testing`.

- [ ] **Step 3: Schrijf de implementatie**

Add at the end of `internal/domain/project.go`:
```go
// TestingCfg lives under `testing:` in .rdm.yml (committed, no secrets).
// acc/prod URLs still come from deploy_conf.json; only `local` is set here.
type TestingCfg struct {
	Environments map[string]string    `yaml:"environments,omitempty" json:"environments,omitempty"`
	BasicAuth    map[string]BasicAuth `yaml:"basic_auth,omitempty"   json:"basicAuth,omitempty"`
	TestAccount  *TestAccount         `yaml:"test_account,omitempty" json:"testAccount,omitempty"`
}

// BasicAuth holds HTTP basic-auth credentials for one environment.
// Pass is a keychain: reference, never a literal secret in git.
type BasicAuth struct {
	User string `yaml:"user" json:"user"`
	Pass string `yaml:"pass" json:"pass"`
}

// TestAccount is a site login used by `login` flow steps.
// Pass is a keychain: reference.
type TestAccount struct {
	User string `yaml:"user" json:"user"`
	Pass string `yaml:"pass" json:"pass"`
}
```

And add the field to the `ProjectConfig` struct (after the `SSH` field):
```go
	Testing       *TestingCfg       `yaml:"testing,omitempty"  json:"testing,omitempty"`
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/domain/ -run TestProjectConfigHasTesting -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/project.go internal/domain/testing_test.go
git commit -m "feat(testing): add testing config to ProjectConfig"
```

---

## Task 3: Access-resolver (secrets per omgeving)

**Files:**
- Create: `internal/config/testing_access.go`
- Test: `internal/config/testing_access_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/config/testing_access_test.go`:
```go
package config

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestResolveTestAccess(t *testing.T) {
	// Plain-string secrets resolve as-is (ResolveSecret only hits keychain
	// for "keychain:" refs), so no macOS keychain is needed in tests.
	cfg := &domain.TestingCfg{
		BasicAuth:   map[string]domain.BasicAuth{"acc": {User: "bf", Pass: "s3cret"}},
		TestAccount: &domain.TestAccount{User: "tester", Pass: "pw"},
	}

	acc, err := ResolveTestAccess(cfg, domain.EnvAcc)
	if err != nil {
		t.Fatalf("acc: %v", err)
	}
	if acc.BasicAuthUser != "bf" || acc.BasicAuthPass != "s3cret" {
		t.Errorf("basic auth = %+v", acc)
	}
	if acc.TestUser != "tester" || acc.TestPass != "pw" {
		t.Errorf("test account = %+v", acc)
	}

	// prod has no basic-auth entry -> empty basic-auth, test account still set.
	prod, err := ResolveTestAccess(cfg, domain.EnvProd)
	if err != nil {
		t.Fatalf("prod: %v", err)
	}
	if prod.BasicAuthUser != "" || prod.BasicAuthPass != "" {
		t.Errorf("expected no basic auth for prod, got %+v", prod)
	}
}

func TestResolveTestAccessNil(t *testing.T) {
	got, err := ResolveTestAccess(nil, domain.EnvLocal)
	if err != nil {
		t.Fatalf("nil cfg: %v", err)
	}
	if (got != ResolvedAccess{}) {
		t.Errorf("expected zero ResolvedAccess, got %+v", got)
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/config/ -run TestResolveTestAccess -v`
Expected: FAIL — undefined: `ResolveTestAccess`, `ResolvedAccess`.

- [ ] **Step 3: Schrijf de implementatie**

Create `internal/config/testing_access.go`:
```go
package config

import (
	"fmt"

	"github.com/rdm/sites-tool/internal/domain"
)

// ResolvedAccess holds runtime credentials for one environment, with secrets
// already resolved from the keychain.
type ResolvedAccess struct {
	BasicAuthUser string
	BasicAuthPass string
	TestUser      string
	TestPass      string
}

// ResolveTestAccess resolves basic-auth and test-account secrets for env.
// A nil cfg or a missing basic-auth entry yields empty fields (not an error).
func ResolveTestAccess(cfg *domain.TestingCfg, env domain.EnvKey) (ResolvedAccess, error) {
	var out ResolvedAccess
	if cfg == nil {
		return out, nil
	}
	if ba, ok := cfg.BasicAuth[string(env)]; ok {
		pass, err := ResolveSecret(ba.Pass)
		if err != nil {
			return out, fmt.Errorf("basic-auth %s: %w", env, err)
		}
		out.BasicAuthUser = ba.User
		out.BasicAuthPass = pass
	}
	if cfg.TestAccount != nil {
		pass, err := ResolveSecret(cfg.TestAccount.Pass)
		if err != nil {
			return out, fmt.Errorf("test-account: %w", err)
		}
		out.TestUser = cfg.TestAccount.User
		out.TestPass = pass
	}
	return out, nil
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/config/ -run TestResolveTestAccess -v`
Expected: PASS (beide tests)

- [ ] **Step 5: Commit**

```bash
git add internal/config/testing_access.go internal/config/testing_access_test.go
git commit -m "feat(testing): resolve per-environment access secrets"
```

---

## Task 4: Flow-file lezen/schrijven/valideren + ResolveEnvURL

### 4a — `.rdm/flows.yml`

**Files:**
- Create: `internal/config/flows.go`
- Test: `internal/config/flows_test.go`

- [ ] **Step 1: Schrijf de falende test**

Create `internal/config/flows_test.go`:
```go
package config

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func sampleFlows() []domain.Flow {
	return []domain.Flow{{
		Name: "Contactformulier",
		Steps: []domain.Step{
			{Action: domain.StepNavigate, Target: "/"},
			{Action: domain.StepClick, Target: "Cookies accepteren"},
			{Action: domain.StepInput, Target: "E-mail", Value: "test@example.com"},
			{Action: domain.StepAssert, Target: "Bedankt-bericht zichtbaar"},
		},
	}}
}

func TestFlowsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFlows(dir, sampleFlows()); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadFlows(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Contactformulier" || len(got[0].Steps) != 4 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got[0].Steps[2].Value != "test@example.com" {
		t.Errorf("value lost: %+v", got[0].Steps[2])
	}
}

func TestLoadFlowsMissingFile(t *testing.T) {
	got, err := LoadFlows(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil flows, got %+v", got)
	}
}

func TestValidateFlows(t *testing.T) {
	cases := []struct {
		name    string
		flows   []domain.Flow
		wantErr bool
	}{
		{"ok", sampleFlows(), false},
		{"empty name", []domain.Flow{{Name: "", Steps: sampleFlows()[0].Steps}}, true},
		{"no steps", []domain.Flow{{Name: "X"}}, true},
		{"bad action", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: "boop", Target: "/"}}}}, true},
		{"type without value", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: domain.StepInput, Target: "E-mail"}}}}, true},
		{"missing target", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: domain.StepNavigate}}}}, true},
		{"login without target ok", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: domain.StepLogin}}}}, false},
		{"login with target ok", []domain.Flow{{Name: "X", Steps: []domain.Step{{Action: domain.StepLogin, Target: "/wp-login.php"}}}}, false},
		{"duplicate name", []domain.Flow{sampleFlows()[0], sampleFlows()[0]}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateFlows(c.flows)
			if (err != nil) != c.wantErr {
				t.Errorf("ValidateFlows() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/config/ -run 'Flows' -v`
Expected: FAIL — undefined: `SaveFlows`, `LoadFlows`, `ValidateFlows`.

- [ ] **Step 3: Schrijf de implementatie**

Create `internal/config/flows.go`:
```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdm/sites-tool/internal/domain"
	"gopkg.in/yaml.v3"
)

// FlowsFile is the committed, secret-free flow definition file per project.
const FlowsFile = ".rdm/flows.yml"

type flowsDoc struct {
	Flows []domain.Flow `yaml:"flows"`
}

// LoadFlows reads and validates .rdm/flows.yml. A missing file yields (nil, nil).
func LoadFlows(repoPath string) ([]domain.Flow, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, FlowsFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read flows.yml: %w", err)
	}
	var doc flowsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse flows.yml: %w", err)
	}
	if err := ValidateFlows(doc.Flows); err != nil {
		return nil, err
	}
	return doc.Flows, nil
}

// SaveFlows validates then writes .rdm/flows.yml, creating .rdm if needed.
func SaveFlows(repoPath string, flows []domain.Flow) error {
	if err := ValidateFlows(flows); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(repoPath, ".rdm"), 0o755); err != nil {
		return fmt.Errorf("mkdir .rdm: %w", err)
	}
	data, err := yaml.Marshal(flowsDoc{Flows: flows})
	if err != nil {
		return fmt.Errorf("marshal flows.yml: %w", err)
	}
	return os.WriteFile(filepath.Join(repoPath, FlowsFile), data, 0o644)
}

// ValidateFlows enforces flow invariants; returns the first violation found.
func ValidateFlows(flows []domain.Flow) error {
	seen := map[string]bool{}
	for i, f := range flows {
		if strings.TrimSpace(f.Name) == "" {
			return fmt.Errorf("flow %d: naam ontbreekt", i)
		}
		if seen[f.Name] {
			return fmt.Errorf("dubbele flow-naam %q", f.Name)
		}
		seen[f.Name] = true
		if len(f.Steps) == 0 {
			return fmt.Errorf("flow %q: geen stappen", f.Name)
		}
		for j, s := range f.Steps {
			if !s.Action.Valid() {
				return fmt.Errorf("flow %q stap %d: onbekende actie %q", f.Name, j, s.Action)
			}
			if s.Action == domain.StepInput && strings.TrimSpace(s.Value) == "" {
				return fmt.Errorf("flow %q stap %d: type-stap zonder waarde", f.Name, j)
			}
			// login uses the test-account credentials; wait/login need no target
			if s.Action != domain.StepWait && s.Action != domain.StepLogin && strings.TrimSpace(s.Target) == "" {
				return fmt.Errorf("flow %q stap %d: target ontbreekt", f.Name, j)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/config/ -run 'Flows' -v`
Expected: PASS (alle subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/config/flows.go internal/config/flows_test.go
git commit -m "feat(testing): load/save/validate .rdm/flows.yml"
```

### 4b — ResolveEnvURL

**Files:**
- Modify: `internal/domain/testing.go` (voeg functie + imports toe)
- Test: `internal/domain/testing_test.go`

- [ ] **Step 6: Schrijf de falende test**

Add to `internal/domain/testing_test.go`:
```go
func TestResolveEnvURL(t *testing.T) {
	p := Project{
		Config: ProjectConfig{Testing: &TestingCfg{
			Environments: map[string]string{"local": "https://cefetra.test"},
		}},
		Deploy: DeployConf{Link: DeployLinks{Acc: "https://acc.cefetra.com", Prod: "https://cefetra.com"}},
	}

	cases := []struct {
		env     EnvKey
		want    string
		wantErr bool
	}{
		{EnvLocal, "https://cefetra.test", false},
		{EnvAcc, "https://acc.cefetra.com", false},
		{EnvProd, "https://cefetra.com", false},
		{EnvKey("staging"), "", true},
	}
	for _, c := range cases {
		got, err := ResolveEnvURL(p, c.env)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.env, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.env, got, c.want)
		}
	}
}

func TestResolveEnvURLMissingLocal(t *testing.T) {
	p := Project{}
	if _, err := ResolveEnvURL(p, EnvLocal); err == nil {
		t.Error("expected error for missing local URL")
	}
}
```

- [ ] **Step 7: Run de test — moet falen**

Run: `go test ./internal/domain/ -run ResolveEnvURL -v`
Expected: FAIL — undefined: `ResolveEnvURL`.

- [ ] **Step 8: Schrijf de implementatie**

Change the import block in `internal/domain/testing.go` from `import "time"` to:
```go
import (
	"fmt"
	"strings"
	"time"
)
```

Add to `internal/domain/testing.go`:
```go
// ResolveEnvURL returns the URL for env. `local` comes from the project's
// testing config in .rdm.yml; `acc`/`prod` come from deploy_conf.json.
func ResolveEnvURL(p Project, env EnvKey) (string, error) {
	switch env {
	case EnvLocal:
		if p.Config.Testing != nil {
			if u := strings.TrimSpace(p.Config.Testing.Environments["local"]); u != "" {
				return u, nil
			}
		}
		return "", fmt.Errorf("geen local-URL in .rdm.yml (testing.environments.local)")
	case EnvAcc:
		if u := strings.TrimSpace(p.Deploy.Link.Acc); u != "" {
			return u, nil
		}
		return "", fmt.Errorf("geen acc-URL in deploy_conf.json")
	case EnvProd:
		if u := strings.TrimSpace(p.Deploy.Link.Prod); u != "" {
			return u, nil
		}
		return "", fmt.Errorf("geen prod-URL in deploy_conf.json")
	}
	return "", fmt.Errorf("onbekende omgeving %q", env)
}
```

- [ ] **Step 9: Run de test — moet slagen**

Run: `go test ./internal/domain/ -run ResolveEnvURL -v`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/domain/testing.go internal/domain/testing_test.go
git commit -m "feat(testing): resolve environment URLs from config"
```

---

## Task 5: Console/status-regressie-diff

**Files:**
- Modify: `internal/domain/testing.go` (voeg `DiffRegressions` toe; `sort` is al geïmporteerd? zo niet toevoegen)
- Test: `internal/domain/testing_test.go`

- [ ] **Step 1: Schrijf de falende test**

Add to `internal/domain/testing_test.go`:
```go
import "reflect" // voeg toe aan bestaande imports van dit testbestand

func TestDiffRegressions(t *testing.T) {
	baseline := PageObservation{
		ConsoleErrors: []string{"shared error"},
		StatusCodes:   map[string]int{"/ok": 200, "/always404": 404, "/always500": 500},
	}
	update := PageObservation{
		ConsoleErrors: []string{"shared error", "NEW error"},
		StatusCodes: map[string]int{
			"/ok":         200,
			"/always404":  404, // shared 4xx -> not a regression
			"/always500":  500, // 5xx -> always reported (hard)
			"/new404":     404, // new 4xx -> regression, not hard
			"/new500":     500, // new 5xx -> regression, hard
		},
	}

	got := DiffRegressions(baseline, update)

	want := []Regression{
		{Kind: RegConsole, Detail: "NEW error", Hard: false},
		{Kind: RegStatus, Detail: "/always500 → 500", Hard: true},
		{Kind: RegStatus, Detail: "/new404 → 404", Hard: false},
		{Kind: RegStatus, Detail: "/new500 → 500", Hard: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffRegressions()\n got  %+v\n want %+v", got, want)
	}
}

func TestDiffRegressionsEmpty(t *testing.T) {
	if got := DiffRegressions(PageObservation{}, PageObservation{}); len(got) != 0 {
		t.Errorf("expected no regressions, got %+v", got)
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/domain/ -run DiffRegressions -v`
Expected: FAIL — undefined: `DiffRegressions`.

- [ ] **Step 3: Schrijf de implementatie**

Ensure the import block in `internal/domain/testing.go` includes `sort`:
```go
import (
	"fmt"
	"sort"
	"strings"
	"time"
)
```

Add to `internal/domain/testing.go`:
```go
// DiffRegressions reports console/status problems that are NEW on the update
// side relative to the release baseline, plus hard failures (5xx) which are
// always reported. Output is deterministic (console sorted, then status by URL).
func DiffRegressions(baseline, update PageObservation) []Regression {
	var out []Regression

	base := make(map[string]bool, len(baseline.ConsoleErrors))
	for _, e := range baseline.ConsoleErrors {
		base[e] = true
	}
	var newConsole []string
	for _, e := range update.ConsoleErrors {
		if !base[e] {
			newConsole = append(newConsole, e)
		}
	}
	sort.Strings(newConsole)
	for _, e := range newConsole {
		out = append(out, Regression{Kind: RegConsole, Detail: e})
	}

	urls := make([]string, 0, len(update.StatusCodes))
	for u := range update.StatusCodes {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	for _, u := range urls {
		us := update.StatusCodes[u]
		if us < 400 {
			continue
		}
		hard := us >= 500
		newFailure := baseline.StatusCodes[u] < 400 // was OK or absent on baseline
		if hard || newFailure {
			out = append(out, Regression{
				Kind:   RegStatus,
				Detail: fmt.Sprintf("%s → %d", u, us),
				Hard:   hard,
			})
		}
	}
	return out
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/domain/ -run DiffRegressions -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/testing.go internal/domain/testing_test.go
git commit -m "feat(testing): compute console/status regressions vs baseline"
```

---

## Task 6: Model-routing-beslissing

**Files:**
- Modify: `internal/domain/testing.go` (voeg routing-types + `ChooseModelTier` toe)
- Test: `internal/domain/testing_test.go`

- [ ] **Step 1: Schrijf de falende test**

Add to `internal/domain/testing_test.go`:
```go
func TestChooseModelTier(t *testing.T) {
	cases := []struct {
		name string
		in   RoutingInput
		want ModelTier
	}{
		{"override wins", RoutingInput{Override: TierOpus, Task: TaskTriage}, TierOpus},
		{"triage is haiku", RoutingInput{Task: TaskTriage}, TierHaiku},
		{"heal is sonnet", RoutingInput{Task: TaskHeal}, TierSonnet},
		{"author is sonnet", RoutingInput{Task: TaskAuthor}, TierSonnet},
		{"compare default sonnet", RoutingInput{Task: TaskCompare}, TierSonnet},
		{"compare ambiguous opus", RoutingInput{Task: TaskCompare, Ambiguous: true}, TierOpus},
		{"compare high impact opus", RoutingInput{Task: TaskCompare, HighImpact: true}, TierOpus},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ChooseModelTier(c.in); got != c.want {
				t.Errorf("ChooseModelTier(%+v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/domain/ -run ChooseModelTier -v`
Expected: FAIL — undefined: `RoutingInput`, `ModelTier`, `ChooseModelTier`, tier/task constants.

- [ ] **Step 3: Schrijf de implementatie**

Add to `internal/domain/testing.go`:
```go
// ModelTier is a logical model choice; mapped to a concrete Anthropic model id
// in the claude adapter (Plan 3).
type ModelTier string

const (
	TierHaiku  ModelTier = "haiku"
	TierSonnet ModelTier = "sonnet"
	TierOpus   ModelTier = "opus"
)

// RoutingTask is the kind of AI work being routed.
type RoutingTask string

const (
	TaskTriage  RoutingTask = "triage"
	TaskCompare RoutingTask = "compare"
	TaskHeal    RoutingTask = "heal"
	TaskAuthor  RoutingTask = "author"
)

// RoutingInput drives ChooseModelTier.
type RoutingInput struct {
	Override   ModelTier // empty = auto
	Task       RoutingTask
	Ambiguous  bool // a cheap triage pass found the diff unclear
	HighImpact bool // step already flagged as important
}

// ChooseModelTier picks the cheapest model that fits, escalating only when
// needed. A manual override always wins.
func ChooseModelTier(in RoutingInput) ModelTier {
	if in.Override != "" {
		return in.Override
	}
	switch in.Task {
	case TaskTriage:
		return TierHaiku
	case TaskHeal, TaskAuthor:
		return TierSonnet
	case TaskCompare:
		if in.HighImpact || in.Ambiguous {
			return TierOpus
		}
		return TierSonnet
	}
	return TierSonnet
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/domain/ -run ChooseModelTier -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/testing.go internal/domain/testing_test.go
git commit -m "feat(testing): add model-routing decision"
```

---

## Task 7: Volledige suite + vet

- [ ] **Step 1: Run alle tests met race-detector**

Run: `go test -race ./internal/domain/ ./internal/config/`
Expected: PASS (ok voor beide packages)

- [ ] **Step 2: Vet**

Run: `go vet ./internal/domain/ ./internal/config/`
Expected: geen output (schoon)

- [ ] **Step 3: Bevestig dat de app nog bouwt**

Run: `go build ./...`
Expected: geen fouten

- [ ] **Step 4: Commit (indien vet/format iets wijzigde)**

```bash
git add -A
git commit -m "chore(testing): gofmt/vet clean for foundations" || echo "niets te committen"
```

---

## Self-Review — dekking t.o.v. spec

- **Vrij koppelbare omgevingen + local-veld** → Task 2 (`TestingCfg.Environments`) + Task 4b (`ResolveEnvURL`). ✓
- **Flows meegecommit, uitbreidbaar, geen secrets** → Task 4a (`.rdm/flows.yml`, load/save/validate). ✓
- **Flow-diepte (navigate/click/type/login/wait/assert)** → Task 1 (`StepType` + `Valid`), Task 4a (validatie). ✓
- **Toegang: basic-auth + testaccount via keychain** → Task 2 (types) + Task 3 (`ResolveTestAccess`). ✓
- **Console/status-regressies t.o.v. release + harde fouten** → Task 5 (`DiffRegressions`). ✓
- **Model-routing H/S/O, autonoom + override** → Task 6 (`ChooseModelTier`). ✓
- **Resultaat-datamodel voor historie/PDF** → Task 1 (`TestRun`, `StepResult`, `Finding`, `Regression`). ✓

**Niet in dit plan (bewust, latere plannen):** authoring NL→stappen (Plan 3), self-heal-uitvoering (Plan 2/3), screenshots maken (Plan 2), historie-persistentie naar schijf (Plan 4), UI (Plan 5), tier→model-id-mapping (Plan 3).

**Placeholder-scan:** geen TBD/TODO; alle code-stappen bevatten volledige code. ✓
**Type-consistentie:** `StepInput`(="type"), `ResolvedAccess`, `PageObservation`, `Regression{Kind,Detail,Hard}`, `ModelTier`/`RoutingInput` consistent gebruikt over tasks. ✓
