package wpfleet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestParseGitHubRepo(t *testing.T) {
	cases := []struct {
		remote string
		want   string
		ok     bool
	}{
		{"git@github.com:bitfactory-nl/internal-satispressnobearsdev.git", "bitfactory-nl/internal-satispressnobearsdev", true},
		{"https://github.com/owner/repo.git", "owner/repo", true},
		{"https://x-access-token:abc@github.com/owner/repo", "owner/repo", true},
		{"ssh://git@github.com/owner/repo.name.git", "owner/repo.name", true},
		{"git@gitlab.com:owner/repo.git", "", false},
		{"https://github.com/owner", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ParseGitHubRepo(c.remote)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseGitHubRepo(%q) = %q,%v; want %q,%v", c.remote, got, ok, c.want, c.ok)
		}
	}
}

func nepRemote(t *testing.T, remote string, err error) {
	t.Helper()
	orig := gitRemoteURL
	gitRemoteURL = func(context.Context, string) (string, error) { return remote, err }
	t.Cleanup(func() { gitRemoteURL = orig })
}

func TestDetectGithubTargetVindtPluginmap(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "public", "wp-content", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	nepRemote(t, "git@github.com:org/site.git", nil)

	got, ok := DetectGithubTarget(context.Background(), dir)
	want := GithubTarget{Repo: "org/site", Mode: "files", PluginsPath: "public/wp-content/plugins"}
	if !ok || got != want {
		t.Fatalf("got %+v,%v; want %+v", got, ok, want)
	}
}

func TestDetectGithubTargetZonderRepoOfPluginmap(t *testing.T) {
	dir := t.TempDir()
	nepRemote(t, "git@github.com:org/site.git", nil)
	if _, ok := DetectGithubTarget(context.Background(), dir); ok {
		t.Fatal("zonder pluginmap hoort er geen doel te zijn")
	}
	if _, ok := DetectGithubTarget(context.Background(), ""); ok {
		t.Fatal("zonder projectmap hoort er geen doel te zijn")
	}
	_ = os.MkdirAll(filepath.Join(dir, "wp-content", "plugins"), 0o755)
	nepRemote(t, "", errors.New("geen git"))
	if _, ok := DetectGithubTarget(context.Background(), dir); ok {
		t.Fatal("zonder remote hoort er geen doel te zijn")
	}
}

func TestGitHubTokenOmgevingGaatVoor(t *testing.T) {
	orig := ghAuthToken
	ghAuthToken = func(context.Context) string { return "van-gh" }
	t.Cleanup(func() { ghAuthToken = orig })

	if got := GitHubToken(context.Background(), []string{"GITHUB_TOKEN=uit-env"}); got != "uit-env" {
		t.Fatalf("got %q", got)
	}
	if got := GitHubToken(context.Background(), []string{"GITHUB_TOKEN="}); got != "van-gh" {
		t.Fatalf("got %q", got)
	}
}

func TestZipDirsAlleenBestaandeMappen(t *testing.T) {
	home := t.TempDir()
	_ = os.Mkdir(filepath.Join(home, "Downloads"), 0o755)
	orig := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = orig })

	got := ZipDirs()
	if len(got) != 1 || got[0] != filepath.Join(home, "Downloads") {
		t.Fatalf("got %v", got)
	}
}

func TestDockerArgsZipMappenEnToken(t *testing.T) {
	args := strings.Join(DockerArgs(RunOptions{
		Image: "img", SitesJSONPath: "/s.json",
		ZipDirs: []string{"/Users/x/Downloads", "/Users/x/Desktop"}, GitHubToken: true,
	}), " ")
	for _, want := range []string{
		"-e GITHUB_TOKEN ",
		"-e PLUGIN_ZIP_DIR=/Users/x/Downloads,/Users/x/Desktop",
		"-v /Users/x/Downloads:/Users/x/Downloads:ro",
		"-v /Users/x/Desktop:/Users/x/Desktop:ro",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("%q ontbreekt in: %s", want, args)
		}
	}
	if strings.Contains(args, "GITHUB_TOKEN=") {
		t.Fatalf("tokenwaarde hoort niet in argv: %s", args)
	}
}

func TestBuildSitesJSONMetGithubDoel(t *testing.T) {
	sites := []domain.WPSite{
		{ID: "a", Name: "A", URL: "https://a.nl", Username: "u"},
		{ID: "b", Name: "B", URL: "https://b.nl", Username: "u"},
	}
	got, err := BuildSitesJSON(sites, map[string]GithubTarget{
		"a": {Repo: "org/a", Mode: "files", PluginsPath: "wp-content/plugins"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, `"github": {`) || !strings.Contains(s, `"repo": "org/a"`) || !strings.Contains(s, `"pluginsPath": "wp-content/plugins"`) {
		t.Fatalf("github-blok voor a ontbreekt: %s", s)
	}
	if strings.Count(s, `"github"`) != 1 {
		t.Fatalf("site b hoort geen github-blok te hebben: %s", s)
	}
}
