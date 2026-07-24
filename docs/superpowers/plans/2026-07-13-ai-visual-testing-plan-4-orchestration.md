# AI Visual Testing — Plan 4: test_service orchestratie + Wails Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een `TestService` die een flow end-to-end draait (omgevingen/secrets resolven → sidecar → self-heal met Claude → screenshots vergelijken → console/status-regressies → opslaan met historie), plus flow-beheer en Wails-bindings zodat de frontend het kan aanroepen.

**Architecture:** `TestService` hangt af van twee interfaces — `browserRunner` (Plan 2's `*browser.Runner`) en `visionClient` (Plan 3's `*claude.Client`) — plus een `RunStore` (JSON + screenshots op schijf) en de bestaande `ProjectService`/`config`. De interfaces maken de orchestratie volledig unit-testbaar met fakes; er draaien geen echte browser/API-calls in CI. De Anthropic-key wordt per run uit de Keychain geresolved.

**Tech Stack:** Go stdlib + de eerder gebouwde packages (`internal/domain`, `internal/config`, `internal/adapters/browser`, `internal/adapters/claude`), Wails v3 service-registratie.

**Depends on:** Plans 1–3. **Branch:** blijf op `feature/ai-visual-testing`.

**Spec:** [../specs/2026-07-13-ai-visual-testing-design.md](../specs/2026-07-13-ai-visual-testing-design.md) · **Roadmap:** [2026-07-13-ai-visual-testing-roadmap.md](2026-07-13-ai-visual-testing-roadmap.md)

---

## File Structure

- Modify: `internal/config/schema.go` — voeg `AI AIGlobal` (met `APIKey` keychain-ref) toe aan `Global`.
- Modify: `internal/services/settings_service.go` — expose `AnthropicApiKey` in de settings-DTO.
- Modify: `internal/services/project_service.go` — voeg `Get(id) (domain.Project, bool)` toe.
- Create: `internal/services/runstore.go` + `runstore_test.go` — TestRun-historie op schijf.
- Create: `internal/services/test_service.go` — orchestratie + flow-beheer.
- Create: `internal/services/test_service_test.go` — orchestratie met fakes.
- Modify: `internal/app/app.go` — registreer `TestService` + injecteer deps.

---

## Task 1: AI-config + ProjectService.Get + settings-exposure

**Files:** Modify `internal/config/schema.go`, `internal/services/settings_service.go`, `internal/services/project_service.go`. Test: `internal/services/project_service_test.go` (nieuw of bestaand).

- [ ] **Step 1: Schrijf de falende test**

Create `internal/services/project_service_get_test.go`:
```go
package services

import (
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestProjectServiceGet(t *testing.T) {
	s := NewProjectService(nil)
	s.projects = []domain.Project{{ID: "abc", DisplayName: "X"}}
	got, ok := s.Get("abc")
	if !ok || got.DisplayName != "X" {
		t.Fatalf("Get(abc) = %+v, %v", got, ok)
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("Get(nope) should be false")
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/services/ -run TestProjectServiceGet -v`
Expected: FAIL — undefined `Get`.

- [ ] **Step 3: Implementatie**

Add to `internal/services/project_service.go`:
```go
// Get returns a project by ID from the last scan.
func (s *ProjectService) Get(id string) (domain.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.projects {
		if p.ID == id {
			return p, true
		}
	}
	return domain.Project{}, false
}
```

In `internal/config/schema.go`, add to the `Global` struct (after `Git`):
```go
	AI            AIGlobal      `yaml:"ai"`
```
And add the type:
```go
type AIGlobal struct {
	APIKey string `yaml:"api_key"` // keychain:rdm.anthropic.apiKey or literal (dev only)
}
```

In `internal/services/settings_service.go`: add field to `AppSettings`:
```go
	AnthropicAPIKey  string `json:"anthropicApiKey"`
```
In `Get()` add: `AnthropicAPIKey: s.cfg.AI.APIKey,`
In `Save()` add: `s.cfg.AI.APIKey = settings.AnthropicAPIKey`

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/services/ -run TestProjectServiceGet -v && go build ./internal/...`
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go internal/services/settings_service.go internal/services/project_service.go internal/services/project_service_get_test.go
git commit -m "feat(testing): AI api-key config, settings exposure, ProjectService.Get"
```

