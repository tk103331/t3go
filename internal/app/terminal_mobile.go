//go:build android || ios

package app

import (
	"errors"

	"github.com/egoist/mygo/ui"
)

// newTerminalSession opens a terminal over the connection to the
// environment's shell. A phone has no terminal library, so the terminal
// surface says so instead.
func newTerminalSession(conn *terminalConn) (terminalSession, error) {
	return nil, errors.New("this platform has no terminal")
}

// drawTerminal shows the shell of a session. A phone draws none.
func drawTerminal(c *ui.Context, session terminalSession) ui.Element {
	return ui.Spacer(c)
}
