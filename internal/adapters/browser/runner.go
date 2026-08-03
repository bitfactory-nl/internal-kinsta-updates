package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Runner spawns the Node/Playwright sidecar. Bin/Args are overridable for tests.
type Runner struct {
	Bin  string   // default "node"
	Args []string // default [scriptPath]
}

// NewRunner returns a Runner that invokes `node <scriptPath>`.
func NewRunner(scriptPath string) *Runner {
	return &Runner{Bin: NodeBin(), Args: []string{scriptPath}}
}

// script is het scriptpad uit Args, voor foutmeldingen.
func (r *Runner) script() string {
	if len(r.Args) > 0 {
		return r.Args[len(r.Args)-1]
	}
	return "sidecar"
}

// Run sends req to the sidecar as JSON on stdin and parses the RunResponse from
// stdout. A non-zero exit or unparseable output is an error (stderr included).
func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return RunResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, r.Bin, r.Args...)
	cmd.Stdin = bytes.NewReader(data)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return RunResponse{}, verduidelijk(r.script(), fmt.Errorf("sidecar exec: %w", err), errb.String())
	}

	var resp RunResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		return RunResponse{}, fmt.Errorf("parse sidecar response: %w", err)
	}
	if resp.Error != "" {
		// Zelfde route als in pdf.go: een door de sidecar zelf gerapporteerde fout
		// verdient dezelfde vertaling als een crash.
		return resp, verduidelijk(r.script(), fmt.Errorf("sidecar reported: %s", resp.Error), "")
	}
	return resp, nil
}
