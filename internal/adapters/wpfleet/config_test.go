package wpfleet

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/rdm/sites-tool/internal/domain"
)

func TestEnvName(t *testing.T) {
	if got := EnvName("klant-a-2"); got != "WP_KLANT_A_2_APP_PASSWORD" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSitesJSONGolden(t *testing.T) {
	sites := []domain.WPSite{
		{ID: "klant-a", Name: "Klant A", URL: "https://www.klant-a.nl", Username: "mcp-bot",
			ProjectPath: "/Users/x/Projects/web-a", Tags: []string{"kinsta"}, Bridge: true},
		{ID: "lokaal", Name: "Lokaal", URL: "http://lokaal.test", Username: "admin", ReadOnly: true},
	}
	got, err := BuildSitesJSON(sites)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile("testdata/sites.golden.json")
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("sites.json wijkt af:\n%s", got)
	}
	if bytes.Contains(got, []byte("/Users/x")) {
		t.Fatal("project_path hoort niet in sites.json")
	}
}

func TestDockerArgsBevatAlleenNamen(t *testing.T) {
	args := DockerArgs(RunOptions{
		Image:         "wp-fleet-mcp:latest",
		ReadOnly:      true,
		SitesJSONPath: "/tmp/x/sites.json",
		EnvNames:      []string{"WP_A_APP_PASSWORD", "WP_B_APP_PASSWORD"},
	})
	joined := strings.Join(args, " ")
	want := "run -i --rm --init -e WP_A_APP_PASSWORD -e WP_B_APP_PASSWORD -e MCP_READ_ONLY=true " +
		"-v /tmp/x/sites.json:/app/config/sites.json:ro -v wp-fleet-logs:/app/logs wp-fleet-mcp:latest"
	if joined != want {
		t.Fatalf("args\n got: %s\nwant: %s", joined, want)
	}
	for _, a := range args {
		if strings.Contains(a, "=") && strings.HasPrefix(a, "WP_") {
			t.Fatalf("waarde van een secret-variabele in argv: %s", a)
		}
	}
}

func TestDockerArgsZonderReadOnly(t *testing.T) {
	args := strings.Join(DockerArgs(RunOptions{Image: "img", SitesJSONPath: "/s.json"}), " ")
	if strings.Contains(args, "MCP_READ_ONLY") {
		t.Fatalf("MCP_READ_ONLY hoort er niet in: %s", args)
	}
}
