// Package terminal runs interactive shells for the app's terminal
// surface. Sessions live in memory and stream through a WebSocket.
package terminal

import (
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/tk/t3go/internal/id"
)

// ErrNotFound says no session has that id.
var ErrNotFound = errors.New("terminal session not found")

const maxBuffer = 256 << 10

// Session is one shell attached to a thread's checkout.
type Session struct {
	ID       string
	ThreadID string
	Shell    string
	Cwd      string

	mu       sync.Mutex
	file     *os.File
	cmd      *exec.Cmd
	output   []byte
	cols     uint16
	rows     uint16
	exited   bool
	exitCode int
	done     chan struct{}
	subs     map[chan []byte]struct{}
}

// Manager owns the environment's terminal sessions.
type Manager struct {
	mu       sync.Mutex
	byID     map[string]*Session
	byThread map[string]*Session
}

// NewManager returns an empty manager.
func NewManager() *Manager {
	return &Manager{byID: map[string]*Session{}, byThread: map[string]*Session{}}
}

// Ensure returns the thread's live session, starting a shell when it has
// none or the old one exited.
func (m *Manager) Ensure(threadID, dir, shell string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old := m.byThread[threadID]; old != nil && !old.Exited() {
		return old, nil
	} else if old != nil {
		delete(m.byID, old.ID)
		delete(m.byThread, threadID)
	}
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	session, err := start(id.New("term"), threadID, dir, shell)
	if err != nil {
		return nil, err
	}
	m.byID[session.ID] = session
	m.byThread[threadID] = session
	return session, nil
}

// Get returns a session by id.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.byID[id]
	return session, ok
}

// Close kills one session.
func (m *Manager) Close(id string) {
	m.mu.Lock()
	session := m.byID[id]
	if session != nil {
		delete(m.byID, id)
		if m.byThread[session.ThreadID] == session {
			delete(m.byThread, session.ThreadID)
		}
	}
	m.mu.Unlock()
	if session != nil {
		session.Close()
	}
}

// CloseThread kills the session attached to a thread.
func (m *Manager) CloseThread(threadID string) {
	m.mu.Lock()
	session := m.byThread[threadID]
	if session != nil {
		delete(m.byThread, threadID)
		delete(m.byID, session.ID)
	}
	m.mu.Unlock()
	if session != nil {
		session.Close()
	}
}

// Shutdown kills every session.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.byID))
	for _, session := range m.byID {
		sessions = append(sessions, session)
	}
	m.byID, m.byThread = map[string]*Session{}, map[string]*Session{}
	m.mu.Unlock()
	for _, session := range sessions {
		session.Close()
	}
}

func start(sessionID, threadID, dir, shell string) (*Session, error) {
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "T3GO_TERMINAL=1")
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		return nil, err
	}
	session := &Session{ID: sessionID, ThreadID: threadID, Shell: shell, Cwd: dir,
		file: file, cmd: cmd, cols: 80, rows: 24, done: make(chan struct{}), subs: map[chan []byte]struct{}{}}
	go session.read()
	go session.wait()
	return session, nil
}

func (s *Session) read() {
	buf := make([]byte, 32<<10)
	for {
		n, err := s.file.Read(buf)
		if n > 0 {
			s.broadcast(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) wait() {
	err := s.cmd.Wait()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		code = -1
	}
	s.finish(code)
}

func (s *Session) broadcast(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output = append(s.output, chunk...)
	if len(s.output) > maxBuffer {
		s.output = append([]byte(nil), s.output[len(s.output)-maxBuffer:]...)
	}
	for ch := range s.subs {
		data := append([]byte(nil), chunk...)
		select {
		case ch <- data:
		default:
		}
	}
}

func (s *Session) finish(code int) {
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return
	}
	s.exited = true
	s.exitCode = code
	close(s.done)
	for ch := range s.subs {
		close(ch)
	}
	s.subs = map[chan []byte]struct{}{}
	s.mu.Unlock()
	_ = s.file.Close()
}

// Exited reports whether the shell has ended.
func (s *Session) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// ExitCode is the shell's last exit code.
func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// Write sends input to the shell.
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return errors.New("terminal has exited")
	}
	_, err := s.file.Write(data)
	return err
}

// Resize changes the pseudo-terminal's size.
func (s *Session) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > 500 || rows > 200 {
		return errors.New("invalid terminal size")
	}
	s.mu.Lock()
	s.cols, s.rows = uint16(cols), uint16(rows)
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return errors.New("terminal has exited")
	}
	return pty.Setsize(s.file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Subscribe returns the current output and a channel of later output. The
// channel closes when the session ends; cancel detaches the subscriber.
func (s *Session) Subscribe() (snapshot []byte, out <-chan []byte, done <-chan struct{}, cancel func()) {
	s.mu.Lock()
	snapshot = append([]byte(nil), s.output...)
	ch := make(chan []byte, 512)
	if !s.exited {
		s.subs[ch] = struct{}{}
	} else {
		close(ch)
	}
	done = s.done
	s.mu.Unlock()
	return snapshot, ch, done, func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
}

// Close ends the shell and every subscriber.
func (s *Session) Close() {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.file.Close()
	s.finish(-1)
}

// Cols and Rows are the pseudo-terminal's current size.
func (s *Session) Cols() int { s.mu.Lock(); defer s.mu.Unlock(); return int(s.cols) }
func (s *Session) Rows() int { s.mu.Lock(); defer s.mu.Unlock(); return int(s.rows) }