---

## Task 2: RunStore (historie op schijf)

**Files:** Create `internal/services/runstore.go`, Test `internal/services/runstore_test.go`.

- [ ] **Step 1: Schrijf de falende test**

Create `internal/services/runstore_test.go`:
```go
package services

import (
	"testing"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestRunStoreRoundTrip(t *testing.T) {
	store := NewRunStore(t.TempDir())

	shotDir, err := store.ScreenshotDir("proj1", "run1")
	if err != nil {
		t.Fatalf("ScreenshotDir: %v", err)
	}
	if shotDir == "" {
		t.Fatal("empty screenshot dir")
	}

	run := domain.TestRun{
		ID: "run1", ProjectID: "proj1", FlowName: "F",
		BaselineEnv: domain.EnvProd, UpdateEnv: domain.EnvLocal,
		StartedAt: time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
		Steps:     []domain.StepResult{{Index: 0, Action: domain.StepNavigate}},
	}
	if err := store.Save(run); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get("proj1", "run1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FlowName != "F" || len(got.Steps) != 1 {
		t.Fatalf("Get mismatch: %+v", got)
	}

	list, err := store.List("proj1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "run1" {
		t.Fatalf("List mismatch: %+v", list)
	}
}

func TestRunStoreListMissing(t *testing.T) {
	store := NewRunStore(t.TempDir())
	list, err := store.List("nobody")
	if err != nil {
		t.Fatalf("List missing: %v", err)
	}
	if list != nil {
		t.Errorf("expected nil, got %+v", list)
	}
}

func TestRunStoreListSortedNewestFirst(t *testing.T) {
	store := NewRunStore(t.TempDir())
	older := domain.TestRun{ID: "a", ProjectID: "p", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	newer := domain.TestRun{ID: "b", ProjectID: "p", StartedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	if err := store.Save(older); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(newer); err != nil {
		t.Fatal(err)
	}
	list, err := store.List("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "b" {
		t.Fatalf("expected newest first, got %+v", list)
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/services/ -run TestRunStore -v`
Expected: FAIL — undefined `NewRunStore`.

- [ ] **Step 3: Implementatie**

Create `internal/services/runstore.go`:
```go
package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/rdm/sites-tool/internal/domain"
)

const runFile = "run.json"

// RunStore persists test-run history on disk under baseDir/<projectID>/<runID>/.
type RunStore struct {
	baseDir string
}

func NewRunStore(baseDir string) *RunStore {
	return &RunStore{baseDir: baseDir}
}

func (s *RunStore) runDir(projectID, runID string) string {
	return filepath.Join(s.baseDir, projectID, runID)
}

// ScreenshotDir ensures and returns the screenshots directory for a run.
func (s *RunStore) ScreenshotDir(projectID, runID string) (string, error) {
	dir := filepath.Join(s.runDir(projectID, runID), "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir screenshots: %w", err)
	}
	return dir, nil
}

// Save writes the run's metadata as run.json.
func (s *RunStore) Save(run domain.TestRun) error {
	dir := s.runDir(run.ProjectID, run.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir run: %w", err)
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, runFile), data, 0o644)
}

// Get reads a single run.
func (s *RunStore) Get(projectID, runID string) (domain.TestRun, error) {
	data, err := os.ReadFile(filepath.Join(s.runDir(projectID, runID), runFile))
	if err != nil {
		return domain.TestRun{}, fmt.Errorf("read run: %w", err)
	}
	var run domain.TestRun
	if err := json.Unmarshal(data, &run); err != nil {
		return domain.TestRun{}, fmt.Errorf("parse run: %w", err)
	}
	return run, nil
}

// List returns all runs for a project, newest first. Missing project → (nil, nil).
func (s *RunStore) List(projectID string) ([]domain.TestRun, error) {
	dir := filepath.Join(s.baseDir, projectID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read project runs: %w", err)
	}
	var runs []domain.TestRun
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		run, err := s.Get(projectID, e.Name())
		if err != nil {
			continue // skip unreadable runs
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	return runs, nil
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/services/ -run TestRunStore -v`
Expected: PASS (alle drie)

