package ssh

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// startAuthServer is startTestServer met een instelbare auth-config, zodat
// wachtwoord- en keyboard-interactive-auth los getest kunnen worden. Kinsta biedt
// beide aan ("SSH key, password"), en sommige hosts alleen het tweede.
func startAuthServer(t *testing.T, configure func(*ssh.ServerConfig), h execHandler) *testServer {
	t.Helper()

	hostSigner := genSigner(t)
	srv := &testServer{hostPub: hostSigner.PublicKey()}

	cfg := &ssh.ServerConfig{}
	configure(cfg)
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.addr = ln.Addr().String()
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.handleConn(conn, cfg, h)
		}
	}()
	return srv
}

func TestPasswordAuth(t *testing.T) {
	dir := t.TempDir()
	srv := startAuthServer(t, func(cfg *ssh.ServerConfig) {
		cfg.PasswordCallback = func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == "geheim" {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("verkeerd wachtwoord")
		}
	}, func(_ string, _ []byte) (string, int) { return "wachtwoord-ok", 0 })
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "steinweg", Password: "geheim"}

	out, err := c.RunCommand(context.Background(), tgt, "echo")
	if err != nil {
		t.Fatalf("RunCommand met wachtwoord: %v", err)
	}
	if out != "wachtwoord-ok" {
		t.Errorf("stdout = %q", out)
	}
}

func TestKeyboardInteractiveAuth(t *testing.T) {
	dir := t.TempDir()
	srv := startAuthServer(t, func(cfg *ssh.ServerConfig) {
		// Alleen keyboard-interactive: geen PasswordCallback, dus de client moet
		// zelf op die methode terugvallen.
		cfg.KeyboardInteractiveCallback = func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			antwoorden, err := challenge("", "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(antwoorden) == 1 && antwoorden[0] == "geheim" {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("verkeerd wachtwoord")
		}
	}, func(_ string, _ []byte) (string, int) { return "interactief-ok", 0 })
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "steinweg", Password: "geheim"}

	out, err := c.RunCommand(context.Background(), tgt, "echo")
	if err != nil {
		t.Fatalf("RunCommand via keyboard-interactive: %v", err)
	}
	if out != "interactief-ok" {
		t.Errorf("stdout = %q", out)
	}
}

func TestVerkeerdWachtwoordGeeftDuidelijkeFout(t *testing.T) {
	dir := t.TempDir()
	srv := startAuthServer(t, func(cfg *ssh.ServerConfig) {
		cfg.PasswordCallback = func(_ ssh.ConnMetadata, _ []byte) (*ssh.Permissions, error) {
			return nil, errors.New("nope")
		}
	}, func(_ string, _ []byte) (string, int) { return "", 0 })
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "steinweg", Password: "fout"}

	_, err := c.RunCommand(context.Background(), tgt, "echo")
	if err == nil {
		t.Fatal("wil een fout bij een verkeerd wachtwoord")
	}
	// Het wachtwoord mag nooit in een foutmelding belanden: die gaat naar de UI.
	if strings.Contains(err.Error(), "fout\"") || strings.Contains(err.Error(), "password=fout") {
		t.Errorf("foutmelding bevat het wachtwoord: %v", err)
	}
}
