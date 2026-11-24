package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
)

// Window controls can occupy either edge, and must remain clear as the
// sidebar and surfaces panel are opened and closed.
func TestUITitleBarWindowControls(t *testing.T) {
	_, ts, cfg := startEnv(t)
	tester, session := startAppAt(t, ts, cfg, 1280, 820, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	for _, layout := range []struct {
		name string
		bar  ui.TitleBar
	}{
		{"macOS", ui.TitleBar{Height: 56, Left: 80, Bottom: 34}},
		{"Windows", ui.TitleBar{Height: 56, Right: 138, Bottom: 56}},
		{"both edges", ui.TitleBar{Height: 68, Left: 80, Right: 72, Bottom: 68}},
		{"full screen", ui.TitleBar{}},
	} {
		t.Run(layout.name, func(t *testing.T) {
			tester.tt.SetTitleBar(layout.bar)
			for _, sidebar := range []bool{true, false} {
				for _, panel := range []bool{true, false} {
					session.sidebarOpen, session.rightOpen, session.tabs = sidebar, panel, nil
					tester.pump()
					label := "Toggle right panel"
					if panel {
						label = "Close right panel"
					}
					right, ok := tester.tt.Access(i18n.T(label))
					if !ok || right.Bounds.W < 28 || right.Bounds.X+right.Bounds.W > 1280-layout.bar.Right {
						t.Fatalf("sidebar=%v, panel=%v: %s overlaps window controls: %+v", sidebar, panel, label, right.Bounds)
					}
					if !sidebar {
						left, ok := tester.tt.Access(i18n.T("Toggle sidebar"))
						if !ok || left.Bounds.X < layout.bar.Left || left.Bounds.H < 28 {
							t.Fatalf("sidebar toggle overlaps window controls: %+v", left.Bounds)
						}
					} else {
						create, ok := tester.tt.Access(i18n.T("New thread"))
						if !ok || create.Bounds.X < layout.bar.Left || create.Bounds.H < 28 {
							t.Fatalf("new thread overlaps window controls: %+v", create.Bounds)
						}
					}
				}
			}
			// In the custom title bar, child buttons still take their click.
			session.sidebarOpen, session.rightOpen = true, false
			tester.pump()
			if err := tester.tt.Click(i18n.T("Toggle sidebar")); err != nil {
				t.Fatal(err)
			}
			if session.sidebarOpen {
				t.Fatal("title bar drag handle consumed the sidebar button click")
			}
			if err := tester.tt.Click(i18n.T("Toggle right panel")); err != nil {
				t.Fatal(err)
			}
			if !session.rightOpen {
				t.Fatal("title bar drag handle consumed the panel button click")
			}
		})
	}
}

func TestUITitleBarStandaloneScreens(t *testing.T) {
	for _, bar := range []ui.TitleBar{
		{},
		{Height: 56, Left: 80, Bottom: 34},
		{Height: 56, Right: 138, Bottom: 56},
	} {
		tt := ui.NewTester(func(c *ui.Context) {
			windowScreen(c, func() { ui.Button(c, "Screen content") })
		}, 640, 480)
		tt.SetTitleBar(bar)
		tt.Frame()
		content, ok := tt.Access("Screen content")
		if !ok {
			t.Fatal("standalone screen content is missing")
		}
		if bar.Height > 0 && content.Bounds.Y < bar.Bottom {
			t.Fatalf("standalone screen overlaps the title bar: %+v", content.Bounds)
		}
		if bar.Height == 0 && content.Bounds.Y > 0 {
			t.Fatal("Web or phone screen gained an unnecessary title bar")
		}
	}
}
