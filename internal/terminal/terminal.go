// Package terminal runs interactive shells for the app's terminal
// surface. Sessions live in memory and stream through a WebSocket.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	gopty "github.com/aymanbagabas/go-pty"
	"github.com/tk/t3go/internal/id"
)

// ErrNotFound says no session has that id.
var ErrNotFound = errors.New("terminal session not found")

const maxBuffer = 256 << 10

// Session is one shell attached to a thread's checkout.
type Session struct {
	ID       string
	ThreadID string
	Title    string
	Shell    string
	Cwd      string

	mu       sync.Mutex
	term     gopty.Pty
	cmd      *gopty.Cmd
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
	byThread map[string][]*Session
}

// NewManager returns an empty manager.
func NewManager() *Manager {
	return &Manager{byID: map[string]*Session{}, byThread: map[string][]*Session{}}
}

// Ensure returns the thread's live session, starting a shell when it has
// none or the old one exited.
func (m *Manager) Ensure(threadID, dir, shell string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.byThread[threadID]
	for i := len(list) - 1; i >= 0; i-- {
		if !list[i].Exited() {
			return list[i], nil
		}
	}
	return m.createLocked(threadID, dir, shell)
}

// Create starts a new session even when the thread already has live ones.
func (m *Manager) Create(threadID, dir, shell string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createLocked(threadID, dir, shell)
}

func (m *Manager) createLocked(threadID, dir, shell string) (*Session, error) {
	list := m.byThread[threadID]
	shell, args := shellCommand(shell)
	session, err := start(id.New("term"), threadID, dir, shell, args)
	if err != nil {
		return nil, err
	}
	session.Title = fmt.Sprintf("Terminal %d", len(list)+1)
	m.byID[session.ID] = session
	m.byThread[threadID] = append(list, session)
	return session, nil
}

// List returns a thread's sessions in creation order.
func (m *Manager) List(threadID string) []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.byThread[threadID]
	return append([]*Session(nil), list...)
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
		list := m.byThread[session.ThreadID]
		next := list[:0]
		for _, candidate := range list {
			if candidate != session {
				next = append(next, candidate)
			}
		}
		if len(next) == 0 {
			delete(m.byThread, session.ThreadID)
		} else {
			m.byThread[session.ThreadID] = next
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
	list := m.byThread[threadID]
	delete(m.byThread, threadID)
	for _, session := range list {
		delete(m.byID, session.ID)
	}
	m.mu.Unlock()
	for _, session := range list {
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
	m.byID, m.byThread = map[string]*Session{}, map[string][]*Session{}
	m.mu.Unlock()
	for _, session := range sessions {
		session.Close()
	}
}

// shellCommand picks a login shell and its first arguments. Windows uses
// PowerShell when it is there and cmd otherwise, through ConPTY.
func shellCommand(shell string) (string, []string) {
	if shell == "" {
		if runtime.GOOS == "windows" {
			for _, name := range []string{"pwsh.exe", "powershell.exe"} {
				if path, err := exec.LookPath(name); err == nil {
					return path, []string{"-NoLogo"}
				}
			}
			if comspec := os.Getenv("COMSPEC"); comspec != "" {
				return comspec, nil
			}
			return "cmd.exe", nil
		}
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
	}
	if runtime.GOOS != "windows" {
		return shell, []string{"-l"}
	}
	switch strings.ToLower(strings.TrimSuffix(strings.ToLower(filepathBase(shell)), ".exe")) {
	case "pwsh", "powershell":
		return shell, []string{"-NoLogo"}
	}
	return shell, nil
}

func filepathBase(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		return path[i+1:]
	}
	return path
}

func start(sessionID, threadID, dir, shell string, args []string) (*Session, error) {
	term, err := gopty.New()
	if err != nil {
		return nil, err
	}
	cmd := term.Command(shell, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "T3GO_TERMINAL=1")
	if err := cmd.Start(); err != nil {
		_ = term.Close()
		return nil, err
	}
	_ = term.Resize(80, 24)
	session := &Session{ID: sessionID, ThreadID: threadID, Shell: shell, Cwd: dir,
		term: term, cmd: cmd, cols: 80, rows: 24, done: make(chan struct{}), subs: map[chan []byte]struct{}{}}
	go session.read()
	go session.wait()
	return session, nil
}

func (s *Session) read() {
	buf := make([]byte, 32<<10)
	for {
		n, err := s.term.Read(buf)
		if n > 0 {
			s.broadcast(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) wait() {
	_ = s.cmd.Wait()
	code := -1
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
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
	_ = s.term.Close()
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
	_, err := s.term.Write(data)
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
	return s.term.Resize(cols, rows)
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
	_ = s.term.Close()
	s.finish(-1)
}

// Cols and Rows are the pseudo-terminal's current size.
func (s *Session) Cols() int { s.mu.Lock(); defer s.mu.Unlock(); return int(s.cols) }
func (s *Session) Rows() int { s.mu.Lock(); defer s.mu.Unlock(); return int(s.rows) }
