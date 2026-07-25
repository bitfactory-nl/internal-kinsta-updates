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
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		msg := "wordfence fetch: rate-limited (429) — de feed mag maar beperkt opgehaald worden, probeer het later opnieuw"
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			msg += fmt.Sprintf(" (Retry-After: %s)", ra)
		}
		return nil, fmt.Errorf("%s", msg)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("wordfence fetch: status %d — controleer de API-key in Instellingen → Wordfence", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("wordfence fetch: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
