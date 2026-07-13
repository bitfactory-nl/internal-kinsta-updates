package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rdm/sites-tool/internal/domain"
)

const (
	apiURL        = "https://api.anthropic.com/v1/messages"
	apiVersion    = "2023-06-01"
	defaultMaxTok = 1024
)

// HTTPDoer is the subset of *http.Client the adapter needs (test seam).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client calls the Anthropic Messages API.
type Client struct {
	APIKey    string
	HTTP      HTTPDoer
	ModelFor  func(domain.ModelTier) string
	MaxTokens int
}

// NewClient returns a Client using the default HTTP client and tier mapping.
func NewClient(apiKey string) *Client {
	return &Client{APIKey: apiKey, HTTP: http.DefaultClient, ModelFor: tierToModel, MaxTokens: defaultMaxTok}
}

// toolCall sends a forced-tool request and returns the tool_use input JSON.
func (c *Client) toolCall(ctx context.Context, tier domain.ModelTier, system string, content []contentBlock, t tool) (json.RawMessage, error) {
	reqBody := apiRequest{
		Model:      c.ModelFor(tier),
		MaxTokens:  c.MaxTokens,
		System:     system,
		Messages:   []message{{Role: "user", Content: content}},
		Tools:      []tool{t},
		ToolChoice: &toolChoice{Type: "tool", Name: t.Name},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(data))
	}
	var ar apiResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if ar.Error != nil {
		return nil, fmt.Errorf("anthropic error: %s", ar.Error.Message)
	}
	for _, b := range ar.Content {
		if b.Type == "tool_use" {
			return b.Input, nil
		}
	}
	return nil, fmt.Errorf("geen tool_use-blok in antwoord")
}
