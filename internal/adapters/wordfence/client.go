// internal/adapters/wordfence/client.go
package wordfence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const productionFeedURL = "https://www.wordfence.com/api/intelligence/v3/vulnerabilities/production"

type Client struct {
	apiKey  string
	BaseURL string
	http    *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		BaseURL: productionFeedURL,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Fetch downloads the raw production feed JSON.
func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordfence fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordfence fetch: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
