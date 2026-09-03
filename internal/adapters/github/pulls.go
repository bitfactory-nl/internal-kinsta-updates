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
	Draft bool   `json:"draft,omitempty"`
}

// CreatePull maakt een nieuwe pull request van head naar base. Bij HTTP 422
// (bijv. "pull request bestaat al" of "no commits between") bevat de fout de
// GitHub-boodschap uit de response, zodat de aanroeper dat geval herkent.
func (c *PullClient) CreatePull(ctx context.Context, owner, repo, head, base, title, body string) (*PullRequest, error) {
	return c.createPull(ctx, owner, repo, createPullBody{Title: title, Head: head, Base: base, Body: body})
}

// CreateDraftPull is CreatePull maar opent de pull request als draft. Dat is wat
// je wil voor werk dat een machine heeft gemaakt: het staat op GitHub zodat het
// te bekijken is, maar het leest niet als iets dat al beoordeeld is en
// review-automatisering slaat er niet op aan.
func (c *PullClient) CreateDraftPull(ctx context.Context, owner, repo, head, base, title, body string) (*PullRequest, error) {
	return c.createPull(ctx, owner, repo, createPullBody{Title: title, Head: head, Base: base, Body: body, Draft: true})
}

func (c *PullClient) createPull(ctx context.Context, owner, repo string, in createPullBody) (*PullRequest, error) {
	payload, err := json.Marshal(in)
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

// RepoAccess is wat de tool over een repo moet weten om te kunnen mergen: mag
// deze token pushen (dat is de rechten-eis voor mergen), en welke merge-methodes
// staat de repo toe. Beide komen uit één repo-call, zodat er niet voor elk
// detail apart hoeft te worden gevraagd.
type RepoAccess struct {
	CanPush          bool
	AllowMergeCommit bool
	AllowSquashMerge bool
	AllowRebaseMerge bool
}

// MergeMethod kiest de methode die deze repo toestaat, met een gewone
// merge-commit als eerste voorkeur. Leeg betekent: geen enkele methode
// toegestaan (dat komt voor bij repo's die alles hebben uitgezet).
func (a RepoAccess) MergeMethod() string {
	switch {
	case a.AllowMergeCommit:
		return "merge"
	case a.AllowSquashMerge:
		return "squash"
	case a.AllowRebaseMerge:
		return "rebase"
	}
	return ""
}

// repoAccessResponse is het stuk van GET /repos/{owner}/{repo} dat we gebruiken.
// permissions komt alleen mee voor een geauthenticeerd verzoek en beschrijft
// wat déze token mag.
type repoAccessResponse struct {
	Permissions struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
	} `json:"permissions"`
	AllowMergeCommit *bool `json:"allow_merge_commit"`
	AllowSquashMerge *bool `json:"allow_squash_merge"`
	AllowRebaseMerge *bool `json:"allow_rebase_merge"`
}

// GetRepoAccess leest de rechten van de huidige token op owner/repo.
func (c *PullClient) GetRepoAccess(ctx context.Context, owner, repo string) (*RepoAccess, error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github: request opbouwen: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: repo %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: repo %s/%s: response lezen: %w", owner, repo, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github: repo %s/%s: status %d: %s", owner, repo, resp.StatusCode, parseGitHubErrorMessage(body))
	}

	var rr repoAccessResponse
	if err := json.Unmarshal(body, &rr); err != nil {
		return nil, fmt.Errorf("github: repo %s/%s: parse: %w", owner, repo, err)
	}

	// Ontbrekende allow_*-velden (bijv. bij een token dat de repo-instellingen
	// niet mag zien) vatten we op als "toegestaan": anders zou de tool mergen
	// weigeren om iets wat ze niet kan uitlezen. Weigert GitHub het alsnog, dan
	// komt dat als duidelijke melding uit de merge zelf.
	aan := func(p *bool) bool { return p == nil || *p }
	return &RepoAccess{
		CanPush:          rr.Permissions.Push || rr.Permissions.Maintain || rr.Permissions.Admin,
		AllowMergeCommit: aan(rr.AllowMergeCommit),
		AllowSquashMerge: aan(rr.AllowSquashMerge),
		AllowRebaseMerge: aan(rr.AllowRebaseMerge),
	}, nil
}

// MergeResult is de uitkomst van een merge-poging.
type MergeResult struct {
	Merged  bool   `json:"merged"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// MergePull merget pull request number met de gegeven methode ("merge",
// "squash" of "rebase"). GitHub weigert een merge met een eigen statuscode als
// het niet kan — geen rechten (403), niet mergebaar wegens conflicten of
// branch-protection (405), of de branch is inmiddels verschoven (409). Die
// gevallen krijgen hier een leesbare melding in plaats van een ruwe status,
// want dit is tekst die de gebruiker te zien krijgt.
func (c *PullClient) MergePull(ctx context.Context, owner, repo string, number int, method string) (*MergeResult, error) {
	if number <= 0 {
		return nil, fmt.Errorf("github: ongeldig pull request-nummer %d", number)
	}
	payload, err := json.Marshal(map[string]string{"merge_method": method})
	if err != nil {
		return nil, fmt.Errorf("github: merge payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/merge", c.baseURL, owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("github: request opbouwen: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: merge pull %s/%s#%d: %w", owner, repo, number, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: merge pull %s/%s#%d: response lezen: %w", owner, repo, number, err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var mr MergeResult
		if err := json.Unmarshal(body, &mr); err != nil {
			return nil, fmt.Errorf("github: merge pull %s/%s#%d: parse: %w", owner, repo, number, err)
		}
		return &mr, nil
	case http.StatusForbidden:
		return nil, fmt.Errorf("geen rechten om te mergen: %s", parseGitHubErrorMessage(body))
	case http.StatusMethodNotAllowed:
		return nil, fmt.Errorf("kan niet gemerged worden: %s", parseGitHubErrorMessage(body))
	case http.StatusConflict:
		return nil, fmt.Errorf("branch is inmiddels verschoven; ververs en probeer opnieuw: %s", parseGitHubErrorMessage(body))
	case http.StatusNotFound:
		return nil, fmt.Errorf("pull request %s/%s#%d niet gevonden (of geen toegang)", owner, repo, number)
	default:
		return nil, fmt.Errorf("github: merge pull %s/%s#%d: status %d: %s", owner, repo, number, resp.StatusCode, parseGitHubErrorMessage(body))
	}
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
