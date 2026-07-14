package services

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/adapters/gitcli"
	"github.com/rdm/sites-tool/internal/adapters/github"
	"github.com/rdm/sites-tool/internal/config"
)

const securityArtifactPrefix = "security-audit-"

// SecurityService fetches security-scan resultaten (composer + npm audit) die de
// wekelijkse GitHub Actions workflow per project-repo als artifact uploadt.
type SecurityService struct {
	cfg     *config.Global
	project *ProjectService
}

// NewSecurityService creates a SecurityService.
func NewSecurityService(cfg *config.Global, project *ProjectService) *SecurityService {
	return &SecurityService{cfg: cfg, project: project}
}

// SecurityFinding is één kwetsbaarheid uit een composer- of npm-audit.
type SecurityFinding struct {
	Source   string `json:"source"`   // "composer" | "npm"
	Package  string `json:"package"`  // pakketnaam
	Severity string `json:"severity"` // critical | high | moderate/medium | low | unknown
	Title    string `json:"title"`
	CVE      string `json:"cve"`
	Link     string `json:"link"`
}

// SecurityScanResult is de meest recente scan-uitkomst van een project-repo.
type SecurityScanResult struct {
	Repo              string            `json:"repo"`
	ScannedAt         time.Time         `json:"scannedAt"`
	HasComposerReport bool              `json:"hasComposerReport"`
	HasNpmReport      bool              `json:"hasNpmReport"`
	Findings          []SecurityFinding `json:"findings"`
}

// GetScanResults haalt het nieuwste security-audit artifact op voor het project.
// Geeft een foutmelding als het project geen GitHub-remote heeft of als er nog
// geen scan-artifact bestaat.
func (s *SecurityService) GetScanResults(projectID string) (*SecurityScanResult, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	repo, err := githubRepoFor(ctx, path)
	if err != nil {
		return nil, err
	}

	token, err := config.ResolveSecret(s.cfg.PluginRepo.GithubToken)
	if err != nil {
		return nil, fmt.Errorf("github token: %w", err)
	}
	if strings.Trim(token, `"'`) == "" {
		return nil, fmt.Errorf("geen GitHub access token ingesteld — voeg deze toe via ⚙ Instellingen → GitHub")
	}
	client := github.NewActionsClient(token)

	artifact, err := latestSecurityArtifact(ctx, client, repo)
	if err != nil {
		return nil, err
	}

	zipBytes, err := client.DownloadArtifact(ctx, repo, artifact.ID)
	if err != nil {
		return nil, err
	}

	result := &SecurityScanResult{
		Repo:      repo,
		ScannedAt: artifact.CreatedAt,
		Findings:  []SecurityFinding{},
	}
	if err := parseAuditZip(zipBytes, result); err != nil {
		return nil, err
	}
	sortFindings(result.Findings)
	return result, nil
}

// ProjectWorkflow is een actieve GitHub Actions workflow van een project-repo,
// met de status van de meest recente run.
type ProjectWorkflow struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	RunStatus     string `json:"runStatus"`
	RunConclusion string `json:"runConclusion"`
	RunURL        string `json:"runUrl"`
	RunAt         string `json:"runAt"`
}

// ListWorkflows haalt de actieve GitHub Actions workflows van het project-repo
// op, elk met de status van de meest recente run (leeg als er nog geen run is).
func (s *SecurityService) ListWorkflows(projectID string) ([]ProjectWorkflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, repo, err := s.githubClientFor(ctx, projectID)
	if err != nil {
		return nil, err
	}

	workflows, err := client.ListWorkflows(ctx, repo)
	if err != nil {
		return nil, err
	}

	result := make([]ProjectWorkflow, 0, len(workflows))
	for _, w := range workflows {
		if w.State != "active" {
			continue
		}
		pw := ProjectWorkflow{ID: w.ID, Name: w.Name, Path: w.Path}

		run, err := client.LatestRun(ctx, repo, w.ID)
		if err != nil {
			return nil, err
		}
		if run != nil {
			pw.RunStatus = run.Status
			pw.RunConclusion = run.Conclusion
			pw.RunURL = run.HTMLURL
			pw.RunAt = run.CreatedAt
		}
		result = append(result, pw)
	}
	return result, nil
}

// DispatchWorkflow start handmatig een workflow_dispatch-run van workflowID op
// de default branch van het project-repo.
func (s *SecurityService) DispatchWorkflow(projectID string, workflowID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, repo, err := s.githubClientFor(ctx, projectID)
	if err != nil {
		return err
	}

	ref, err := client.DefaultBranch(ctx, repo)
	if err != nil {
		return err
	}
	return client.DispatchWorkflow(ctx, repo, workflowID, ref)
}

// githubClientFor resolves projectID to its repo and builds an authenticated
// ActionsClient, reusing the same token/remote resolution as GetScanResults.
func (s *SecurityService) githubClientFor(ctx context.Context, projectID string) (*github.ActionsClient, string, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, "", err
	}

	repo, err := githubRepoFor(ctx, path)
	if err != nil {
		return nil, "", err
	}

	token, err := config.ResolveSecret(s.cfg.PluginRepo.GithubToken)
	if err != nil {
		return nil, "", fmt.Errorf("github token: %w", err)
	}
	if strings.Trim(token, `"'`) == "" {
		return nil, "", fmt.Errorf("geen GitHub access token ingesteld — voeg deze toe via ⚙ Instellingen → GitHub")
	}
	return github.NewActionsClient(token), repo, nil
}

