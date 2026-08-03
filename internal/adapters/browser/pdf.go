package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// pdfRequest is the sidecar input (sent as JSON on stdin).
type pdfRequest struct {
	HTML string `json:"html"`
	Path string `json:"path"`
}

// pdfResponse is the sidecar output (JSON on stdout).
type pdfResponse struct {
	OK    bool   `json:"ok"`
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
}

// PDFRunner spawns the Node/Playwright PDF sidecar. Bin/Args are overridable
// for tests, mirroring Runner.
type PDFRunner struct {
	Bin  string   // default "node"
	Args []string // default [scriptPath]
}

// NewPDFRunner returns a PDFRunner that invokes `node <scriptPath>`.
// script is het scriptpad uit Args, voor foutmeldingen.
func (r *PDFRunner) script() string {
	if len(r.Args) > 0 {
		return r.Args[len(r.Args)-1]
	}
	return "sidecar"
}

func NewPDFRunner(scriptPath string) *PDFRunner {
	return &PDFRunner{Bin: "node", Args: []string{scriptPath}}
}

// RenderPDF sends {html, path} to the sidecar on stdin and expects
// {"ok":true} (or {"error":...}) on stdout. A non-zero exit, unparseable
// output, or a reported error is returned as a Go error.
func (r *PDFRunner) RenderPDF(ctx context.Context, html, outPath string) error {
	data, err := json.Marshal(pdfRequest{HTML: html, Path: outPath})
	if err != nil {
		return fmt.Errorf("marshal pdf request: %w", err)
	}

	cmd := exec.CommandContext(ctx, r.Bin, r.Args...)
	cmd.Stdin = bytes.NewReader(data)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return verduidelijk(r.script(), fmt.Errorf("pdf sidecar exec: %w", err), errb.String())
	}

	var resp pdfResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		return fmt.Errorf("parse pdf sidecar response: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("pdf sidecar reported: %s", resp.Error)
	}
	if !resp.OK {
		return fmt.Errorf("pdf sidecar did not report ok")
	}
	return nil
}
