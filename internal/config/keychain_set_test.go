package config

import (
	"errors"
	"strings"
	"testing"
)

func TestAddPasswordCommandEscapet(t *testing.T) {
	got, err := addPasswordCommand("svc", "wp:klant-a", `ab cd"ef\gh`)
	if err != nil {
		t.Fatal(err)
	}
	want := `add-generic-password -s "svc" -a "wp:klant-a" -w "ab cd\"ef\\gh" -U` + "\n"
	if got != want {
		t.Fatalf("command\n got: %q\nwant: %q", got, want)
	}
}

func TestAddPasswordCommandWeigertRegeleinde(t *testing.T) {
	for _, pw := range []string{"a\nb", "a\rb"} {
		if _, err := addPasswordCommand("svc", "acc", pw); err == nil {
			t.Fatalf("verwacht fout voor %q", pw)
		}
	}
	if _, err := addPasswordCommand("svc", "a\nb", "pw"); err == nil {
		t.Fatal("verwacht fout voor account met regeleinde")
	}
}

func TestKeychainSetInGeeftSecretViaStdin(t *testing.T) {
	var input string
	orig := securityStdin
	securityStdin = func(in string) error { input = in; return nil }
	t.Cleanup(func() { securityStdin = orig })

	if err := keychainSetIn("svc", "acc", "geheim-123"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(input, `-w "geheim-123"`) {
		t.Fatalf("secret niet in stdin-invoer: %q", input)
	}
}

func TestSecurityResult(t *testing.T) {
	if err := securityResult([]byte(" \n"), nil); err != nil {
		t.Fatalf("lege uitvoer met exit 0 hoort geen fout te zijn: %v", err)
	}
	err := securityResult([]byte("security: fout een\nregel twee\n"), nil)
	if err == nil || err.Error() != "security: fout een" {
		t.Fatalf("exit 0 met uitvoer: %v", err)
	}
	exit := errors.New("exit status 1")
	err = securityResult([]byte("\nSecKeychainItemCreate: bestaat al\nmeer\n"), exit)
	if !errors.Is(err, exit) {
		t.Fatalf("oorspronkelijke fout niet gewrapt: %v", err)
	}
	if err.Error() != "exit status 1: SecKeychainItemCreate: bestaat al" {
		t.Fatalf("melding %q", err.Error())
	}
	if err := securityResult(nil, exit); err != exit {
		t.Fatalf("zonder uitvoer hoort de fout zelf terug te komen: %v", err)
	}
}
