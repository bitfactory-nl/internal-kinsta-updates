package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

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

// ResolveEnvURL returns the URL for env. `local` comes from the project's
// testing config in .rdm/config.yml; `acc`/`prod` come from deploy_conf.json.
func ResolveEnvURL(p Project, env EnvKey) (string, error) {
	switch env {
	case EnvLocal:
		if p.Config.Testing != nil {
			if u := strings.TrimSpace(p.Config.Testing.Environments["local"]); u != "" {
				return u, nil
			}
		}
		return "", fmt.Errorf("geen local-URL in .rdm/config.yml (testing.environments.local)")
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
	// TaskSQL zet een vraag in natuurlijke taal om in een SQL-query.
	TaskSQL RoutingTask = "sql"
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
	case TaskHeal, TaskAuthor, TaskSQL:
		return TierSonnet
	case TaskCompare:
		if in.HighImpact || in.Ambiguous {
			return TierOpus
		}
		return TierSonnet
	}
	return TierSonnet
}
