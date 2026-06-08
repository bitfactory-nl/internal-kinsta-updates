package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// execHandler receives the requested command and the bytes the client sent on
// stdin, and returns the stdout to write back plus an exit code.
type execHandler func(cmd string, stdin []byte) (stdout string, exit int)

// testServer is an in-process SSH server used to exercise the client.
type testServer struct {
	addr     string
	hostPub  ssh.PublicKey
	mu       sync.Mutex
	lastCmd  string
	lastBody []byte
}

func startTestServer(t *testing.T, authorized ssh.PublicKey, h execHandler) *testServer {
	t.Helper()

	hostSigner := genSigner(t)
	srv := &testServer{hostPub: hostSigner.PublicKey()}

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if authorized != nil && string(key.Marshal()) == string(authorized.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
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

func (s *testServer) handleConn(conn net.Conn, cfg *ssh.ServerConfig, h execHandler) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go s.handleSession(ch, creqs, h)
	}
}

func (s *testServer) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, h execHandler) {
	for req := range reqs {
		switch req.Type {
		case "exec":
			var m struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &m)
			_ = req.Reply(true, nil)

			body, _ := io.ReadAll(ch)
			out, code := h(m.Command, body)

			s.mu.Lock()
			s.lastCmd = m.Command
			s.lastBody = body
			s.mu.Unlock()

			_, _ = ch.Write([]byte(out))
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(code)}))
			_ = ch.Close()
			return
		case "pty-req", "window-change":
			_ = req.Reply(true, nil)
		case "shell":
			_ = req.Reply(true, nil)
			// Echo server: copy the channel's input back to its output.
			go func() {
				_, _ = io.Copy(ch, ch)
				_ = ch.Close()
			}()
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func (s *testServer) command() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastCmd
}

func (s *testServer) body() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBody
}

func genSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer
}

// writeIdentity generates a key, writes the private key as a PEM identity file,
// and returns the path plus the matching public key.
func writeIdentity(t *testing.T, dir string) (path string, pub ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	blk, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	path = filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(blk), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return path, signer.PublicKey()
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return host, port
}

// noAgent is an agent dialer that always fails (simulates an empty/absent agent).
func noAgent() (net.Conn, error) { return nil, errors.New("no agent") }

func TestRunCommand(t *testing.T) {
	dir := t.TempDir()
	idPath, pub := writeIdentity(t, dir)
	srv := startTestServer(t, pub, func(cmd string, _ []byte) (string, int) {
		return "hello\n", 0
	})
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(
		WithKnownHostsPath(filepath.Join(dir, "known_hosts")),
		WithAgentDialer(noAgent),
		WithDialTimeout(5*time.Second),
	)
	tgt := Target{Host: host, Port: port, User: "wp", IdentityFile: idPath}

	out, err := c.RunCommand(context.Background(), tgt, "wp plugin list")
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if out != "hello\n" {
		t.Errorf("stdout = %q, want %q", out, "hello\n")
	}
	if got := srv.command(); got != "wp plugin list" {
		t.Errorf("server saw cmd %q, want %q", got, "wp plugin list")
	}
}

func TestRunCommandNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	idPath, pub := writeIdentity(t, dir)
	srv := startTestServer(t, pub, func(_ string, _ []byte) (string, int) {
		return "", 1
	})
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "wp", IdentityFile: idPath}

	if _, err := c.RunCommand(context.Background(), tgt, "false"); err == nil {
		t.Fatal("expected error on non-zero exit, got nil")
	}
}

func TestUpload(t *testing.T) {
	dir := t.TempDir()
	idPath, pub := writeIdentity(t, dir)
	srv := startTestServer(t, pub, func(_ string, _ []byte) (string, int) {
		return "", 0
	})
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "wp", IdentityFile: idPath}

	payload := []byte("PK\x03\x04 zip bytes")
	if err := c.Upload(context.Background(), tgt, "/tmp/plugin.zip", payload); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if got := srv.body(); string(got) != string(payload) {
		t.Errorf("server received %q, want %q", got, payload)
	}
	if got := srv.command(); got != "cat > '/tmp/plugin.zip'" {
		t.Errorf("server saw cmd %q, want cat redirect", got)
	}
}

