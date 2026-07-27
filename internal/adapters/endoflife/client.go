// Package endoflife is een kleine client voor https://endoflife.date/api,
// met een in-memory cache van 24 uur per product (feed verandert zelden en
// prefill mag geen herhaalde requests veroorzaken).
package endoflife

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Flex is endoflife.date's union-veld: false | true | "YYYY-MM-DD".
type Flex struct {
	IsDate bool
	Date   time.Time
	Bool   bool
}

func (f *Flex) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return fmt.Errorf("endoflife: ongeldige datum %q: %w", s, err)
		}
		f.IsDate, f.Date = true, t
		return nil
	}
	var v bool
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("endoflife: veld is geen datum of bool: %w", err)
	}
	f.Bool = v
	return nil
}

// Cycle is één release-lijn van een product.
type Cycle struct {
	Cycle   string `json:"cycle"`
	Latest  string `json:"latest"`
	LTS     Flex   `json:"lts"`
	EOL     Flex   `json:"eol"`
	Support Flex   `json:"support"`
}

type cacheEntry struct {
	cycles  []Cycle
	fetched time.Time
}

// Client haalt release-cycli op met een TTL-cache per product.
type Client struct {
	http    *http.Client
	baseURL string
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func NewClient() *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://endoflife.date/api",
		ttl:     24 * time.Hour,
		now:     time.Now,
		cache:   map[string]cacheEntry{},
	}
}

// Cycles geeft de release-cycli voor product (bijv. "php", "nodejs").
func (c *Client) Cycles(ctx context.Context, product string) ([]Cycle, error) {
	c.mu.Lock()
	if e, ok := c.cache[product]; ok && c.now().Sub(e.fetched) < c.ttl {
		c.mu.Unlock()
		return e.cycles, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+product+".json", nil)
	if err != nil {
		return nil, fmt.Errorf("endoflife: request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endoflife: fetch %s: %w", product, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endoflife: fetch %s: status %d", product, resp.StatusCode)
	}
	var cycles []Cycle
	if err := json.NewDecoder(resp.Body).Decode(&cycles); err != nil {
		return nil, fmt.Errorf("endoflife: decode %s: %w", product, err)
	}

	c.mu.Lock()
	c.cache[product] = cacheEntry{cycles: cycles, fetched: c.now()}
	c.mu.Unlock()
	return cycles, nil
}