// pathFor resolves a projectID to its filesystem path.
func (s *SecurityService) pathFor(id string) (string, error) {
	for _, p := range s.project.List() {
		if p.ID == id {
			return p.Path, nil
		}
	}
	return "", fmt.Errorf("project %q not found", id)
}

var githubRemoteRe = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(?:\.git)?$`)

// githubRepoFor derives "org/repo" from the project's origin remote URL.
func githubRepoFor(ctx context.Context, path string) (string, error) {
	out, err := gitcli.Run(ctx, path, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("geen git remote gevonden: %w", err)
	}
	m := githubRemoteRe.FindStringSubmatch(strings.TrimSpace(out))
	if m == nil {
		return "", fmt.Errorf("remote is geen GitHub-repository: %s", strings.TrimSpace(out))
	}
	return m[1], nil
}

// latestSecurityArtifact returns the newest non-expired security-audit artifact.
func latestSecurityArtifact(ctx context.Context, client *github.ActionsClient, repo string) (*github.Artifact, error) {
	artifacts, err := client.ListArtifacts(ctx, repo)
	if err != nil {
		return nil, err
	}

	var latest *github.Artifact
	for i := range artifacts {
		a := artifacts[i]
		if a.Expired || !strings.HasPrefix(a.Name, securityArtifactPrefix) {
			continue
		}
		if latest == nil || a.CreatedAt.After(latest.CreatedAt) {
			latest = &a
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("geen security-scan artifact gevonden voor %s — draai eerst de update-workflow", repo)
	}
	return latest, nil
}

// parseAuditZip extracts composer-audit.json and npm-audit.json from the
// artifact zip and appends their findings to result.
func parseAuditZip(zipBytes []byte, result *SecurityScanResult) error {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("artifact zip lezen: %w", err)
	}

	for _, f := range reader.File {
		name := f.Name
		if name != "composer-audit.json" && name != "npm-audit.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("artifact bestand %s openen: %w", name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("artifact bestand %s lezen: %w", name, err)
		}

		switch name {
		case "composer-audit.json":
			result.HasComposerReport = true
			result.Findings = append(result.Findings, parseComposerAudit(data)...)
		case "npm-audit.json":
			result.HasNpmReport = true
			result.Findings = append(result.Findings, parseNpmAudit(data)...)
		}
	}
	return nil
}

// composer audit --format=json: {"advisories": {"vendor/pkg": [{...}, ...]}}
type composerAdvisory struct {
	Title    string `json:"title"`
	CVE      string `json:"cve"`
	Link     string `json:"link"`
	Severity string `json:"severity"`
}

func parseComposerAudit(data []byte) []SecurityFinding {
	var doc struct {
		Advisories map[string][]composerAdvisory `json:"advisories"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}

	var findings []SecurityFinding
	for pkg, advisories := range doc.Advisories {
		for _, a := range advisories {
			findings = append(findings, SecurityFinding{
				Source:   "composer",
				Package:  pkg,
				Severity: normalizeSeverity(a.Severity),
				Title:    a.Title,
				CVE:      a.CVE,
				Link:     a.Link,
			})
		}
	}
	return findings
}

// npm audit --json: {"vulnerabilities": {"pkg": {"severity", "via": [str|obj]}}}
type npmVulnerability struct {
	Severity string            `json:"severity"`
	Via      []json.RawMessage `json:"via"`
}

type npmVia struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

func parseNpmAudit(data []byte) []SecurityFinding {
	var doc struct {
		Vulnerabilities map[string]npmVulnerability `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}

	var findings []SecurityFinding
	for pkg, vuln := range doc.Vulnerabilities {
		finding := SecurityFinding{
			Source:   "npm",
			Package:  pkg,
			Severity: normalizeSeverity(vuln.Severity),
		}
		// via bevat strings (verwijzing naar ander pakket) of objecten (de
		// advisory zelf); pak de eerste advisory met details.
		for _, raw := range vuln.Via {
			var v npmVia
			if err := json.Unmarshal(raw, &v); err != nil || v.Title == "" {
				continue
			}
			finding.Title = v.Title
			finding.Link = v.URL
			break
		}
		// Transitieve entries zonder eigen advisory zijn ruis in het overzicht.
		if finding.Title == "" {
			continue
		}
		findings = append(findings, finding)
	}
	return findings
}

var severityRank = map[string]int{
	"critical": 0,
	"high":     1,
	"moderate": 2,
	"medium":   2,
	"low":      3,
}

func normalizeSeverity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "medium" {
		return "moderate"
	}
	if _, ok := severityRank[s]; !ok {
		return "unknown"
	}
	return s
}

func sortFindings(findings []SecurityFinding) {
	sort.SliceStable(findings, func(i, j int) bool {
		ri, ok := severityRank[findings[i].Severity]
		if !ok {
			ri = 4
		}
		rj, ok := severityRank[findings[j].Severity]
		if !ok {
			rj = 4
		}
		if ri != rj {
			return ri < rj
		}
		return findings[i].Package < findings[j].Package
	})
}
