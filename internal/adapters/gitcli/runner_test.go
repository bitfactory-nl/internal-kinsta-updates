package gitcli

import (
	"bufio"
	"context"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startScriptViaGitCmd bouwt via newGitCmd (dus met de echte Cancel/WaitDelay-
// bedrading) een cmd en vervangt het binary door een shellscript, zodat het
// afbraakgedrag deterministisch te testen is zonder een hangend git-proces.
// Het script moet één regel naar stdout schrijven zodra het klaarstaat; die
// regel wordt teruggegeven.
func startScriptViaGitCmd(t *testing.T, ctx context.Context, script string) (*exec.Cmd, string) {
	t.Helper()
	cmd := newGitCmd(ctx, t.TempDir())
	cmd.Path = "/bin/sh"
	cmd.Args = []string{"sh", "-c", script}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	line, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil {
		t.Fatalf("ready-regel lezen: %v", err)
	}
	return cmd, strings.TrimSpace(line)
}

func TestCancelStuurtEerstSIGTERM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, _ := startScriptViaGitCmd(t, ctx, `trap 'exit 0' TERM; echo klaar; while :; do sleep 1; done`)

	begin := time.Now()
	cancel()
	_ = cmd.Wait()
	if d := time.Since(begin); d >= 1900*time.Millisecond {
		t.Errorf("afbraak duurde %v — SIGTERM had vóór de WaitDelay (2s) moeten werken", d)
	}
}

func TestCancelEscaleertNaWaitDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, _ := startScriptViaGitCmd(t, ctx, `trap '' TERM; echo klaar; while :; do sleep 1; done`)

	begin := time.Now()
	cancel()
	_ = cmd.Wait()
	d := time.Since(begin)
	if d < 1500*time.Millisecond {
		t.Errorf("afbraak duurde maar %v — proces dat SIGTERM negeert hoort pas na de WaitDelay met SIGKILL afgekapt te worden", d)
	}
	if d > 15*time.Second {
		t.Errorf("afbraak duurde %v — SIGKILL na WaitDelay is uitgebleven", d)
	}
}

// wachtTotDood pollt tot pid verdwenen is; ruimt op als dat niet gebeurt.
func wachtTotDood(pid int) bool {
	for i := 0; i < 20; i++ {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL) // niet laten slingeren
	return false
}

// kanGroepsignaalTesten controleert of deze omgeving een SIGTERM aan een
// Setpgid-procesgroep überhaupt aflevert bij een kleinkind van een shell.
// Onder go test op sommige macOS-versies komt dat signaal daar aantoonbaar
// niet aan, terwijl exact dezelfde code standalone (en dus in de app) wél
// werkt — geverifieerd met losstaande reproducties op 2026-07-28. In zo'n
// omgeving is dit gedrag niet zinvol te asserten en skippen we.
func kanGroepsignaalTesten(t *testing.T) bool {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", `trap 'exit 0' TERM; sleep 29731 & echo $!; wait`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("probe StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("probe Start: %v", err)
	}
	line, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil {
		t.Fatalf("probe ready-regel: %v", err)
	}
	kid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("probe pid parsen: %v", err)
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	_ = cmd.Wait()
	return wachtTotDood(kid)
}

func TestCancelKiltHeleProcesgroep(t *testing.T) {
	if !kanGroepsignaalTesten(t) {
		t.Skip("omgeving levert groepssignalen niet af aan shell-kleinkinderen (macOS/go-test-eigenaardigheid); gedrag standalone geverifieerd")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Het script start een kleinkind (zoals git dat doet met git-remote-http
	// of hook-scripts) en meldt diens pid.
	cmd, pidLine := startScriptViaGitCmd(t, ctx, `trap 'exit 0' TERM; sleep 29731 & echo $!; wait`)
	kleinkind, err := strconv.Atoi(pidLine)
	if err != nil {
		t.Fatalf("pid parsen uit %q: %v", pidLine, err)
	}

	cancel()
	_ = cmd.Wait()

	if !wachtTotDood(kleinkind) {
		t.Errorf("kleinkind-proces %d draait nog na cancel — signaal bereikt de procesgroep niet", kleinkind)
	}
}

func TestNewGitCmdVeiligeDefaults(t *testing.T) {
	cmd := newGitCmd(context.Background(), "/tmp", "status", "--porcelain")

	want := []string{"git", "--no-optional-locks", "status", "--porcelain"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("Args = %v, want %v", cmd.Args, want)
	}
	if cmd.Dir != "/tmp" {
		t.Errorf("Dir = %q, want /tmp", cmd.Dir)
	}
	// Nette afbraak bij context-cancel: eerst SIGTERM, na de gratieperiode
	// pas SIGKILL — anders blijven index.lock-bestanden achter.
	if cmd.Cancel == nil {
		t.Error("Cancel ontbreekt: context-cancel zou SIGTERM moeten sturen i.p.v. SIGKILL")
	}
	if cmd.WaitDelay != 2*time.Second {
		t.Errorf("WaitDelay = %v, want 2s", cmd.WaitDelay)
	}
	// Eigen procesgroep, zodat het cancel-signaal ook git-kinderen
	// (git-remote-http, ssh, hook-scripts) bereikt.
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("SysProcAttr.Setpgid ontbreekt: git-kinderen zouden een timeout als wees overleven")
	}
}

func TestRunWerktMetVlag(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skipf("git init niet beschikbaar: %v", err)
	}
	out, err := Run(context.Background(), dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "true" {
		t.Errorf("rev-parse = %q, want true", out)
	}
}
