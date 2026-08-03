package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/browser"
	"github.com/rdm/sites-tool/internal/adapters/claude"
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
	newVision func(override domain.ModelTier) (visionClient, error)
}

// NewTestService wires the service. newVision defaults to a real claude.Client
// built from the resolved Anthropic key; it is overridden in tests.
func NewTestService(projects *ProjectService, cfg *config.Global, store *RunStore, runner browserRunner) *TestService {
	s := &TestService{projects: projects, cfg: cfg, store: store, runner: runner}
	s.newVision = s.defaultVision
	return s
}

// defaultVision builds a claude.Client from the resolved Anthropic key.
func (s *TestService) defaultVision(override domain.ModelTier) (visionClient, error) {
	key, err := config.ResolveSecret(s.cfg.AI.APIKey)
	if err != nil {
		return nil, fmt.Errorf("anthropic key: %w", err)
	}
	if key == "" {
		return nil, fmt.Errorf("geen Anthropic API-key geconfigureerd (Instellingen)")
	}
	c := claude.NewClient(key)
	c.Override = override
	return c, nil
}

// SidecarScriptPath returns the runner.mjs path, overridable via RDM_SIDECAR.
func SidecarScriptPath() string {
	return vindSidecar("runner.mjs", "RDM_SIDECAR")
}

// DefaultRunHistoryDir is ~/.config/rdm/test-runs.
func DefaultRunHistoryDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rdm", "test-runs")
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
	v, err := s.newVision("")
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

// Screenshot returns a run screenshot as a data: URL for the webview. The path
// must live inside the run-history directory.
func (s *TestService) Screenshot(path string) (string, error) {
	if !s.store.Owns(path) {
		return "", fmt.Errorf("pad buiten historie: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("lees screenshot: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// stepTimeoutMs bounds a single sidecar request (per replay).
const stepTimeoutMs = 30000

// runCtxTimeout is a Go-side backstop for the whole run (all AI + sidecar work).
const runCtxTimeout = 5 * time.Minute

// Run executes a flow on two environments, self-heals failed steps once,
// compares screenshots, computes console/status regressions, saves and returns
// the run. baselineEnv is the release side. override, when non-empty, forces the
// model tier for every AI call in the run ("" = automatic routing).
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
	v, err := s.newVision(override)
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
			TimeoutMs:     stepTimeoutMs,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), runCtxTimeout)
	defer cancel()
	resp, err := s.runner.Run(ctx, buildReq(flows[flowIdx]))
	if err != nil {
		return domain.TestRun{}, fmt.Errorf("run: %w", err)
	}

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
