package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdm/sites-tool/internal/adapters/browser"
	"github.com/rdm/sites-tool/internal/config"
	"github.com/rdm/sites-tool/internal/domain"
)

// --- fakes ---

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
	svc.newVision = func(override domain.ModelTier) (visionClient, error) { return vis, nil }
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
	flows, _ := svc.ListFlows("p1")
	if flows[0].Steps[0].Selector != "button.accept" {
		t.Errorf("healed selector not persisted: %+v", flows[0].Steps[0])
	}
}
