package main

import "time"

// menuGuard keeps a menu that opens under a press from closing again
// right away.
//
// A press that opens a menu rebuilds the tree it lives in, and the
// framework hands the press to the rebuilt view once more. A menu that
// toggles on its trigger therefore opens and closes under the reader's
// finger, and the sheet is gone before it can be read. The guard
// swallows that second close: the menu stays open until it is dismissed
// in its own right.
type menuGuard struct {
	openedAt time.Time
}

// change reports a menu's new state, keeping it open when the close
// follows the open closely enough to be the replayed press.
func (g *menuGuard) change(open bool, set func(bool)) {
	if open {
		g.openedAt = time.Now()
		set(true)
		return
	}
	if !g.openedAt.IsZero() && time.Since(g.openedAt) < 250*time.Millisecond {
		return
	}
	g.openedAt = time.Time{}
	set(false)
}
