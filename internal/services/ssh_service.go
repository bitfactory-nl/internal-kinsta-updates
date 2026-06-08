package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"

	sshadapter "github.com/rdm/sites-tool/internal/adapters/ssh"
	"github.com/rdm/sites-tool/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// eventEmitter abstracts Wails event emission so the service can be tested.
// *application.EventManager satisfies it.
type eventEmitter interface {
	Emit(name string, data ...any) bool
}

// shellSession is the interactive session contract used by SSHService;
// *sshadapter.Session satisfies it.
type shellSession interface {
	Write(p []byte) error
	Resize(cols, rows int) error
	Close() error
	Done() <-chan struct{}
}

// shellOpener opens interactive shells; abstracted for testing.
type shellOpener interface {
	OpenShell(ctx context.Context, t sshadapter.Target, cols, rows int, onOutput func([]byte)) (shellSession, error)
}

// realOpener adapts *sshadapter.Client to shellOpener.
type realOpener struct{ c *sshadapter.Client }

func (r realOpener) OpenShell(ctx context.Context, t sshadapter.Target, cols, rows int, onOutput func([]byte)) (shellSession, error) {
	return r.c.OpenShell(ctx, t, cols, rows, onOutput)
}

// SSHService manages interactive SSH terminal sessions and streams their output
// to the frontend via Wails events ("ssh:<id>:data" and "ssh:<id>:exit").
type SSHService struct {
	emitter eventEmitter
	opener  shellOpener

	mu       sync.Mutex
	sessions map[string]shellSession
}

func NewSSHService() *SSHService {
	return &SSHService{
		opener:   realOpener{c: sshadapter.NewClient()},
		sessions: make(map[string]shellSession),
	}
}

// SetApp injects the Wails app reference (called after app creation).
func (s *SSHService) SetApp(app *application.App) {
	s.emitter = app.Event
}

// OpenSession starts an interactive shell and returns its session ID. Terminal
// output arrives as base64 strings on the "ssh:<id>:data" event; "ssh:<id>:exit"
// fires when the session ends.
func (s *SSHService) OpenSession(target domain.SSHTarget, cols, rows int) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	dataEvent := "ssh:" + id + ":data"
	exitEvent := "ssh:" + id + ":exit"

	tgt := sshadapter.Target{Host: target.Host, Port: target.Port, User: target.User}
	sess, err := s.opener.OpenShell(context.Background(), tgt, cols, rows, func(b []byte) {
		if s.emitter != nil {
			s.emitter.Emit(dataEvent, base64.StdEncoding.EncodeToString(b))
		}
	})
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	go func() {
		<-sess.Done()
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
		if s.emitter != nil {
			s.emitter.Emit(exitEvent, nil)
		}
	}()

	return id, nil
}

// Write sends keyboard input to the session.
func (s *SSHService) Write(sessionID, data string) error {
	sess, ok := s.get(sessionID)
	if !ok {
		return fmt.Errorf("sessie %q niet gevonden", sessionID)
	}
	return sess.Write([]byte(data))
}

// Resize updates the remote PTY size.
func (s *SSHService) Resize(sessionID string, cols, rows int) error {
	sess, ok := s.get(sessionID)
	if !ok {
		return fmt.Errorf("sessie %q niet gevonden", sessionID)
	}
	return sess.Resize(cols, rows)
}

// Close terminates the session. Unknown IDs are a no-op.
func (s *SSHService) Close(sessionID string) error {
	sess, ok := s.get(sessionID)
	if !ok {
		return nil
	}
	return sess.Close()
}

func (s *SSHService) get(id string) (shellSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sessie-id genereren: %w", err)
	}
	return hex.EncodeToString(b), nil
}
