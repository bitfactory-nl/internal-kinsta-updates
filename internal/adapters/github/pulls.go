package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PullRequest is een GitHub pull request (subset van velden die de tool nodig heeft).
type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

// PullClient beheert pull requests op willekeurige GitHub-repositories.
type PullClient struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewPullClient bouwt een client geauthenticeerd met een personal access token.
func NewPullClient(token string) *PullClient {
	return &PullClient{
		token:   token,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ParseRepoFromRemote haalt owner en repo uit een git remote-URL. Ondersteund:
// scp-achtige ssh-vorm ("git@github.com:owner/repo.git"), "https://github.com/owner/repo(.git)"
// en "ssh://git@github.com/owner/repo.git". Omringende whitespace en trailing
// slashes worden genegeerd. Alles wat niet naar github.com wijst of niet als
// owner/repo te herleiden is, geeft een duidelijke fout terug (geen panic).
func ParseRepoFromRemote(remoteURL string) (owner, repo string, err error) {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return "", "", fmt.Errorf("github: lege remote-URL")
	}

	raw := trimmed
	if !strings.Contains(raw, "://") {
		host, path, ok := splitSCPLike(raw)
		if !ok {
			return "", "", fmt.Errorf("github: onherkenbare remote-URL %q", remoteURL)
		}
		raw = "ssh://" + host + "/" + path
	}

	u, parseErr := url.Parse(raw)
	if parseErr != nil {
		return "", "", fmt.Errorf("github: kan remote-URL %q niet parsen: %w", remoteURL, parseErr)
	}

	host := strings.ToLower(u.Hostname())
	if host != "github.com" {
		return "", "", fmt.Errorf("github: remote %q is geen github.com-URL", remoteURL)
	}

	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("github: kan owner/repo niet uit %q halen", remoteURL)
	}
	return parts[0], parts[1], nil
}

// splitSCPLike ontleedt de scp-achtige git-syntax "user@host:path" die geen
// "://"-scheme heeft. ok is false als s die vorm niet heeft.
func splitSCPLike(s string) (host, path string, ok bool) {
	at := strings.Index(s, "@")
	if at < 0 {
		return "", "", false
	}
	rest := s[at+1:]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return "", "", false
	}
	host, path = rest[:colon], rest[colon+1:]
	if host == "" || path == "" || strings.Contains(host, "/") {
		return "", "", false
	}
	return host, path, true
}

// FindOpenPull zoekt een openstaande pull request voor head (branchnaam) in
// owner/repo. Geeft (nil, nil) terug als er geen open PR bestaat — dat is
// geen foutgeval, alleen een lege GitHub-resultaatlijst.
func (c *PullClient) FindOpenPull(ctx context.Context, owner, repo, head string) (*PullRequest, error) {
	q := url.Values{}
	q.Set("state", "open")
	q.Set("head", owner+":"+head)
	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", c.baseURL, owner, repo, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github: request opbouwen: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: find open pull %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: find open pull %s/%s: response lezen: %w", owner, repo, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github: find open pull %s/%s: status %d: %s", owner, repo, resp.StatusCode, snippet(body))
	}

	var pulls []PullRequest
	if err := json.Unmarshal(body, &pulls); err != nil {
		return nil, fmt.Errorf("github: find open pull %s/%s: parse: %w", owner, repo, err)
	}
	if len(pulls) == 0 {
		return nil, nil
	}
	return &pulls[0], nil
}

type createPullBody struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body"`
}

// CreatePull maakt een nieuwe pull request van head naar base. Bij HTTP 422
// (bijv. "pull request bestaat al" of "no commits between") bevat de fout de
// GitHub-boodschap uit de response, zodat de aanroeper dat geval herkent.
func (c *PullClient) CreatePull(ctx context.Context, owner, repo, head, base, title, body string) (*PullRequest, error) {
	payload, err := json.Marshal(createPullBody{Title: title, Head: head, Base: base, Body: body})
	if err != nil {
		return nil, fmt.Errorf("github: create pull payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls", c.baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("github: request opbouwen: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: create pull %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: create pull %s/%s: response lezen: %w", owner, repo, err)
	}

	if resp.StatusCode == http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("github: create pull %s/%s geweigerd (422): %s", owner, repo, parseGitHubErrorMessage(respBody))
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("github: create pull %s/%s: status %d: %s", owner, repo, resp.StatusCode, snippet(respBody))
	}

	var pr PullRequest
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return nil, fmt.Errorf("github: create pull %s/%s: parse: %w", owner, repo, err)
	}
	return &pr, nil
}

func (c *PullClient) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

// githubErrorBody is de foutvorm die de GitHub REST API teruggeeft bij 4xx-status:
// een top-level message plus optioneel een lijst met veldspecifieke errors.
type githubErrorBody struct {
	Message string `json:"message"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// parseGitHubErrorMessage haalt de leesbare GitHub-boodschap uit een
// foutresponse. Valt terug op een ruwe snippet als de body niet het
// verwachte GitHub-foutformaat heeft.
func parseGitHubErrorMessage(body []byte) string {
	var ge githubErrorBody
	if err := json.Unmarshal(body, &ge); err != nil {
		return snippet(body)
	}

	msg := ge.Message
	for _, e := range ge.Errors {
		if e.Message == "" {
			continue
		}
		if msg == "" {
			msg = e.Message
		} else {
			msg = msg + ": " + e.Message
		}
	}
	if msg == "" {
		return snippet(body)
	}
	return msg
}

// snippet knipt een response-body af tot een leesbare lengte voor foutmeldingen.
func snippet(body []byte) string {
	const maxLen = 300
	s := strings.TrimSpace(string(body))
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}