- [ ] **Step 5: Commit**

```bash
git add internal/services/runstore.go internal/services/runstore_test.go
git commit -m "feat(testing): RunStore persists test-run history"
```

---

## Task 3: TestService — flow-beheer

**Files:** Create `internal/services/test_service.go`, Test `internal/services/test_service_test.go`.

- [ ] **Step 1: Schrijf de falende test**

Create `internal/services/test_service_test.go`:
```go
package services

import (
	"context"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/browser"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// --- fakes ---

type fakeRunner struct {
	resp     browser.RunResponse
	err      error
	calls    int
	lastReq  browser.RunRequest
}

func (f *fakeRunner) Run(_ context.Context, req browser.RunRequest) (browser.RunResponse, error) {
	f.calls++
	f.lastReq = req
	return f.resp, f.err
}

type fakeVision struct {
	steps    []domain.Step
	findings []domain.Finding
	selector string
	healErr  error
}

func (f *fakeVision) Author(_ context.Context, _ string) ([]domain.Step, error) {
	return f.steps, nil
}
func (f *fakeVision) Compare(_ context.Context, _, _ []byte, _ string, _ bool) ([]domain.Finding, error) {
	return f.findings, nil
}
func (f *fakeVision) Heal(_ context.Context, _, _ string) (string, error) {
	return f.selector, f.healErr
}

// newTestService builds a TestService with a project already loaded at repoPath.
func newTestService(t *testing.T, repoPath string, runner browserRunner, vis visionClient) *TestService {
	t.Helper()
	ps := NewProjectService(nil)
	ps.projects = []domain.Project{{
		ID:   "p1",
		Path: repoPath,
		Config: domain.ProjectConfig{Testing: &domain.TestingCfg{
			Environments: map[string]string{"local": "https://local.test"},
		}},
		Deploy: domain.DeployConf{Link: domain.DeployLinks{Prod: "https://prod.test"}},
	}}
	svc := NewTestService(ps, &config.Global{}, NewRunStore(t.TempDir()), runner)
	svc.newVision = func() (visionClient, error) { return vis, nil }
	return svc
}

func TestServiceFlowsRoundTrip(t *testing.T) {
	repo := t.TempDir()
	svc := newTestService(t, repo, &fakeRunner{}, &fakeVision{})

	flows := []domain.Flow{{Name: "F", Steps: []domain.Step{{Action: domain.StepNavigate, Target: "/"}}}}
	if err := svc.SaveFlows("p1", flows); err != nil {
		t.Fatalf("SaveFlows: %v", err)
	}
	got, err := svc.ListFlows("p1")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(got) != 1 || got[0].Name != "F" {
		t.Fatalf("flows mismatch: %+v", got)
	}
}

func TestServiceAuthorFlow(t *testing.T) {
	vis := &fakeVision{steps: []domain.Step{{Action: domain.StepNavigate, Target: "/"}}}
	svc := newTestService(t, t.TempDir(), &fakeRunner{}, vis)
	steps, err := svc.AuthorFlow("p1", "ga naar home")
	if err != nil {
		t.Fatalf("AuthorFlow: %v", err)
	}
	if len(steps) != 1 || steps[0].Action != domain.StepNavigate {
		t.Fatalf("steps: %+v", steps)
	}
}

func TestServiceUnknownProject(t *testing.T) {
	svc := newTestService(t, t.TempDir(), &fakeRunner{}, &fakeVision{})
	if _, err := svc.ListFlows("nope"); err == nil {
		t.Error("expected error for unknown project")
	}
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/services/ -run TestServiceFlows -v`
Expected: FAIL — undefined `TestService`, `NewTestService`, `browserRunner`, `visionClient`.

- [ ] **Step 3: Implementatie**

