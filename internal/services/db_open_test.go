package services

import (
	"net/url"
	"os/exec"
	"strings"
	"testing"
)

func TestBuildMySQLURL(t *testing.T) {
	got := BuildMySQLURL("127.0.0.1", 3306, "root", "secret", "dev_vanluykennl")
	want := "mysql://root:secret@127.0.0.1:3306/dev_vanluykennl"
	if got != want {
		t.Errorf("BuildMySQLURL = %q, wil %q", got, want)
	}
}

func TestBuildMySQLURLEscapesSpecialCharacters(t *testing.T) {
	got := BuildMySQLURL("127.0.0.1", 3306, "root", "p@ss:word/1", "mydb")

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", got, err)
	}
	if parsed.Scheme != "mysql" {
		t.Errorf("scheme = %q", parsed.Scheme)
	}
	if parsed.User.Username() != "root" {
		t.Errorf("user = %q", parsed.User.Username())
	}
	pw, ok := parsed.User.Password()
	if !ok || pw != "p@ss:word/1" {
		t.Errorf("password = %q, ok=%v; wil het oorspronkelijke wachtwoord terug na parse", pw, ok)
	}
	if parsed.Hostname() != "127.0.0.1" || parsed.Port() != "3306" {
		t.Errorf("host:port = %s:%s", parsed.Hostname(), parsed.Port())
	}
	if parsed.Path != "/mydb" {
		t.Errorf("path = %q", parsed.Path)
	}
}

func TestOpenInApp(t *testing.T) {
	orig := openCommand
	t.Cleanup(func() { openCommand = orig })

	var gotName string
	var gotArgs []string
	openCommand = func(name string, arg ...string) *exec.Cmd {
		gotName = name
		gotArgs = arg
		return exec.Command("/usr/bin/true")
	}

	if err := OpenInApp("Sequel Ace", "mysql://root:secret@127.0.0.1:3306/mydb"); err != nil {
		t.Fatalf("OpenInApp: %v", err)
	}
	if gotName != "open" {
		t.Errorf("commando = %q, wil open", gotName)
	}
	if len(gotArgs) != 3 || gotArgs[0] != "-a" || gotArgs[1] != "Sequel Ace" || gotArgs[2] != "mysql://root:secret@127.0.0.1:3306/mydb" {
		t.Errorf("argumenten = %v", gotArgs)
	}
}

func TestOpenInAppWrapsFailure(t *testing.T) {
	orig := openCommand
	t.Cleanup(func() { openCommand = orig })
	openCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("/usr/bin/false")
	}

	err := OpenInApp("Onbestaande App", "mysql://x")
	if err == nil || !strings.Contains(err.Error(), `kon "Onbestaande App" niet openen`) {
		t.Fatalf("verwachtte een foutmelding met de appnaam, kreeg: %v", err)
	}
}
