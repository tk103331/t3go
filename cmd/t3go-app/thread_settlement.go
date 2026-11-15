package main

import (
	"context"
	"errors"

	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

func (a *app) settleMenuItem(thread proto.Thread, rowID string) ui.MenuItem {
	label := "Settle thread"
	if thread.Settled {
		label = "Unsettle thread"
	}
	return ui.MenuItem{Text: label, TestID: "settle-thread-" + rowID,
		OnSelect: func() { a.setThreadSettled(thread.ID, !thread.Settled) }}
}

func (a *app) setThreadSettled(id string, settled bool) {
	if id == "" {
		return
	}
	a.connErr.Set("")
	var updated proto.Thread
	a.call(func(ctx context.Context, c *client.Client) error {
		var err error
		updated, err = c.UpdateThread(ctx, id, map[string]any{"settled": settled})
		if err == nil && updated.Settled != settled {
			return errors.New("This environment needs a newer T3Go server to support Settle.")
		}
		return err
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
			return
		}
		a.upsertThread(updated)
		detail := a.detail.GetUntracked()
		if detail.Thread.ID == id {
			detail.Thread = updated
			a.detail.Set(detail)
		}
	})
}