func TestUploadRejectsUnsafePath(t *testing.T) {
	c := NewClient(WithKnownHostsPath(filepath.Join(t.TempDir(), "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: "127.0.0.1", Port: 22, User: "wp"}
	err := c.Upload(context.Background(), tgt, "/tmp/evil';rm -rf /", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "ongeldig remote pad") {
		t.Fatalf("expected unsafe-path error, got %v", err)
	}
}

func TestTOFURecordsAndVerifies(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	idPath, pub := writeIdentity(t, dir)
	srv := startTestServer(t, pub, func(_ string, _ []byte) (string, int) { return "ok", 0 })
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(WithKnownHostsPath(khPath), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "wp", IdentityFile: idPath}

	// First connect: unknown host → recorded.
	if _, err := c.RunCommand(context.Background(), tgt, "echo"); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	data, err := os.ReadFile(khPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Fatal("known_hosts was not written on first connect")
	}

	// Second connect: host now known and matching → still succeeds.
	if _, err := c.RunCommand(context.Background(), tgt, "echo"); err != nil {
		t.Fatalf("second connect: %v", err)
	}
}

func TestHostKeyMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	idPath, pub := writeIdentity(t, dir)
	srv := startTestServer(t, pub, func(_ string, _ []byte) (string, int) { return "ok", 0 })
	host, port := splitHostPort(t, srv.addr)

	// Pre-populate known_hosts with a DIFFERENT host key for this addr.
	wrong := genSigner(t).PublicKey()
	c := NewClient(WithKnownHostsPath(khPath), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "wp", IdentityFile: idPath}
	if err := c.appendKnownHost(tgt.addr(), nil, wrong); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}

	if _, err := c.RunCommand(context.Background(), tgt, "echo"); err == nil {
		t.Fatal("expected host key mismatch to be rejected")
	}
}

func TestAgentAuth(t *testing.T) {
	dir := t.TempDir()
	// Key lives only in the in-memory agent, no identity file.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("agent add: %v", err)
	}

	srv := startTestServer(t, signer.PublicKey(), func(_ string, _ []byte) (string, int) {
		return "agent-ok", 0
	})
	host, port := splitHostPort(t, srv.addr)

	dialer := func() (net.Conn, error) {
		c1, c2 := net.Pipe()
		go func() { _ = agent.ServeAgent(keyring, c2) }()
		return c1, nil
	}

	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(dialer))
	tgt := Target{Host: host, Port: port, User: "wp"} // no IdentityFile

	out, err := c.RunCommand(context.Background(), tgt, "echo")
	if err != nil {
		t.Fatalf("RunCommand via agent: %v", err)
	}
	if out != "agent-ok" {
		t.Errorf("stdout = %q, want %q", out, "agent-ok")
	}
}

func TestNoAuthAvailable(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: "127.0.0.1", Port: 2222, User: "wp"} // no agent, no identity file

	_, err := c.RunCommand(context.Background(), tgt, "echo")
	if err == nil || !strings.Contains(err.Error(), "geen SSH-authenticatie") {
		t.Fatalf("expected no-auth error, got %v", err)
	}
}

func TestOpenShell(t *testing.T) {
	dir := t.TempDir()
	idPath, pub := writeIdentity(t, dir)
	srv := startTestServer(t, pub, func(_ string, _ []byte) (string, int) { return "", 0 })
	host, port := splitHostPort(t, srv.addr)

	c := NewClient(WithKnownHostsPath(filepath.Join(dir, "known_hosts")), WithAgentDialer(noAgent))
	tgt := Target{Host: host, Port: port, User: "wp", IdentityFile: idPath}

	out := make(chan []byte, 64)
	sess, err := c.OpenShell(context.Background(), tgt, 80, 24, func(b []byte) {
		out <- b
	})
	if err != nil {
		t.Fatalf("OpenShell: %v", err)
	}
	defer sess.Close()

	if err := sess.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sess.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	// The echo server returns what we sent; collect until we see it or time out.
	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for !strings.Contains(got.String(), "hello") {
		select {
		case b := <-out:
			got.Write(b)
		case <-deadline:
			t.Fatalf("timed out waiting for echo; got %q", got.String())
		}
	}

	// Closing ends the session; Done must fire.
	_ = sess.Close()
	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session Done not signalled after Close")
	}
}
