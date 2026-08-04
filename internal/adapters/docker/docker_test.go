package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	// Exec verwijdert het env-bestand zodra het proces klaar is, dus het
	// fake-script moet de argumenten én de inhoud/rechten van dat bestand
	// zelf wegschrijven terwijl het nog bestaat — daarna is het te laat.
	dumpDir := t.TempDir()
	argsDump := dumpDir + "/args.txt"
	envSnapshot := dumpDir + "/env-snapshot.txt"
	fake := writeFakeDockerBin(t, `#!/bin/sh
echo "$*" > `+argsDump+`
for a in "$@"; do
  if [ "$prev" = "--env-file" ]; then
    stat -f%Lp "$a" > `+envSnapshot+`.perm 2>/dev/null || stat -c%a "$a" > `+envSnapshot+`.perm
    cat "$a" > `+envSnapshot+`
  fi
  prev="$a"
done
cat
`)
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, arg...)
	}

	var out bytes.Buffer
	envFileBefore, _ := filepath.Glob(filepath.Join(os.TempDir(), "rdm-docker-env-*"))
	err := Exec(context.Background(), "bitf-mysql", []string{"mysql", "-uroot", "mydb"}, []string{"MYSQL_PWD=secret"}, strings.NewReader("SELECT 1;"), &out)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(out.String(), "SELECT 1;") {
		t.Errorf("stdin kwam niet aan op stdout: %q", out.String())
	}

	argsRaw, err := os.ReadFile(argsDump)
	if err != nil {
		t.Fatalf("argumenten-dump lezen: %v", err)
	}
	args := strings.TrimSpace(string(argsRaw))
	if strings.Contains(args, "MYSQL_PWD") {
		t.Errorf("wachtwoord staat los op de commandline in plaats van in een --env-file: %q", args)
	}
	if !strings.Contains(args, "--env-file ") {
		t.Errorf("verwacht --env-file, kreeg: %q", args)
	}
	if !strings.HasSuffix(args, "bitf-mysql mysql -uroot mydb") {
		t.Errorf("verwacht dat container en args na het env-file komen, kreeg: %q", args)
	}

	envContent, err := os.ReadFile(envSnapshot)
	if err != nil {
		t.Fatalf("env-snapshot lezen: %v", err)
	}
	if strings.TrimSpace(string(envContent)) != "MYSQL_PWD=secret" {
		t.Errorf("env-bestand inhoud = %q", envContent)
	}
	permRaw, err := os.ReadFile(envSnapshot + ".perm")
	if err != nil {
		t.Fatalf("env-permissies lezen: %v", err)
	}
	if perm := strings.TrimSpace(string(permRaw)); perm != "600" {
		t.Errorf("env-bestand permissies = %q, wil 600", perm)
	}

	// Na Exec moet het tijdelijke env-bestand weer weg zijn.
	envFileAfter, _ := filepath.Glob(filepath.Join(os.TempDir(), "rdm-docker-env-*"))
	if len(envFileAfter) > len(envFileBefore) {
		t.Error("env-bestand had na Exec al verwijderd moeten zijn")
	}
}

func TestExecWithoutEnvSkipsEnvFile(t *testing.T) {
	dumpDir := t.TempDir()
	argsDump := dumpDir + "/args.txt"
	fake := writeFakeDockerBin(t, "#!/bin/sh\necho \"$*\" > "+argsDump+"\ncat >/dev/null\n")
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, arg...)
	}

	if err := Exec(context.Background(), "bitf-mysql", []string{"mysql", "-e", "SHOW TABLES"}, nil, nil, io.Discard); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	argsRaw, err := os.ReadFile(argsDump)
	if err != nil {
		t.Fatalf("argumenten-dump lezen: %v", err)
	}
	if strings.Contains(string(argsRaw), "--env-file") {
		t.Errorf("zonder env-variabelen hoort er geen --env-file te zijn: %q", argsRaw)
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
