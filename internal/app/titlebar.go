package app

import "github.com/egoist/mygo/ui"

// The workspace's existing top row is the title bar. Native window
// controls stay above it; their platform-reported bounds include layouts
// with controls on either edge and disappear in full screen and on Web.
func windowTitleBarHeight(c *ui.Context) float32 {
	bar := c.TitleBar()
	return max(44, bar.Height, bar.Bottom)
}

// Screens without a workspace toolbar still need a drag handle and room
// for the native controls. There is no extra bar on Web or phones.
func windowTitleBar(c *ui.Context) {
	bar := c.TitleBar()
	if bar.Height == 0 && bar.Left == 0 && bar.Right == 0 && bar.Bottom == 0 {
		return
	}
	ui.Row(c).FillWidth().Height(windowTitleBarHeight(c)).Shrink(0).
		Padding(6, bar.Right+12, 6, bar.Left+12).AlignItems(ui.Center).
		Background(c.Theme().Surface).BorderWidth(0, 0, 1, 0).
		BorderColor(c.Theme().Border).DragWindow().Children(func() {
		brandWordmark(c, 19)
	})
}

func windowScreen(c *ui.Context, build func()) {
	ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
		windowTitleBar(c)
		ui.Box(c).Grow(1).FillWidth().Clip().Children(build)
	})
}
