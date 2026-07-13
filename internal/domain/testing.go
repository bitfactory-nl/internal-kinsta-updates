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

// The Dutch values ("hoog"/"laag") are intentional user-facing report labels; do not translate to English.
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
