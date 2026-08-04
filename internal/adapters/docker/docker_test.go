package docker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// nepDocker legt een uitvoerbaar bestand neer dat als docker moet doorgaan.
func nepDocker(t *testing.T, pad string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(pad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pad, []byte("#!/bin/sh\necho v27.0.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return pad
}

// resetDockerZoeker maakt de cache leeg en herstelt de seams na de test.
func resetDockerZoeker(t *testing.T) {
	t.Helper()
	origLook, origPaden := dockerLookPath, dockerVastePaden
	t.Cleanup(func() {
		dockerLookPath, dockerVastePaden = origLook, origPaden
	})
	dockerLookPath = func(string) (string, error) { return "", errors.New("niet op PATH") }
}

func TestZoekDockerOmgevingsvariabeleWint(t *testing.T) {
	resetDockerZoeker(t)
	t.Setenv("RDM_DOCKER", "/eigen/docker")
	if got := zoekDocker(); got != "/eigen/docker" {
		t.Errorf("zoekDocker = %q, wil de override", got)
	}
}

func TestZoekDockerViaVastPad(t *testing.T) {
	resetDockerZoeker(t)
	t.Setenv("RDM_DOCKER", "")
	pad := nepDocker(t, filepath.Join(t.TempDir(), "docker"))
	dockerVastePaden = []string{"/bestaat/niet/docker", pad}

	if got := zoekDocker(); got != pad {
		t.Errorf("zoekDocker = %q, wil %q — dit is het geval van een .app zonder shell-PATH", got, pad)
	}
}

func TestZoekDockerSlaatNietUitvoerbaarBestandOver(t *testing.T) {
	resetDockerZoeker(t)
	t.Setenv("RDM_DOCKER", "")
	dir := t.TempDir()
	nietUitvoerbaar := filepath.Join(dir, "docker")
	if err := os.WriteFile(nietUitvoerbaar, []byte("geen programma"), 0o644); err != nil {
		t.Fatal(err)
	}
	goed := nepDocker(t, filepath.Join(t.TempDir(), "docker"))
	dockerVastePaden = []string{nietUitvoerbaar, goed}

	if got := zoekDocker(); got != goed {
		t.Errorf("zoekDocker = %q; een bestand zonder x-bit is geen docker", got)
	}
}

// writeFakeDockerBin schrijft een shellscript dat doet alsof het `docker` is:
// het print de argumenten die het kreeg (min "exec -i") en de stdin die het
// ontving, zodat een test kan controleren wat Exec daadwerkelijk aanriep.
func writeFakeDockerBin(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake docker gebruikt /bin/sh")
	}
	dir := t.TempDir()
	pad := filepath.Join(dir, "fake-docker.sh")
	if err := os.WriteFile(pad, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return pad
}

func TestExecWiresStdinEnvArgsAndStdout(t *testing.T) {
	fake := writeFakeDockerBin(t, "#!/bin/sh\necho \"ARGS:$*\"\ncat\n")
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, arg...)
	}

	var out bytes.Buffer
	err := Exec(context.Background(), "bitf-mysql", []string{"mysql", "-uroot", "mydb"}, []string{"MYSQL_PWD=secret"}, strings.NewReader("SELECT 1;"), &out)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	gotOut := out.String()
	if !strings.Contains(gotOut, "ARGS:exec -i -e MYSQL_PWD=secret bitf-mysql mysql -uroot mydb") {
		t.Errorf("onverwachte argumenten doorgegeven: %q", gotOut)
	}
	if !strings.Contains(gotOut, "SELECT 1;") {
		t.Errorf("stdin kwam niet aan op stdout: %q", gotOut)
	}
}

func TestExecWrapsFailureWithStderr(t *testing.T) {
	fake := writeFakeDockerBin(t, "#!/bin/sh\ncat >/dev/null\necho 'ERROR 1045: Access denied' 1>&2\nexit 1\n")
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, arg...)
	}

	var out bytes.Buffer
	err := Exec(context.Background(), "bitf-mysql", []string{"mysql"}, nil, strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("verwachtte een fout")
	}
	if !strings.Contains(err.Error(), "docker exec op \"bitf-mysql\" mislukt") || !strings.Contains(err.Error(), "Access denied") {
		t.Errorf("foutmelding = %v, wil de containernaam en stderr erin", err)
	}
}
