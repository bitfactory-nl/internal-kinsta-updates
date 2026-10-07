package wpfleet

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GithubTarget is het "github"-blok van een site in wp-fleet's sites.json:
// waar upload_plugin na een geüploade plugin-zip de PR opent. De plugins staan
// in de site-repo, dus de modus is altijd "files" (pluginbestanden committen).
type GithubTarget struct {
	Repo        string `json:"repo"`
	Mode        string `json:"mode"`
	PluginsPath string `json:"pluginsPath"`
}

// Test seams.
var (
	gitRemoteURL = func(ctx context.Context, dir string) (string, error) {
		out, err := execCommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin").Output()
		return strings.TrimSpace(string(out)), err
	}
	ghAuthToken = func(ctx context.Context) string {
		bin := ghBin()
		if bin == "" {
			return ""
		}
		out, err := execCommandContext(ctx, bin, "auth", "token").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	userHomeDir = os.UserHomeDir
)

// pluginDirCandidates zijn de gangbare plekken van de pluginmap in een site-repo
// (eigen layout met public/, Bedrock, klassiek).
var pluginDirCandidates = []string{
	"public/wp-content/plugins",
	"web/app/plugins",
	"wp-content/plugins",
	"wordpress/wp-content/plugins",
	"htdocs/wp-content/plugins",
}

var githubRemoteRE = regexp.MustCompile(`^(?:git@github\.com:|ssh://git@github\.com/|https://(?:[^@/]+@)?github\.com/)([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// ParseGitHubRepo haalt "owner/naam" uit een GitHub-remote-URL.
func ParseGitHubRepo(remote string) (string, bool) {
	m := githubRemoteRE.FindStringSubmatch(strings.TrimSpace(remote))
	if m == nil {
		return "", false
	}
	return m[1] + "/" + m[2], true
}

// DetectGithubTarget leidt het PR-doel af uit de lokale projectmap van een site:
// de origin-remote moet op GitHub staan en er moet een pluginmap zijn.
func DetectGithubTarget(ctx context.Context, projectPath string) (GithubTarget, bool) {
	if projectPath == "" {
		return GithubTarget{}, false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	remote, err := gitRemoteURL(cctx, projectPath)
	if err != nil {
		return GithubTarget{}, false
	}
	repo, ok := ParseGitHubRepo(remote)
	if !ok {
		return GithubTarget{}, false
	}
	for _, rel := range pluginDirCandidates {
		if info, err := os.Stat(filepath.Join(projectPath, filepath.FromSlash(rel))); err == nil && info.IsDir() {
			return GithubTarget{Repo: repo, Mode: "files", PluginsPath: rel}, true
		}
	}
	return GithubTarget{}, false
}

// ghBin zoekt de GitHub CLI; een vanuit de GUI gestart proces heeft Homebrew
// niet altijd in PATH.
func ghBin() string {
	if p, err := exec.LookPath("gh"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// GitHubToken geeft het token voor de PR's: GITHUB_TOKEN uit de omgeving, anders
// dat van `gh auth login`. Leeg als geen van beide er is (dan slaat wp-fleet de PR over).
func GitHubToken(ctx context.Context, baseEnv []string) string {
	for _, kv := range baseEnv {
		if v, ok := strings.CutPrefix(kv, "GITHUB_TOKEN="); ok && v != "" {
			return v
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return ghAuthToken(cctx)
}

// ZipDirs zijn de mappen waar een in de chat gesleepte plugin-zip meestal staat.
// Ze gaan read-only en op hetzelfde pad de container in, zodat het pad dat
// Claude ziet ook in de container klopt.
func ZipDirs() []string {
	home, err := userHomeDir()
	if err != nil || home == "" {
		return nil
	}
	var dirs []string
	for _, name := range []string{"Downloads", "Desktop"} {
		d := filepath.Join(home, name)
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			dirs = append(dirs, d)
		}
	}
	return dirs
}