Create `internal/services/test_service.go`:
```go
package services

import (
	"context"
	"fmt"

	"github.com/rdm/sites-tool/internal/adapters/browser"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// browserRunner is the subset of *browser.Runner TestService needs (test seam).
type browserRunner interface {
	Run(ctx context.Context, req browser.RunRequest) (browser.RunResponse, error)
}

// visionClient is the subset of *claude.Client TestService needs (test seam).
type visionClient interface {
	Author(ctx context.Context, description string) ([]domain.Step, error)
	Compare(ctx context.Context, baselinePNG, updatePNG []byte, stepDesc string, highImpact bool) ([]domain.Finding, error)
	Heal(ctx context.Context, ariaSnapshot, target string) (string, error)
}

// TestService orchestrates AI visual test runs and flow management.
type TestService struct {
	projects  *ProjectService
	cfg       *config.Global
	store     *RunStore
	runner    browserRunner
	newVision func() (visionClient, error)
}

// NewTestService wires the service. newVision defaults to a real claude.Client
// built from the resolved Anthropic key; it is overridden in tests.
func NewTestService(projects *ProjectService, cfg *config.Global, store *RunStore, runner browserRunner) *TestService {
	s := &TestService{projects: projects, cfg: cfg, store: store, runner: runner}
	s.newVision = s.defaultVision
	return s
}

func (s *TestService) project(id string) (domain.Project, error) {
	p, ok := s.projects.Get(id)
	if !ok {
		return domain.Project{}, fmt.Errorf("project %q niet gevonden", id)
	}
	return p, nil
}

// ListFlows returns the committed flows for a project.
func (s *TestService) ListFlows(projectID string) ([]domain.Flow, error) {
	p, err := s.project(projectID)
	if err != nil {
		return nil, err
	}
	return config.LoadFlows(p.Path)
}

// SaveFlows validates and writes the flows for a project.
func (s *TestService) SaveFlows(projectID string, flows []domain.Flow) error {
	p, err := s.project(projectID)
	if err != nil {
		return err
	}
	return config.SaveFlows(p.Path, flows)
}

// AuthorFlow turns a natural-language description into flow steps via Claude.
func (s *TestService) AuthorFlow(projectID, description string) ([]domain.Step, error) {
	if _, err := s.project(projectID); err != nil {
		return nil, err
	}
	v, err := s.newVision()
	if err != nil {
		return nil, err
	}
	return v.Author(context.Background(), description)
}

// ListRuns returns the run history for a project, newest first.
func (s *TestService) ListRuns(projectID string) ([]domain.TestRun, error) {
	return s.store.List(projectID)
}

// GetRun returns a single stored run.
func (s *TestService) GetRun(projectID, runID string) (domain.TestRun, error) {
	return s.store.Get(projectID, runID)
}
```

> NOTE: `defaultVision` (the real claude.Client factory) is added in Task 5 together with the Wails wiring, because it depends on `config.ResolveSecret` and `claude.NewClient`. For Task 3 the tests override `newVision`, so define a temporary stub now so the package compiles, and REPLACE it in Task 5:
> ```go
> func (s *TestService) defaultVision() (visionClient, error) {
> 	return nil, fmt.Errorf("vision client not wired")
> }
> ```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/services/ -run 'TestServiceFlows|TestServiceAuthor|TestServiceUnknown' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/services/test_service.go internal/services/test_service_test.go
git commit -m "feat(testing): TestService flow management with seams"
```

---

## Task 4: TestService — Run-orchestratie (met self-heal, compare, regressies)

**Files:** Modify `internal/services/test_service.go`, `internal/services/test_service_test.go`.

- [ ] **Step 1: Schrijf de falende test**

Append to `internal/services/test_service_test.go`:
```go
import (
	"os"        // add to the existing import block
	"path/filepath"
)

