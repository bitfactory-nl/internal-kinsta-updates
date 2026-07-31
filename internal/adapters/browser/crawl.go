package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// CrawlRequest is the input for the crawl sidecar (JSON on stdin).
type CrawlRequest struct {
	BaseURL   string     `json:"baseURL"`
	MaxPages  int        `json:"maxPages"`
	TimeoutMs int        `json:"timeoutMs"`
	BasicAuth *BasicCred `json:"basicAuth,omitempty"`
}

// CrawlResponse is what the crawl sidecar reports: per uploads path the pages where
// the browser actually requested it. That is evidence of use no database scan can
// give — a rendered page tells the truth about sliders, page builders and lazy
// loading.
type CrawlResponse struct {
	PagesVisited int                 `json:"pagesVisited"`
	PagesPlanned int                 `json:"pagesPlanned"`
	Uploads      map[string][]string `json:"uploads"`
	Errors       []string            `json:"errors"`
}

// Crawler spawns the Node/Playwright crawl sidecar. Bin/Args are overridable so
// tests can substitute a stub.
type Crawler struct {
	Bin  string
	Args []string
}

// NewCrawler returns a Crawler that invokes `node <scriptPath>`.
func NewCrawler(scriptPath string) *Crawler {
	return &Crawler{Bin: "node", Args: []string{scriptPath}}
}

// Crawl visits the site and returns which uploads it saw. A crawl that could not
// reach a single page is an error; partial failures come back in Errors, because a
// site with a few broken pages should still yield its evidence.
func (c *Crawler) Crawl(ctx context.Context, req CrawlRequest) (CrawlResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return CrawlResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, c.Bin, c.Args...)
	cmd.Stdin = bytes.NewReader(data)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return CrawlResponse{}, fmt.Errorf("crawl-sidecar: %w: %s", err, errb.String())
	}

	var resp CrawlResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		return CrawlResponse{}, fmt.Errorf("parse crawl-antwoord: %w", err)
	}
	if resp.PagesVisited == 0 {
		melding := "geen enkele pagina kon worden geopend"
		if len(resp.Errors) > 0 {
			melding = resp.Errors[0]
		}
		return resp, fmt.Errorf("crawl mislukte: %s", melding)
	}
	return resp, nil
}
