package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/proto"
)

func TestUIMobileDirectoryControlsFitWithKeyboard(t *testing.T) {
	for _, size := range []struct{ width, height core.Points }{{320, 740}, {390, 844}, {740, 390}} {
		t.Run(fmt.Sprintf("%vx%v", size.width, size.height), func(t *testing.T) {
			root := func(cx *core.Scope) core.Node {
				a := newApp(cx)
				entries := make([]proto.HostDir, 40)
				for i := range entries {
					entries[i] = proto.HostDir{Name: fmt.Sprintf("folder-%02d", i), Path: fmt.Sprintf("/work/folder-%02d", i)}
				}
				a.dirs.Set(proto.HostDirs{Path: "/work", Parent: "/", Entries: entries})
				a.showDirs.Set(true)
				return a.dirsDialog(cx)
			}
			keyboard := core.Points(300)
			if size.height < 500 {
				keyboard = 100
			}
			app := nsgotest.Start(t, root, nsgotest.Environment(core.Environment{
				OS: core.OSAndroid, Idiom: core.IdiomPhone, KeyboardHeight: keyboard,
				SafeArea: core.Edges{Top: 24, Bottom: 16},
			}), nsgotest.Size(size.width, size.height))
			app.WaitFor("id:dirs-dialog", 5*time.Second)
			app.Advance(250 * time.Millisecond)
			for _, id := range []string{"dirs-up", "dirs-new-folder", "dirs-more", "dirs-use", "dirs-cancel"} {
				view := app.Find("id:" + id)
				frame := view.Frame
				for parent := view.Parent; parent != nil; parent = parent.Parent {
					frame.Origin.X += parent.Frame.Origin.X - parent.Offset.X
					frame.Origin.Y += parent.Frame.Origin.Y - parent.Offset.Y
				}
				if frame.Origin.X < 0 || frame.Origin.X+frame.Size.Width > size.width ||
					frame.Origin.Y < 24 || frame.Origin.Y+frame.Size.Height > size.height-keyboard {
					t.Fatalf("%s is outside usable screen: %v", id, frame)
				}
			}
			if frame := app.Find("id:dirs-entry-folder-00").Frame; frame.Size.Height < 44 {
				t.Fatalf("directory touch target is too small: %v", frame)
			}
			app.Press("id:dirs-more")
			app.WaitFor("id:dirs-home", 5*time.Second)
			app.WaitFor("id:dirs-refresh", 5*time.Second)
		})
	}
}
