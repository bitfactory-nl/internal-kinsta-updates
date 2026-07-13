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
