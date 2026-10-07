package wpfleet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

// nepDocker laat execCommandContext deze testbinary als "docker" starten.
func nepDocker(t *testing.T, modus string) {
	t.Helper()
	orig, origBin := execCommandContext, dockerBin
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "WPFLEET_HELPER=1", "WPFLEET_MODUS="+modus)
		return cmd
	}
	dockerBin = func() string { return "docker" }
	t.Cleanup(func() { execCommandContext, dockerBin = orig, origBin })
}

// TestHelperProcess is geen echte test: het is de nep-docker.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("WPFLEET_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	modus := os.Getenv("WPFLEET_MODUS")
	switch args[0] {
	case "info":
		if modus == "down" {
			fmt.Fprintln(os.Stderr, "Cannot connect to the Docker daemon")
			os.Exit(1)
		}
		os.Exit(0)
	case "image":
		if modus == "noimage" {
			os.Exit(1)
		}
		os.Exit(0)
	case "run":
		// Rapporteer wat we zagen: args, of de secret in de env zit, en of
		// sites.json leesbaar was.
		var sitesPath string
		for i, a := range args {
			if a == "-v" && strings.HasSuffix(args[i+1], ":/app/config/sites.json:ro") {
				sitesPath = strings.TrimSuffix(args[i+1], ":/app/config/sites.json:ro")
			}
		}
		sites, _ := os.ReadFile(sitesPath)
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"args":   args,
			"secret": os.Getenv("WP_KLANT_A_APP_PASSWORD"),
			"github": os.Getenv("GITHUB_TOKEN"),
			"sites":  string(sites),
			"dir":    filepath.Dir(sitesPath),
		})
		os.Exit(7)
	}
	os.Exit(2)
}

func testSites() []domain.WPSite {
	return []domain.WPSite{
		{ID: "klant-a", Name: "A", URL: "https://a.nl", Username: "mcp-bot"},
		{ID: "klant-b", Name: "B", URL: "https://b.nl", Username: "mcp-bot"},
	}
}

func secretVoorA(id string) (string, error) {
	if id == "klant-a" {
		return "geheim-a", nil
	}
	return "", errors.New("niet gevonden")
}

func TestLaunchGeeftSecretViaEnvEnRuimtOp(t *testing.T) {
	nepDocker(t, "ok")
	var stdout, stderr bytes.Buffer
	code, err := Launch(context.Background(), LaunchConfig{
		Sites: testSites(), Image: "img", Secret: secretVoorA,
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		BaseEnv: append(os.Environ(), "WPFLEET_HELPER=1", "WPFLEET_MODUS=ok"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Fatalf("exitcode %d, want 7 (van docker doorgegeven)", code)
	}
	var seen struct {
		Args   []string `json:"args"`
		Secret string   `json:"secret"`
		Sites  string   `json:"sites"`
		Dir    string   `json:"dir"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &seen); err != nil {
		t.Fatalf("stdout: %q (%v)", stdout.String(), err)
	}
	if seen.Secret != "geheim-a" {
		t.Fatalf("secret niet in env van docker: %q", seen.Secret)
	}
	for _, a := range seen.Args {
		if strings.Contains(a, "geheim-a") {
			t.Fatalf("secret in argv: %v", seen.Args)
		}
	}
	if !strings.Contains(seen.Sites, `"klant-a"`) || strings.Contains(seen.Sites, `"klant-b"`) {
		t.Fatalf("sites.json hoort alleen klant-a te bevatten: %s", seen.Sites)
	}
	if !strings.Contains(stderr.String(), "klant-b") {
		t.Fatalf("waarschuwing voor klant-b ontbreekt op stderr: %q", stderr.String())
	}
	if _, err := os.Stat(seen.Dir); !os.IsNotExist(err) {
		t.Fatalf("tijdelijke map %s niet opgeruimd", seen.Dir)
	}
}

func TestLaunchDockerDown(t *testing.T) {
	nepDocker(t, "down")
	var stderr bytes.Buffer
	_, err := Launch(context.Background(), LaunchConfig{
		Sites: testSites(), Image: "img", Secret: secretVoorA,
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &stderr,
	})
	if !errors.Is(err, ErrDockerDown) {
		t.Fatalf("err = %v, want ErrDockerDown", err)
	}
}

func TestLaunchImageOntbreekt(t *testing.T) {
	nepDocker(t, "noimage")
	_, err := Launch(context.Background(), LaunchConfig{
		Sites: testSites(), Image: "img", Secret: secretVoorA,
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if !errors.Is(err, ErrImageMissing) {
		t.Fatalf("err = %v, want ErrImageMissing", err)
	}
}

func TestLaunchZonderSites(t *testing.T) {
	// Docker mag niet eens gecontroleerd worden als er geen site overblijft.
	orig := execCommandContext
	aangeroepen := false
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		aangeroepen = true
		return exec.CommandContext(ctx, "false")
	}
	t.Cleanup(func() { execCommandContext = orig })

	var stderr bytes.Buffer
	code, err := Launch(context.Background(), LaunchConfig{
		Sites: testSites(), Image: "img",
		Secret: func(string) (string, error) { return "", errors.New("niet gevonden") },
		Stdin:  strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &stderr,
	})
	if !errors.Is(err, ErrNoSites) {
		t.Fatalf("err = %v, want ErrNoSites", err)
	}
	if code != 1 {
		t.Fatalf("exitcode %d, want 1", code)
	}
	if aangeroepen {
		t.Fatal("docker is aangeroepen terwijl er geen sites zijn")
	}
}

func TestLaunchGeeftGithubDoelEnTokenViaEnv(t *testing.T) {
	nepDocker(t, "ok")
	nepRemote(t, "git@github.com:org/klant-a.git", nil)
	origTok := ghAuthToken
	ghAuthToken = func(context.Context) string { return "gho_geheim" }
	t.Cleanup(func() { ghAuthToken = origTok })

	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "wp-content", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	sites := testSites()
	sites[0].ProjectPath = project

	var stdout bytes.Buffer
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GITHUB_TOKEN=") {
			env = append(env, kv)
		}
	}
	_, err := Launch(context.Background(), LaunchConfig{
		Sites: sites, Image: "img", Secret: secretVoorA,
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &bytes.Buffer{},
		BaseEnv: append(env, "WPFLEET_HELPER=1", "WPFLEET_MODUS=ok"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var seen struct {
		Args   []string `json:"args"`
		Github string   `json:"github"`
		Sites  string   `json:"sites"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &seen); err != nil {
		t.Fatalf("stdout: %q (%v)", stdout.String(), err)
	}
	if seen.Github != "gho_geheim" {
		t.Fatalf("GITHUB_TOKEN niet in env van docker: %q", seen.Github)
	}
	for _, a := range seen.Args {
		if strings.Contains(a, "gho_geheim") {
			t.Fatalf("token in argv: %v", seen.Args)
		}
	}
	if !strings.Contains(seen.Sites, `"repo": "org/klant-a"`) || !strings.Contains(seen.Sites, `"mode": "files"`) {
		t.Fatalf("github-doel ontbreekt in sites.json: %s", seen.Sites)
	}
}
