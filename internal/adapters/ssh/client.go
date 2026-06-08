// Package ssh provides a minimal SSH client for running wp-cli commands and
// uploading files to Kinsta/VPS hosts. Authentication prefers the local
// ssh-agent and falls back to a configured identity file. Unknown host keys
// are trusted on first use (TOFU) and verified strictly on later connects.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Target identifies an SSH host.
type Target struct {
	Host         string
	Port         int
	User         string
	IdentityFile string // optional; used when the ssh-agent has no usable key
}

func (t Target) addr() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

// Client dials SSH hosts using agent + identity-file auth and TOFU host keys.
type Client struct {
	knownHostsPath string
	agentDialer    func() (net.Conn, error)
	dialTimeout    time.Duration

	mu sync.Mutex // serializes known_hosts appends
}

// Option configures a Client.
type Option func(*Client)

// WithKnownHostsPath overrides the known_hosts file location.
func WithKnownHostsPath(p string) Option { return func(c *Client) { c.knownHostsPath = p } }

// WithAgentDialer overrides how the ssh-agent connection is obtained.
func WithAgentDialer(d func() (net.Conn, error)) Option {
	return func(c *Client) { c.agentDialer = d }
}

// WithDialTimeout overrides the TCP/handshake timeout.
func WithDialTimeout(d time.Duration) Option { return func(c *Client) { c.dialTimeout = d } }

// NewClient builds a Client with sane defaults: the user's ~/.ssh/known_hosts,
// the local ssh-agent via SSH_AUTH_SOCK, and a 15s dial timeout.
func NewClient(opts ...Option) *Client {
	c := &Client{
		knownHostsPath: defaultKnownHosts(),
		agentDialer:    defaultAgentDialer,
		dialTimeout:    15 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func defaultKnownHosts() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func defaultAgentDialer() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("geen ssh-agent (SSH_AUTH_SOCK is leeg)")
	}
	return net.Dial("unix", sock)
}

// RunCommand opens a session, runs cmd, and returns its stdout. A non-zero exit
// status is returned as an error including stderr.
func (c *Client) RunCommand(ctx context.Context, t Target, cmd string) (string, error) {
	client, closeFn, err := c.dial(ctx, t)
	if err != nil {
		return "", err
	}
	defer closeFn()

	sess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh sessie: %w", err)
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()

	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return stdout.String(), ctx.Err()
	case err := <-done:
		if err != nil {
			return stdout.String(), fmt.Errorf("commando mislukt: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	}
}

// Upload streams data to remotePath on the host (via `cat >`), avoiding an
// sftp dependency. remotePath must not contain a single quote or newline.
func (c *Client) Upload(ctx context.Context, t Target, remotePath string, data []byte) error {
	if strings.ContainsAny(remotePath, "'\n") {
		return fmt.Errorf("ongeldig remote pad: %q", remotePath)
	}
	client, closeFn, err := c.dial(ctx, t)
	if err != nil {
		return err
	}
	defer closeFn()

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh sessie: %w", err)
	}
	defer sess.Close()

	sess.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	sess.Stderr = &stderr

	cmd := fmt.Sprintf("cat > '%s'", remotePath)
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()

	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("upload mislukt: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
}

func (c *Client) dial(ctx context.Context, t Target) (*ssh.Client, func(), error) {
	methods, cleanup, err := c.authMethods(t)
	if err != nil {
		return nil, nil, err
	}
	hkcb, err := c.hostKeyCallback()
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            t.User,
		Auth:            methods,
		HostKeyCallback: hkcb,
		Timeout:         c.dialTimeout,
	}

	d := net.Dialer{Timeout: c.dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", t.addr())
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("verbinden met %s: %w", t.addr(), err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, t.addr(), cfg)
	if err != nil {
		_ = conn.Close()
		cleanup()
		return nil, nil, fmt.Errorf("ssh handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	return client, func() { _ = client.Close(); cleanup() }, nil
}

// authMethods assembles auth: ssh-agent first, then the identity file. cleanup
// closes the agent connection and must be called once dialing is finished.
func (c *Client) authMethods(t Target) ([]ssh.AuthMethod, func(), error) {
	var methods []ssh.AuthMethod
	cleanup := func() {}

	if c.agentDialer != nil {
		if conn, err := c.agentDialer(); err == nil {
			ag := agent.NewClient(conn)
			methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
			cleanup = func() { _ = conn.Close() }
		}
	}

	if t.IdentityFile != "" {
		signer, err := loadIdentity(t.IdentityFile)
		if err != nil {
			if len(methods) == 0 {
				cleanup()
				return nil, func() {}, fmt.Errorf("identity file: %w", err)
			}
			// agent available; ignore the unusable identity file
		} else {
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}

	if len(methods) == 0 {
		cleanup()
		return nil, func() {}, errors.New("geen SSH-authenticatie beschikbaar: laad een sleutel in ssh-agent of stel een identity file in")
	}
	return methods, cleanup, nil
}

func loadIdentity(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		var pmErr *ssh.PassphraseMissingError
		if errors.As(err, &pmErr) {
			return nil, fmt.Errorf("sleutel %s heeft een passphrase; laad hem in ssh-agent", path)
		}
		return nil, err
	}
	return signer, nil
}

// hostKeyCallback returns a TOFU verifier: unknown hosts are recorded and
// accepted; later connects are verified strictly and mismatches are rejected.
func (c *Client) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if c.knownHostsPath == "" {
		return nil, errors.New("known_hosts pad niet ingesteld")
	}
	if err := ensureFile(c.knownHostsPath); err != nil {
		return nil, fmt.Errorf("known_hosts voorbereiden: %w", err)
	}
	verify, err := knownhosts.New(c.knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("known_hosts laden: %w", err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := verify(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			return c.appendKnownHost(hostname, remote, key)
		}
		return err
	}, nil
}

func (c *Client) appendKnownHost(hostname string, remote net.Addr, key ssh.PublicKey) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	addrs := []string{knownhosts.Normalize(hostname)}
	if remote != nil {
		if ra := knownhosts.Normalize(remote.String()); ra != addrs[0] {
			addrs = append(addrs, ra)
		}
	}
	line := knownhosts.Line(addrs, key)

	f, err := os.OpenFile(c.knownHostsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("known_hosts bijwerken: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("known_hosts schrijven: %w", err)
	}
	return nil
}

func ensureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}
