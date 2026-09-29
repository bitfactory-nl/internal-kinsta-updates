package config

import (
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