// writePNG writes a tiny placeholder file so os.ReadFile succeeds during Compare.
func writePNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRunCleanCompareAndRegressions(t *testing.T) {
	repo := t.TempDir()
	// A runner that writes screenshots to the request's dir and reports one step
	// with a NEW console error on the update side.
	runner := &fakeRunner{}
	runner.respFn = func(req browser.RunRequest) browser.RunResponse {
		b := filepath.Join(req.ScreenshotDir, "s0-baseline.png")
		u := filepath.Join(req.ScreenshotDir, "s0-update.png")
		writePNG(t, b)
		writePNG(t, u)
		return browser.RunResponse{Steps: []browser.SidecarStepResult{{
			Index: 0, Action: "navigate",
			Baseline: browser.SideObservation{Screenshot: b, ConsoleErrors: nil, StatusCodes: map[string]int{"/": 200}},
			Update:   browser.SideObservation{Screenshot: u, ConsoleErrors: []string{"NEW err"}, StatusCodes: map[string]int{"/": 200}},
		}}}
	}
	vis := &fakeVision{findings: []domain.Finding{{Category: domain.CatStyling, Severity: domain.SeverityLow, Description: "kleur"}}}

	svc := newTestService(t, repo, runner, vis)
	if err := svc.SaveFlows("p1", []domain.Flow{{Name: "F", Steps: []domain.Step{{Action: domain.StepNavigate, Target: "/"}}}}); err != nil {
		t.Fatal(err)
	}

	run, err := svc.Run("p1", "F", domain.EnvProd, domain.EnvLocal, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(run.Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(run.Steps))
	}
	step := run.Steps[0]
	if len(step.Findings) != 1 || step.Findings[0].Category != domain.CatStyling {
		t.Errorf("findings not attached: %+v", step.Findings)
	}
	if len(step.Regressions) != 1 || step.Regressions[0].Detail != "NEW err" {
		t.Errorf("regression not computed: %+v", step.Regressions)
	}
	// run persisted
	got, err := svc.GetRun("p1", run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.FlowName != "F" {
		t.Errorf("run not saved: %+v", got)
	}
	if runner.calls != 1 {
		t.Errorf("expected 1 runner call (no heal), got %d", runner.calls)
	}
}

func TestServiceRunSelfHealRerun(t *testing.T) {
	repo := t.TempDir()
	runner := &fakeRunner{}
	// First call: step fails with a snapshot. Second call (after heal): success.
	runner.respFn = func(req browser.RunRequest) browser.RunResponse {
		b := filepath.Join(req.ScreenshotDir, "s0-baseline.png")
		u := filepath.Join(req.ScreenshotDir, "s0-update.png")
		writePNG(t, b)
		writePNG(t, u)
		if runner.calls == 1 {
			return browser.RunResponse{Steps: []browser.SidecarStepResult{{
				Index: 0, Action: "click",
				Baseline: browser.SideObservation{Screenshot: b},
				Update:   browser.SideObservation{Screenshot: u},
				Error:    "locator not found", Snapshot: "- button \"Accepteren\"",
			}}}
		}
		return browser.RunResponse{Steps: []browser.SidecarStepResult{{
			Index: 0, Action: "click",
			Baseline: browser.SideObservation{Screenshot: b},
			Update:   browser.SideObservation{Screenshot: u},
		}}}
	}
	vis := &fakeVision{selector: "button.accept"}

	svc := newTestService(t, repo, runner, vis)
	if err := svc.SaveFlows("p1", []domain.Flow{{Name: "F", Steps: []domain.Step{{Action: domain.StepClick, Target: "Accepteren"}}}}); err != nil {
		t.Fatal(err)
	}

	run, err := svc.Run("p1", "F", domain.EnvProd, domain.EnvLocal, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.calls != 2 {
		t.Errorf("expected 2 runner calls (heal + rerun), got %d", runner.calls)
	}
	if run.Steps[0].Error != "" {
		t.Errorf("step should have healed clean, got error %q", run.Steps[0].Error)
	}
	// the healed selector was persisted to the flow
	flows, _ := svc.ListFlows("p1")
	if flows[0].Steps[0].Selector != "button.accept" {
		t.Errorf("healed selector not persisted: %+v", flows[0].Steps[0])
	}
}
```
Also update `fakeRunner` at the top of the file to support a response function:
```go
type fakeRunner struct {
	resp    browser.RunResponse
	respFn  func(browser.RunRequest) browser.RunResponse
	err     error
	calls   int
	lastReq browser.RunRequest
}

func (f *fakeRunner) Run(_ context.Context, req browser.RunRequest) (browser.RunResponse, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return browser.RunResponse{}, f.err
	}
	if f.respFn != nil {
		return f.respFn(req), nil
	}
	return f.resp, nil
}
```

- [ ] **Step 2: Run de test — moet falen**

Run: `go test ./internal/services/ -run TestServiceRun -v`
Expected: FAIL — undefined `Run`.

- [ ] **Step 3: Implementatie**

Add to `internal/services/test_service.go` (imports needed: `os`, `time`, plus existing):
```go
// runTimeout bounds a full run.
const runTimeoutMs = 60000

// Run executes a flow on two environments, self-heals failed steps once,
// compares screenshots, computes console/status regressions, saves and returns
// the run. baselineEnv is the release side. override forces a model tier ("" = auto).
func (s *TestService) Run(projectID, flowName string, baselineEnv, updateEnv domain.EnvKey, override domain.ModelTier) (domain.TestRun, error) {
	p, err := s.project(projectID)
	if err != nil {
		return domain.TestRun{}, err
	}
	flows, err := config.LoadFlows(p.Path)
	if err != nil {
		return domain.TestRun{}, err
	}
	flowIdx := -1
	for i, f := range flows {
		if f.Name == flowName {
			flowIdx = i
			break
		}
	}
	if flowIdx < 0 {
		return domain.TestRun{}, fmt.Errorf("flow %q niet gevonden", flowName)
	}

	baseURL, err := domain.ResolveEnvURL(p, baselineEnv)
	if err != nil {
		return domain.TestRun{}, err
	}
	updURL, err := domain.ResolveEnvURL(p, updateEnv)
	if err != nil {
		return domain.TestRun{}, err
	}
	baseAcc, err := config.ResolveTestAccess(p.Config.Testing, baselineEnv)
	if err != nil {
		return domain.TestRun{}, err
	}
	updAcc, err := config.ResolveTestAccess(p.Config.Testing, updateEnv)
	if err != nil {
		return domain.TestRun{}, err
	}
	v, err := s.newVision()
	if err != nil {
		return domain.TestRun{}, err
	}

	runID := time.Now().Format("20060102-150405")
	shotDir, err := s.store.ScreenshotDir(projectID, runID)
	if err != nil {
		return domain.TestRun{}, err
	}

	buildReq := func(flow domain.Flow) browser.RunRequest {
		return browser.RunRequest{
			Baseline:      browser.EnvTarget{URL: baseURL, BasicAuth: toBasic(baseAcc)},
			Update:        browser.EnvTarget{URL: updURL, BasicAuth: toBasic(updAcc)},
			TestAccount:   toAccount(updAcc),
			Flow:          flow,
			ScreenshotDir: shotDir,
			TimeoutMs:     runTimeoutMs,
		}
	}

	ctx := context.Background()
	resp, err := s.runner.Run(ctx, buildReq(flows[flowIdx]))
	if err != nil {
		return domain.TestRun{}, fmt.Errorf("run: %w", err)
	}

	// Self-heal: for a failed step with a snapshot, ask Claude for a selector,
	// patch the flow, persist, and re-run once.
	if s.healFlow(ctx, v, &flows[flowIdx], resp) {
		if err := config.SaveFlows(p.Path, flows); err != nil {
			return domain.TestRun{}, fmt.Errorf("persist healed flow: %w", err)
		}
		resp, err = s.runner.Run(ctx, buildReq(flows[flowIdx]))
		if err != nil {
			return domain.TestRun{}, fmt.Errorf("rerun after heal: %w", err)
		}
	}

	run := domain.TestRun{
		ID: runID, ProjectID: projectID, FlowName: flowName,
		BaselineEnv: baselineEnv, UpdateEnv: updateEnv,
		StartedAt: time.Now(),
		Models:    []string{modelLabel(override)},
	}
	for _, sr := range resp.Steps {
		sres := domain.StepResult{
			Index:            sr.Index,
			Action:           domain.StepType(sr.Action),
			ScreenshotBase:   sr.Baseline.Screenshot,
			ScreenshotUpdate: sr.Update.Screenshot,
			Error:            sr.Error,
		}
		sres.Regressions = domain.DiffRegressions(
			domain.PageObservation{ConsoleErrors: sr.Baseline.ConsoleErrors, StatusCodes: sr.Baseline.StatusCodes},
			domain.PageObservation{ConsoleErrors: sr.Update.ConsoleErrors, StatusCodes: sr.Update.StatusCodes},
		)
		if sr.Baseline.Screenshot != "" && sr.Update.Screenshot != "" {
			baseImg, e1 := os.ReadFile(sr.Baseline.Screenshot)
			updImg, e2 := os.ReadFile(sr.Update.Screenshot)
			if e1 == nil && e2 == nil {
				highImpact := len(sres.Regressions) > 0
				findings, ferr := v.Compare(ctx, baseImg, updImg,
					fmt.Sprintf("stap %d (%s)", sr.Index, sr.Action), highImpact)
				if ferr != nil {
					sres.Error = joinErr(sres.Error, ferr.Error())
				} else {
					sres.Findings = findings
				}
			}
		}
		run.Steps = append(run.Steps, sres)
	}

	if err := s.store.Save(run); err != nil {
		return run, fmt.Errorf("save run: %w", err)
	}
	return run, nil
}

// healFlow patches selectors for failed steps that carry an accessibility
// snapshot. Returns true if any step was patched.
func (s *TestService) healFlow(ctx context.Context, v visionClient, flow *domain.Flow, resp browser.RunResponse) bool {
	patched := false
	for _, sr := range resp.Steps {
		if sr.Error == "" || sr.Snapshot == "" {
			continue
		}
		if sr.Index < 0 || sr.Index >= len(flow.Steps) {
			continue
		}
		target := flow.Steps[sr.Index].Target
		sel, err := v.Heal(ctx, sr.Snapshot, target)
		if err != nil || sel == "" {
			continue
		}
		flow.Steps[sr.Index].Selector = sel
		patched = true
	}
	return patched
}

func toBasic(a config.ResolvedAccess) *browser.BasicCred {
	if a.BasicAuthUser == "" && a.BasicAuthPass == "" {
		return nil
	}
	return &browser.BasicCred{User: a.BasicAuthUser, Pass: a.BasicAuthPass}
}

func toAccount(a config.ResolvedAccess) *browser.AccountCred {
	if a.TestUser == "" && a.TestPass == "" {
		return nil
	}
	return &browser.AccountCred{User: a.TestUser, Pass: a.TestPass}
}

func modelLabel(override domain.ModelTier) string {
	if override == "" {
		return "auto"
	}
	return string(override)
}

func joinErr(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}
```

- [ ] **Step 4: Run de test — moet slagen**

Run: `go test ./internal/services/ -run TestServiceRun -v`
Expected: PASS (beide runs)

- [ ] **Step 5: Volledige service-suite + commit**

Run: `go test -race ./internal/services/`
Expected: ok

```bash
git add internal/services/test_service.go internal/services/test_service_test.go
git commit -m "feat(testing): run orchestration with self-heal, compare, regressions"
```

---

## Task 5: Wails-wiring + echte vision-factory

**Files:** Modify `internal/services/test_service.go` (vervang `defaultVision`), `internal/app/app.go`.

- [ ] **Step 1: Vervang de stub-`defaultVision`**

In `internal/services/test_service.go`, replace the temporary `defaultVision` stub with the real one, and add the imports `claude` and `time` if not present:
```go
func (s *TestService) defaultVision() (visionClient, error) {
	key, err := config.ResolveSecret(s.cfg.AI.APIKey)
	if err != nil {
		return nil, fmt.Errorf("anthropic key: %w", err)
	}
	if key == "" {
		return nil, fmt.Errorf("geen Anthropic API-key geconfigureerd (Instellingen)")
	}
	return claude.NewClient(key), nil
}
```
Add import: `"github.com/rdm/sites-tool/internal/adapters/claude"`.

- [ ] **Step 2: Sidecar-pad + RunStore-basis helpers**

Add to `internal/services/test_service.go`:
```go
// SidecarScriptPath returns the runner.mjs path, overridable via RDM_SIDECAR.
func SidecarScriptPath() string {
	if p := os.Getenv("RDM_SIDECAR"); p != "" {
		return p
	}
	return filepath.Join("sidecar", "runner.mjs")
}

// DefaultRunHistoryDir is ~/.config/rdm/test-runs.
func DefaultRunHistoryDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "test-runs")
}
```
Add imports `"os"` and `"path/filepath"` (os is already imported from Task 4; add path/filepath).

- [ ] **Step 3: Registreer de service in `internal/app/app.go`**

Add to the `Services` struct:
```go
	Test     *services.TestService
