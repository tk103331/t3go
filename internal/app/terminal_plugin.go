//go:build !(android || ios)

package app

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// newTerminalSession opens a terminal over the connection to the
// environment's shell.
func newTerminalSession(conn *terminalConn) (terminalSession, error) {
	t, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// drawTerminal shows the shell of a session.
func drawTerminal(c *ui.Context, session terminalSession) ui.Element {
	t, ok := session.(*terminal.Terminal)
	if !ok || t == nil {
		return ui.Spacer(c)
	}
	return terminal.View(c, t)
}

// UseTheme is not needed: the terminal takes the colors of the window it
// shows in.
func (c *terminalConn) UseTheme(theme *terminal.Theme) error { return nil }
