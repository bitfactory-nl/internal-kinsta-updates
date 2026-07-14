// Package browser drives the Node/Playwright sidecar that replays flows on two
// environments and captures screenshots, console errors and HTTP status codes.
package browser

import "github.com/rdm/sites-tool/internal/domain"

// BasicCred is HTTP basic-auth for one environment (already resolved, not a ref).
type BasicCred struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// AccountCred is a site login used by `login` steps (already resolved).
type AccountCred struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// EnvTarget is one side of a comparison.
type EnvTarget struct {
	URL       string     `json:"url"`
	BasicAuth *BasicCred `json:"basicAuth,omitempty"`
}

// RunRequest is the sidecar input (sent as JSON on stdin).
type RunRequest struct {
	Baseline      EnvTarget    `json:"baseline"`
	Update        EnvTarget    `json:"update"`
	TestAccount   *AccountCred `json:"testAccount,omitempty"`
	Flow          domain.Flow  `json:"flow"`
	ScreenshotDir string       `json:"screenshotDir"`
	TimeoutMs     int          `json:"timeoutMs"`
}

// SideObservation is what the sidecar captured for one side of one step.
type SideObservation struct {
	Screenshot    string         `json:"screenshot"`
	ConsoleErrors []string       `json:"consoleErrors"`
	StatusCodes   map[string]int `json:"statusCodes"`
}

// SidecarStepResult is the raw per-step outcome (no comparison done here).
type SidecarStepResult struct {
	Index    int             `json:"index"`
	Action   string          `json:"action"`
	Baseline SideObservation `json:"baseline"`
	Update   SideObservation `json:"update"`
	Error    string          `json:"error,omitempty"`
	Snapshot string          `json:"snapshot,omitempty"` // a11y tree on failure (self-heal input, Plan 3)
}

// RunResponse is the sidecar output (JSON on stdout).
type RunResponse struct {
	Steps []SidecarStepResult `json:"steps"`
	Error string              `json:"error,omitempty"`
}
