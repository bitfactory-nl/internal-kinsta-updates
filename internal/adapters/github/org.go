package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// maxOrgRepoPages is een hard plafond tegen een oneindige paginatielus: bij
	// ~590 repos in de doelorg zijn dat 6 pagina's, dus 50 laat ruim marge zonder
	// dat een kapotte server (die altijd een volle pagina teruggeeft) de aanroeper
	// eeuwig laat wachten.
	maxOrgRepoPages = 50

	// maxRawContentBytes begrenst het gelezen antwoord van GetContentsRaw. Bestanden
	// als deploy_conf.json zijn hooguit honderden bytes; een verkeerd pad dat op een
	// grote binary uitkomt mag geen onbegrensd geheugen opslokken.
	maxRawContentBytes = 1 << 20 // 1 MB

	rawContentsAcceptHeader = "application/vnd.github.raw+json"
)

// ErrNotFound signaleert dat het opgevraagde bestand niet bestaat (HTTP 404 op
// de contents API). Aanroepers herkennen dit geval met errors.Is.
var ErrNotFound = errors.New("github: bestand niet gevonden")

// OrgRepo is een repository zoals teruggegeven door de organisatie-repos-lijst
// van de GitHub REST API (subset van velden die de tool nodig heeft).
type OrgRepo struct {
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	HTMLURL       string   `json:"html_url"`
	DefaultBranch string   `json:"default_branch"`
	PushedAt      string   `json:"pushed_at"`
	Archived      bool     `json:"archived"`
	Fork          bool     `json:"fork"`
	Private       bool     `json:"private"`
	Topics        []string `json:"topics"`
}

// OrgClient leest repository-metadata en bestandsinhoud van willekeurige
// GitHub-organisaties over de REST API.
type OrgClient struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewOrgClient bouwt een client geauthenticeerd met een personal access token.
func NewOrgClient(token string) *OrgClient {
	return &OrgClient{
		token:   token,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ListOrgRepos somt alle repositories van org op, gepagineerd (100 per pagina,
// gesorteerd op full_name voor een stabiele volgorde). De paginatie stopt zodra
// een pagina minder dan 100 items teruggeeft, of bij maxOrgRepoPages als
// bescherming tegen een oneindige lus.
func (c *OrgClient) ListOrgRepos(ctx context.Context, org string) ([]OrgRepo, error) {
	if strings.TrimSpace(org) == "" {
		return nil, fmt.Errorf("github: lege organisatienaam")
	}

	const perPage = 100
	var all []OrgRepo
	for page := 1; page <= maxOrgRepoPages; page++ {
		q := url.Values{}
		q.Set("per_page", fmt.Sprintf("%d", perPage))
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("sort", "full_name")
		reqURL := fmt.Sprintf("%s/orgs/%s/repos?%s", c.baseURL, url.PathEscape(org), q.Encode())

		body, err := c.get(ctx, reqURL, "application/vnd.github+json")
		if err != nil {
			return nil, fmt.Errorf("github: list org repos %s (pagina %d): %w", org, page, err)
		}

		var repos []OrgRepo
		if err := json.Unmarshal(body, &repos); err != nil {
			return nil, fmt.Errorf("github: list org repos %s (pagina %d): parse: %w", org, page, err)
		}
		all = append(all, repos...)

		if len(repos) < perPage {
			return all, nil
		}
	}

	return nil, fmt.Errorf("github: list org repos %s: plafond van %d pagina's bereikt zonder eindpagina", org, maxOrgRepoPages)
}

// GetContentsRaw haalt de ruwe bytes van path in owner/repo op via de contents
// API. Een lege ref laat GitHub de default branch gebruiken. HTTP 404 geeft een
// fout terug die met errors.Is(err, ErrNotFound) herkenbaar is.
func (c *OrgClient) GetContentsRaw(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo), escapePath(path))
	if ref != "" {
		q := url.Values{}
		q.Set("ref", ref)
		reqURL += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github: request opbouwen: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", rawContentsAcceptHeader)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: get contents %s/%s %s: %w", owner, repo, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("github: %s/%s %s: %w", owner, repo, path, ErrNotFound)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRawContentBytes))
	if err != nil {
		return nil, fmt.Errorf("github: get contents %s/%s %s: response lezen: %w", owner, repo, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github: get contents %s/%s %s: status %d: %s", owner, repo, path, resp.StatusCode, snippet(body))
	}
	return body, nil
}

// get doet een geauthenticeerde GET en geeft de responsbody terug bij een
// 2xx-status, anders een fout met statuscode en body-snippet.
func (c *OrgClient) get(ctx context.Context, reqURL, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github: request opbouwen: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("response lezen: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, snippet(body))
	}
	return body, nil
}

// escapePath escaped path per segment met url.PathEscape, zodat scheidende
// slashes behouden blijven en elk segment apart correct ge-escaped wordt.
func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}
