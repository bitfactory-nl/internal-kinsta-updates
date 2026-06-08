package services

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/domain"
)

type fakeShellSession struct {
	mu      sync.Mutex
	writes  []string
	resizes [][2]int
	done    chan struct{}
	closed  bool
}

func newFakeShellSession() *fakeShellSession {
	return &fakeShellSession{done: make(chan struct{})}
}

func (f *fakeShellSession) Write(p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, string(p))
	return nil
}

func (f *fakeShellSession) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizes = append(f.resizes, [2]int{cols, rows})
	return nil
}

func (f *fakeShellSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	return nil
}

func (f *fakeShellSession) Done() <-chan struct{} { return f.done }

type fakeOpener struct {
	sess     *fakeShellSession
	onOutput func([]byte)
}

func (o *fakeOpener) OpenShell(_ context.Context, _ sshadapter.Target, _, _ int, onOutput func([]byte)) (shellSession, error) {
	o.onOutput = onOutput
	return o.sess, nil
}

type emitted struct {
	name string
	data []any
}

type fakeEmitter struct {
	mu     sync.Mutex
	events []emitted
}

func (e *fakeEmitter) Emit(name string, data ...any) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, emitted{name: name, data: data})
	return true
}

func (e *fakeEmitter) find(name string) (emitted, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev.name == name {
			return ev, true
		}
	}
	return emitted{}, false
}

func newTestSSHService() (*SSHService, *fakeOpener, *fakeEmitter) {
	op := &fakeOpener{sess: newFakeShellSession()}
	em := &fakeEmitter{}
	return &SSHService{emitter: em, opener: op, sessions: make(map[string]shellSession)}, op, em
}

func TestSSHServiceOpenSessionStreamsOutput(t *testing.T) {
	svc, op, em := newTestSSHService()

	id, err := svc.OpenSession(domain.SSHTarget{Host: "h", Port: 22, User: "u"}, 80, 24)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if id == "" {
		t.Fatal("empty session id")
	}

	// Output from the shell is emitted as base64 on ssh:<id>:data.
	op.onOutput([]byte("output\n"))
	ev, ok := em.find("ssh:" + id + ":data")
	if !ok {
		t.Fatalf("no data event emitted; events=%v", em.events)
	}
	if len(ev.data) != 1 || ev.data[0] != base64.StdEncoding.EncodeToString([]byte("output\n")) {
		t.Errorf("data payload = %v, want base64 of output", ev.data)
	}
}

func TestSSHServiceWriteAndResize(t *testing.T) {
	svc, op, _ := newTestSSHService()
	id, err := svc.OpenSession(domain.SSHTarget{Host: "h"}, 80, 24)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	if err := svc.Write(id, "ls\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := svc.Resize(id, 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	op.sess.mu.Lock()
	defer op.sess.mu.Unlock()
	if len(op.sess.writes) != 1 || op.sess.writes[0] != "ls\n" {
		t.Errorf("writes = %v, want [ls\\n]", op.sess.writes)
	}
	if len(op.sess.resizes) != 1 || op.sess.resizes[0] != [2]int{120, 40} {
		t.Errorf("resizes = %v, want [[120 40]]", op.sess.resizes)
	}
}

func TestSSHServiceCloseEmitsExit(t *testing.T) {
	svc, _, em := newTestSSHService()
	id, err := svc.OpenSession(domain.SSHTarget{Host: "h"}, 80, 24)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	if err := svc.Close(id); err != nil {
		t.Fatalf("Close: %v", err)
	}

	exitName := "ssh:" + id + ":exit"
	deadline := time.After(2 * time.Second)
	for {
		if _, ok := em.find(exitName); ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("exit event not emitted after Close")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Session should be removed; further writes fail.
	if err := svc.Write(id, "x"); err == nil {
		t.Error("expected error writing to closed session")
	}
}

func TestSSHServiceUnknownSession(t *testing.T) {
	svc, _, _ := newTestSSHService()
	if err := svc.Write("nope", "x"); err == nil {
		t.Error("expected error for unknown session write")
	}
	if err := svc.Resize("nope", 80, 24); err == nil {
		t.Error("expected error for unknown session resize")
	}
	if err := svc.Close("nope"); err != nil {
		t.Errorf("Close unknown should be no-op, got %v", err)
	}
}