```
In `NewServices`, after the existing constructions, add:
```go
	runner := browser.NewRunner(services.SidecarScriptPath())
	runStore := services.NewRunStore(services.DefaultRunHistoryDir())
	testSvc := services.NewTestService(project, &cfg.Global, runStore, runner)
```
and include `Test: testSvc,` in the returned `&Services{...}`.
Add the import `"github.com/rdm/sites-tool/internal/adapters/browser"`.
In `Wails()`, add to the returned slice:
```go
		application.NewService(s.Test),
```

- [ ] **Step 4: Bouw + genereer bindings**

Run: `go build ./internal/... && go vet ./internal/...`
Expected: clean.

Run (regenerates the TS bindings so the frontend sees TestService — Plan 5 uses them):
```bash
task generate-bindings 2>&1 | tail -5 || echo "bindings task niet beschikbaar; wordt in Plan 5 opgepakt"
```
Expected: bindings regenerated onder `frontend/bindings/...` (of nette melding als de task ontbreekt).

- [ ] **Step 5: Volledige verificatie + commit**

Run: `go test -race ./internal/... && go vet ./internal/...`
Expected: ok + clean.

```bash
git add internal/services/test_service.go internal/app/app.go
git commit -m "feat(testing): wire TestService into Wails app"
```
> Als `task generate-bindings` bindings wijzigde, commit die apart:
> ```bash
> git add frontend/bindings/
> git commit -m "chore(bindings): regenerate for TestService"
> ```

---

## Task 6: Eindverificatie

- [ ] **Step 1: Alles**

Run: `go test -race ./internal/... && go vet ./internal/... && go build ./internal/...`
Expected: ok + clean.

- [ ] **Step 2: Commit (indien nodig)**

```bash
git add -A && git commit -m "chore(testing): vet/format clean for orchestration" || echo "niets te committen"
```

---

## Self-Review — dekking t.o.v. spec

- **Run-levenscyclus (resolve → sidecar → self-heal → compare → regressies → opslaan)** → Task 4 (`Run`). ✓
- **Vrij koppelbare omgevingen (baselineEnv/updateEnv)** → `Run`-params + `domain.ResolveEnvURL`. ✓
- **Basic-auth + testaccount uit Keychain** → `config.ResolveTestAccess` + `toBasic`/`toAccount`. ✓
- **Self-heal + selector persistent maken** → `healFlow` + `config.SaveFlows`; getest (rerun + persist). ✓
- **AI-vergelijking met escalatie bij regressies** → `Compare(..., highImpact=len(regressions)>0)`. ✓
- **Console/status-regressies** → `domain.DiffRegressions` per stap. ✓
- **Historie opslaan/teruglezen** → `RunStore` (Task 2) + `ListRuns`/`GetRun`. ✓
- **Flow-beheer + authoring** → `ListFlows`/`SaveFlows`/`AuthorFlow` (Task 3). ✓
- **API-key config** → Task 1 (`AIGlobal` + settings). ✓
- **Aanroepbaar vanuit frontend** → Task 5 (Wails-registratie + bindings). ✓

**Bewust later (Plan 5):** de TestsTab-UI; verfijnde ambiguity-escalatie (nu via highImpact); kostenschatting vooraf; sidecar-bundeling in de macOS-build (`SidecarScriptPath` gebruikt nu een dev-pad/env-override).

**Placeholder-scan:** geen TBD/TODO; alle Go-stappen hebben volledige code. De `defaultVision`-stub in Task 3 is expliciet tijdelijk en wordt in Task 5 vervangen. ✓
**Type-consistentie:** `browserRunner`/`visionClient`-interfaces matchen `*browser.Runner.Run` en `*claude.Client.{Author,Compare,Heal}`; `toBasic`/`toAccount` mappen `config.ResolvedAccess` → `browser.BasicCred`/`AccountCred`; `RunStore`-methodes consistent tussen service en tests. ✓
