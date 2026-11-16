package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

// Root is built once. The connection, its event stream and the open
// thread live in app, so rebuilding a responsive branch keeps them.
func Root() ui.Node {
	a := newApp()
	signal.OnCleanup(a.close)
	ui.OnOpenURL(a.handleIncomingURL)
	return ui.View(ui.ViewProps{
		Style: ui.Style{Grow: 1, Background: colors.Background},
		Keys: ui.KeyMap{
			ui.Cmd("k"):                 a.openPalette,
			ui.Cmd("f"):                 a.openSearch,
			ui.Cmd("n"):                 func() { a.openNewThread("") },
			ui.Cmd("n").With(ui.Shift):  func() { a.showNewProject.Set(true) },
			ui.Cmd(","):                 func() { a.openSettings(settingsGeneral) },
			ui.Cmd("\\").With(ui.Shift): func() { a.openSurface(a.rightTab.Peek()) },
		},
	},
		ui.SafeArea(ui.Style{Grow: 1},
			ui.Show(a.connected,
				func() ui.Node { return mainView(a) },
				func() ui.Node { return connectView(a, true) },
			),
		),
		ui.Sheet(ui.SheetProps{Open: a.showConnect, Content: func() ui.Node { return connectView(a, false) }}),
		ui.Sheet(ui.SheetProps{Open: a.showConnections, Content: func() ui.Node { return connectionsView(a) }}),
		ui.Sheet(ui.SheetProps{Open: a.showNewThread, Content: func() ui.Node { return newThreadView(a) }}),
		ui.Sheet(ui.SheetProps{Open: a.showNewProject, Content: func() ui.Node { return newProjectView(a) }}),
		ui.Sheet(ui.SheetProps{Open: a.showSettings, Content: func() ui.Node { return settingsView(a) }}),
		ui.Sheet(ui.SheetProps{Open: a.showCustomModel, Content: func() ui.Node { return customModelView(a) }}),
		ui.Sheet(ui.SheetProps{Open: a.showSearch, Content: func() ui.Node { return searchView(a) }}),
		ui.Sheet(ui.SheetProps{Open: a.showPalette, Content: func() ui.Node { return paletteView(a) }}),
		ui.Sheet(ui.SheetProps{Open: a.showPRInbox, Content: func() ui.Node { return prInboxView(a) }}),
		ui.Show(narrowScreen(), func() ui.Node {
			return ui.Sheet(ui.SheetProps{Open: a.rightOpen, Content: func() ui.Node { return surfacePanel(a, true) }})
		}),
	)
}

func narrowScreen() signal.Accessor[bool] {
	env := ui.UseEnvironment()
	return signal.Func(func() bool {
		e := env.Get()
		return e.WidthClass == ui.SizeClassCompact || e.Width < 760
	})
}

func mainView(a *app) ui.Node {
	return ui.Column(ui.Style{Grow: 1},
		ui.Show(signal.Func(func() bool { return a.notice.Get() != "" }), func() ui.Node { return noticeBanner(a) }),
		ui.Show(narrowScreen(),
			func() ui.Node {
				return ui.Show(signal.Func(func() bool { return a.selected.Get() != "" }),
					func() ui.Node { return conversation(a, true) },
					func() ui.Node { return sidebar(a, true) },
				)
			},
			func() ui.Node {
				return ui.Row(ui.Style{Grow: 1},
					ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(304), Background: colors.SecondaryBackground}}, sidebar(a, false)),
					ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(1), Background: colors.Separator}}),
					ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, conversation(a, false)),
					ui.Show(a.rightOpen, func() ui.Node {
						return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(390), Background: colors.SecondaryBackground,
							BorderWidth: 0.5, BorderColor: colors.Separator}}, surfacePanel(a, false))
					}),
				)
			},
		),
	)
}

func noticeBanner(a *app) ui.Node {
	return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(6, 12), Background: colors.Red.Opacity(0.1)},
		ui.Text(a.notice, ui.Style{Font: fonts.Footnote, Color: colors.Red, Grow: 1}),
		ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Dismiss"), OnPress: func() { a.notice.Set("") }}),
	)
}

type sidebarItem struct {
	Kind    string // "thread" or "new"
	Thread  proto.Thread
	Project proto.Project
	EnvID   string
	EnvName string
	EnvHost string
	Count   int
}

func (it sidebarItem) key() string {
	switch it.Kind {
	case "new":
		return "new:" + it.Project.ID
	case "header":
		return "project:" + it.Project.ID
	case "environment":
		return "environment:" + it.EnvID
	case "settled":
		return "settled:" + it.Project.ID
	case "remote":
		return "remote:" + it.EnvID + ":" + it.Thread.ID
	}
	return it.Thread.ID
}

func sidebarItems(a *app) []sidebarItem {
	projects := a.projects.Get()
	threads := slices.Clone(a.threads.Get())
	archived := a.showArchived.Get()
	showSettled := a.showSettled.Get()
	pinned := a.pinned.Get()
	byProject := map[string][]proto.Thread{}
	for _, t := range threads {
		if t.SideChat || (t.Archived && !archived) {
			continue
		}
		byProject[t.ProjectID] = append(byProject[t.ProjectID], t)
	}
	var out []sidebarItem
	for _, p := range projects {
		out = append(out, sidebarItem{Kind: "header", Project: p})
		var active, settled []proto.Thread
		for _, thread := range byProject[p.ID] {
			if thread.Settled {
				settled = append(settled, thread)
			} else {
				active = append(active, thread)
			}
		}
		sortSidebarThreads(active, pinned)
		sortSidebarThreads(settled, pinned)
		for _, t := range active {
			out = append(out, sidebarItem{Kind: "thread", Thread: t, Project: p})
		}
		if showSettled && len(settled) > 0 {
			out = append(out, sidebarItem{Kind: "settled", Project: p, Count: len(settled)})
			for _, t := range settled {
				out = append(out, sidebarItem{Kind: "thread", Thread: t, Project: p})
			}
		}
		out = append(out, sidebarItem{Kind: "new", Project: p})
	}
	for _, snapshot := range a.remoteEnvironmentThreads() {
		out = append(out, sidebarItem{Kind: "environment", EnvID: snapshot.ID, EnvName: snapshot.Name, EnvHost: snapshot.Host})
		threads := slices.Clone(snapshot.Threads)
		sortSidebarThreads(threads, pinned)
		for _, thread := range threads {
			if thread.SideChat || (thread.Archived && !archived) {
				continue
			}
			out = append(out, sidebarItem{Kind: "remote", Thread: thread, EnvID: snapshot.ID, EnvName: snapshot.Name, EnvHost: snapshot.Host})
		}
	}
	return out
}

func sortSidebarThreads(threads []proto.Thread, pinned map[string]bool) {
	slices.SortFunc(threads, func(x, y proto.Thread) int {
		if pinned[x.ID] != pinned[y.ID] {
			if pinned[x.ID] {
				return -1
			}
			return 1
		}
		if x.Status == proto.ThreadRunning && y.Status != proto.ThreadRunning {
			return -1
		}
		if y.Status == proto.ThreadRunning && x.Status != proto.ThreadRunning {
			return 1
		}
		return y.UpdatedAt.Compare(x.UpdatedAt)
	})
}

func sidebar(a *app, narrow bool) ui.Node {
	items := signal.NewMemoFunc(func() []sidebarItem { return sidebarItems(a) }, nil)
	header := ui.Column(ui.Style{Gap: 2, Padding: ui.Edges{Top: 14, Right: 12, Bottom: 10, Left: 16}},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
			ui.Column(ui.Style{Grow: 1, Gap: 1},
				ui.Text(ui.Val("T3Go"), ui.Style{Font: fonts.Title3.Weighted(fonts.Bold)}),
				ui.Text(signal.Func(func() string {
					name := a.envName.Get()
					host := a.envHost.Get()
					if host != "" && host != name {
						return name + " · " + host
					}
					return name
				}), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 1}),
			),
			ui.Button(ui.ButtonProps{Icon: ui.IconCompose, Label: ui.Val("New thread"), Tooltip: ui.Val("New thread"),
				OnPress: func() { a.openNewThread("") }}),
			ui.Button(ui.ButtonProps{Icon: ui.IconSearch, Label: ui.Val("Search"), Tooltip: ui.Val("Search messages"), OnPress: a.openSearch}),
			ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("More"), Menu: sidebarActions(a)}),
		),
		ui.Show(signal.Func(func() bool { return a.connecting.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6}, ui.Activity(), ui.Text(ui.Val("Connecting…"), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
		}),
		ui.Show(signal.Func(func() bool { return a.offline.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
				ui.Symbol(ui.IconWarning, ui.Style{Color: colors.Orange}),
				ui.Text(ui.Val("Reconnecting…"), ui.Style{Font: fonts.Caption, Color: colors.Orange}))
		}),
		ui.Show(signal.Func(func() bool { return a.connErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.connErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 3})
		}),
	)

	list := ui.List(ui.ListProps{
		Appearance: ui.ListBare,
	}, items, func(it sidebarItem) string { return it.key() }, func(item signal.Accessor[sidebarItem], _ signal.Accessor[int]) ui.Node {
		return ui.Dynamic(func() ui.Node {
			it := item.Get()
			switch it.Kind {
			case "header":
				return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 4, Padding: ui.Edges{Top: 12, Right: 8, Bottom: 4, Left: 16}},
					ui.Text(ui.Val(it.Project.Name), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold),
						Color: colors.SecondaryLabel, Grow: 1, Lines: 1}),
					ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("Project actions"),
						Menu: projectActions(a, it.Project)}),
				)
			case "environment":
				return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 5, Padding: ui.Edges{Top: 12, Right: 8, Bottom: 4, Left: 16}},
					ui.Symbol(ui.IconGlobe, ui.Style{Color: colors.SecondaryLabel}),
					ui.Text(ui.Val(it.EnvName), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold),
						Color: colors.SecondaryLabel, Grow: 1, Lines: 1}),
					ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Refresh environments"),
						OnPress: a.refreshEnvironmentSnapshots}),
				)
			case "settled":
				return ui.View(ui.ViewProps{
					OnPress: func() { a.showSettled.Set(false) },
					Style:   ui.Style{Padding: ui.Edges{Top: 10, Right: 10, Bottom: 2, Left: 16}},
				}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 5},
					ui.Symbol(ui.IconCheck, ui.Style{Color: colors.SecondaryLabel}),
					ui.Text(ui.Val(fmt.Sprintf("Settled (%d)", it.Count)), ui.Style{Font: fonts.Caption2.Weighted(fonts.Semibold),
						Color: colors.SecondaryLabel, Grow: 1}),
					ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: ui.Val("Hide settled threads"),
						OnPress: func() { a.showSettled.Set(false) }}),
				))
			case "new":
				return ui.View(ui.ViewProps{
					OnPress: func() { a.openNewThread(it.Project.ID) },
					Style:   ui.Style{Padding: ui.Symmetric(8, 16)},
				}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
					ui.Symbol(ui.IconPlus, ui.Style{Color: colors.Accent}),
					ui.Text(ui.Val("New thread"), ui.Style{Font: fonts.Footnote, Color: colors.Accent}),
				))
			}
			return threadRow(a, item)
		})
	})

	var body []ui.Node
	body = append(body, header)
	if len(a.projects.Peek()) == 0 {
		body = append(body, ui.Column(ui.Style{Padding: ui.All(24), Gap: 10, Align: ui.AlignCenter},
			ui.Text(ui.Val("No projects yet"), ui.Style{Font: fonts.Headline}),
			ui.Text(ui.Val("Add a folder the agent can work in."), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, TextAlign: ui.TextCenter}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add project"), Variant: ui.ButtonFilled, OnPress: func() {
				a.notice.Set("")
				a.showNewProject.Set(true)
			}}),
		))
	} else {
		body = append(body, list)
	}
	if !narrow {
		return ui.Column(ui.Style{Grow: 1}, body...)
	}
	return ui.Column(ui.Style{Grow: 1}, body...)
}

func sidebarActions(a *app) []ui.Action {
	archived := func() {
		a.showArchived.Update(func(v bool) bool { return !v })
	}
	return []ui.Action{
		{Title: ui.Val("Command palette…"), Icon: ui.IconMenu, OnPress: a.openPalette},
		{Title: ui.Val("Search messages…"), Icon: ui.IconSearch, OnPress: a.openSearch},
		{Title: ui.Val("Settings…"), Icon: ui.IconSettings, OnPress: func() { a.openSettings(settingsGeneral) }},
		{Title: ui.Val("Environments…"), Icon: ui.IconGlobe, OnPress: func() { a.openSettings(settingsEnvironments) }},
		{Title: ui.Val("Surfaces…"), Icon: ui.IconList, OnPress: func() { a.openSurface(a.rightTab.Peek()) }},
		{Title: ui.Val("Pull requests…"), Icon: ui.IconGlobe, OnPress: a.openPullRequestInbox},
		{Title: ui.Val("Connections…"), Icon: ui.IconGlobe, OnPress: func() { a.showConnections.Set(true) }},
		{Title: ui.Val("Add project…"), Icon: ui.IconPlus, OnPress: func() { a.notice.Set(""); a.showNewProject.Set(true) }},
		{Title: ui.Val("Show archived"), Icon: ui.IconList, Checked: a.showArchived, OnPress: archived},
		{Title: ui.Val("Show settled"), Icon: ui.IconCheck, Checked: a.showSettled,
			OnPress: func() { a.showSettled.Update(func(v bool) bool { return !v }) }},
		{Title: ui.Val("Reload"), Icon: ui.IconRefresh, OnPress: a.reload},
	}
}

func threadRow(a *app, item signal.Accessor[sidebarItem]) ui.Node {
	return ui.Dynamic(func() ui.Node {
		row := item.Get()
		thread := row.Thread
		selectThread := func() {
			if row.Kind == "remote" {
				a.openEnvironmentThread(row.EnvID, thread.ID)
				return
			}
			a.selectThread(thread.ID)
		}
		actions := a.threadActions(thread)
		if row.Kind == "remote" {
			actions = a.remoteThreadActions(row.EnvID, row.EnvName, thread)
		}
		rowView := ui.View(ui.ViewProps{
			OnPress: selectThread,
			Style: ui.StyleFunc(func() ui.Style {
				t := item.Get().Thread
				s := ui.Style{Padding: ui.Symmetric(8, 16), Gap: 3}
				if a.selected.Get() == t.ID {
					s.Background = colors.Accent.Opacity(0.14)
				}
				return s
			}),
		},
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 7},
				statusDot(a, item),
				ui.Text(signal.Func(func() string {
					t := item.Get().Thread
					if title := strings.TrimSpace(t.Title); title != "" {
						return title
					}
					return "Untitled thread"
				}), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Medium), Lines: 1, Grow: 1}),
				ui.Show(signal.Func(func() bool { return a.isThreadPinned(thread.ID) }), func() ui.Node {
					return ui.Symbol(ui.IconPin, ui.Style{Color: colors.Accent})
				}),
			),
			ui.Text(signal.Func(func() string {
				t := item.Get().Thread
				if row.Kind == "remote" {
					status := "Idle"
					if t.Status == proto.ThreadRunning {
						status = "Running"
					} else if t.Status == proto.ThreadFailed {
						status = "Failed"
					}
					return row.EnvName + " · " + status
				}
				return a.statusText(t)
			}), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1,
				Padding: ui.Edges{Left: 15}}),
		)
		return ui.Menu(ui.MenuProps{LongPress: true, Actions: actions}, rowView)
	})
}

func projectActions(a *app, p proto.Project) []ui.Action {
	return a.projectActions(p)
}

func statusDot(a *app, item signal.Accessor[sidebarItem]) ui.Node {
	return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		t := item.Get().Thread
		c := colors.Green
		switch t.Status {
		case proto.ThreadRunning:
			c = colors.Accent
		case proto.ThreadFailed:
			c = colors.Red
		}
		if t.Status == proto.ThreadIdle {
			c = colors.Separator
		}
		if a.isThreadUnread(t) {
			c = colors.Blue
		}
		return ui.Style{Width: ui.Pt(7), Height: ui.Pt(7), CornerRadius: 4, Background: c}
	})})
}

func connectView(a *app, initial bool) ui.Node {
	card := ui.Column(ui.Style{Width: ui.Pct(100), MaxWidth: ui.Pt(470), Padding: ui.All(22), Gap: 14,
		Background: colors.SecondaryBackground, CornerRadius: 14, BorderWidth: 1, BorderColor: colors.Separator},
		ui.Column(ui.Style{Gap: 4},
			ui.Text(ui.Val("Connect to T3Go"), ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}),
			ui.Text(ui.Val("Use a pairing link or the environment's address and token."),
				ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
		),
		ui.TextField(ui.TextFieldProps{Value: a.formURL, Placeholder: ui.Val("Server address or t3go://pair link"),
			Keyboard: ui.KeyboardURL, Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo,
			ReturnKey: ui.ReturnNext, ContentType: ui.ContentURL}),
		ui.TextField(ui.TextFieldProps{Value: a.formToken, Placeholder: ui.Val("Token"), Secure: true,
			Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo, ReturnKey: ui.ReturnGo,
			OnSubmit: func() { connectFromForm(a) }}),
		ui.Show(signal.Func(func() bool { return a.connErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.connErr, ui.Style{Font: fonts.Footnote, Color: colors.Red})
		}),
		ui.Show(signal.Func(func() bool { return a.connecting.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8}, ui.Activity(),
				ui.Text(ui.Val("Connecting…"), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}))
		}),
		ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
			if a.connecting.Get() {
				return "Connecting…"
			}
			return "Connect"
		}), Variant: ui.ButtonFilled,
			Disabled: signal.Func(func() bool { return a.connecting.Get() || strings.TrimSpace(a.formURL.Get()) == "" }),
			OnPress:  func() { connectFromForm(a) }}),
		savedEnvironmentsView(a),
	)
	if !initial {
		card = ui.Column(ui.Style{Grow: 1},
			ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.All(12)},
				ui.Text(ui.Val("Add environment"), ui.Style{Font: fonts.Headline}),
				ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close"), OnPress: func() { a.showConnect.Set(false) }}),
			),
			ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}}, ui.Column(ui.Style{Grow: 1, Align: ui.AlignCenter, Justify: ui.JustifyCenter, Padding: ui.All(20)}, card)),
		)
		return ui.SafeArea(ui.Style{Grow: 1}, card)
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.Column(ui.Style{Grow: 1, Align: ui.AlignCenter, Justify: ui.JustifyCenter, Padding: ui.All(24), Gap: 16},
			ui.Text(ui.Val("T3Go"), ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
			card,
		))
}

func connectFromForm(a *app) {
	if a.connecting.Peek() {
		return
	}
	a.connect(a.formURL.Peek(), a.formToken.Peek())
}

func (a *app) openAddEnvironment() {
	a.showConnections.Set(false)
	a.showSettings.Set(false)
	a.formURL.Set("")
	a.formToken.Set("")
	a.connErr.Set("")
	a.showConnect.Set(true)
}

func savedEnvironmentsView(a *app) ui.Node {
	return ui.Show(signal.Func(func() bool { return len(a.profilesV.Get()) > 0 }), func() ui.Node {
		return ui.Column(ui.Style{Gap: 6},
			ui.Text(ui.Val("Saved environments"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
			ui.For(a.profilesV, func(p client.EnvironmentProfile) string { return p.ID },
				func(item signal.Accessor[client.EnvironmentProfile], _ signal.Accessor[int]) ui.Node {
					p := item.Get()
					return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
						ui.Button(ui.ButtonProps{Title: signal.Func(func() string { return environmentBaseName(item.Get()) }), Icon: ui.IconGlobe, Style: ui.Style{Grow: 1},
							OnPress: func() {
								a.formURL.Set(firstRoute(p))
								a.formToken.Set(p.Token)
								a.connectProfile(p, true)
							}}),
						ui.Button(ui.ButtonProps{Icon: ui.IconTrash, Label: ui.Val("Forget environment"),
							OnPress: func() { a.forgetProfile(p.ID) }}),
					)
				}),
		)
	})
}

func connectionsView(a *app) ui.Node {
	rows := ui.For(a.profilesV, func(p client.EnvironmentProfile) string { return p.ID },
		func(item signal.Accessor[client.EnvironmentProfile], _ signal.Accessor[int]) ui.Node {
			return ui.Dynamic(func() ui.Node {
				p := item.Get()
				current := a.activeV.Get() == p.ID
				detail := environmentHost(p)
				if detail == "" {
					detail = "Saved environment"
				}
				detail += " · " + profilePreferenceLabel(p.Preference)
				if a.defaultID == p.ID {
					detail += " · default"
				}
				return ui.View(ui.ViewProps{
					OnPress: func() { a.switchProfile(p.ID) },
					Style:   ui.Style{Padding: ui.Symmetric(10, 4)},
				}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
					ui.Column(ui.Style{Grow: 1, Gap: 1},
						ui.Text(ui.Val(environmentBaseName(p)), ui.Style{Font: fonts.Body.Weighted(fonts.Medium), Lines: 1}),
						ui.Text(ui.Val(detail), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 1}),
					),
					ui.Show(signal.Func(func() bool { return current }), func() ui.Node {
						return ui.Symbol(ui.IconCheck, ui.Style{Color: colors.Accent})
					}),
					ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("Environment actions"),
						Menu: environmentActions(a, p)}),
				))
			})
		})
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.All(12)},
			ui.Text(ui.Val("Connections"), ui.Style{Font: fonts.Headline}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close"), OnPress: func() { a.showConnections.Set(false) }}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.All(16), Gap: 12},
				ui.Text(signal.Func(func() string {
					host := a.envHost.Get()
					if host == "" {
						host = "Not connected"
					}
					return "Current: " + host
				}), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
				rows,
				ui.Button(ui.ButtonProps{Title: ui.Val("Add environment"), Icon: ui.IconPlus, Variant: ui.ButtonFilled,
					OnPress: a.openAddEnvironment}),
			)),
	)
}

func environmentActions(a *app, p client.EnvironmentProfile) []ui.Action {
	token := signal.New("")
	alias := signal.New(environmentBaseName(p))
	return []ui.Action{
		{Title: ui.Val("Reconnect"), Icon: ui.IconRefresh, OnPress: func() { a.reconnectProfile(p.ID) }},
		{Title: ui.Val("Route: Auto"), Icon: ui.IconGlobe,
			Checked: signal.Func(func() bool { return a.findProfile(p.ID).Preference == "" || a.findProfile(p.ID).Preference == "auto" }),
			OnPress: func() { a.setProfilePreference(p.ID, "auto") }},
		{Title: ui.Val("Direct only"), Icon: ui.IconGlobe,
			Checked: signal.Func(func() bool { return a.findProfile(p.ID).Preference == "direct-only" }),
			OnPress: func() { a.setProfilePreference(p.ID, "direct-only") }},
		{Title: ui.Val("Relay only"), Icon: ui.IconGlobe,
			Checked: signal.Func(func() bool { return a.findProfile(p.ID).Preference == "relay-only" }),
			OnPress: func() { a.setProfilePreference(p.ID, "relay-only") }},
		{Title: ui.Val("Update token…"), Icon: ui.IconLock, OnPress: func() {
			token.Set("")
			ui.Alert(ui.AlertProps{Title: "Update token", Message: environmentBaseName(p),
				Fields: []ui.AlertField{{Text: token, Placeholder: "Environment token", Secure: true}},
				Actions: []ui.Action{{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
					{Title: ui.Val("Save"), Role: ui.ActionDefault,
						OnPress: func() { a.updateProfileToken(p.ID, token.Peek()) }}}})
		}},
		{Title: ui.Val("Rename…"), Icon: ui.IconCompose, OnPress: func() {
			alias.Set(environmentBaseName(p))
			ui.Alert(ui.AlertProps{Title: "Rename environment", Fields: []ui.AlertField{{Text: alias, Placeholder: "Display name"}},
				Actions: []ui.Action{{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
					{Title: ui.Val("Save"), Role: ui.ActionDefault,
						OnPress: func() { a.renameProfile(p.ID, alias.Peek()) }}}})
		}},
		{Title: ui.Val("Set as default"), Icon: ui.IconCheck, Disabled: signal.Func(func() bool { return a.defaultID == p.ID }),
			OnPress: func() { a.setDefaultProfile(p.ID) }},
		{Title: ui.Val("Forget…"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "Forget environment?", Message: environmentBaseName(p),
				Actions: []ui.Action{{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
					{Title: ui.Val("Forget"), Role: ui.ActionDestructive,
						OnPress: func() { a.forgetProfile(p.ID) }}}})
		}},
	}
}

func newThreadView(a *app) ui.Node {
	projects := signal.NewMemoFunc(func() []proto.Project { return a.projects.Get() }, nil)
	options := signal.NewMemoFunc(func() []threadModelOption {
		return modelOptionsOf(a.providers.Get(), a.models.Get(), a.customModels.Get(), a.settings.Get())
	}, nil)
	projectLabels := signal.Func(func() []string {
		var out []string
		for _, p := range projects.Get() {
			out = append(out, p.Name)
		}
		return out
	})
	optionLabels := signal.Func(func() []string {
		var out []string
		for _, o := range options.Get() {
			out = append(out, o.Label)
		}
		return out
	})
	create := func() {
		if len(projects.Get()) == 0 {
			a.showNewThread.Set(false)
			a.showNewProject.Set(true)
			return
		}
		pi := a.newThreadProject.Peek()
		project := projects.Get()[min(max(pi, 0), len(projects.Get())-1)]
		in := client.CreateThreadInput{ProjectID: project.ID, Title: strings.TrimSpace(a.newThreadTitle.Peek())}
		if opts := options.Get(); len(opts) > 0 {
			o := opts[min(max(a.newThreadModel.Peek(), 0), len(opts)-1)]
			in.Provider, in.Model = o.Provider, o.Model
		}
		in.Permission = permissionAt(a.newThreadPerm.Peek())
		in.Worktree = a.newThreadWorktree.Peek()
		in.Branch = strings.TrimSpace(a.newThreadBranch.Peek())
		in.BaseRef = strings.TrimSpace(a.newThreadBase.Peek())
		a.createThread(in, a.newThreadPrompt.Peek())
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.All(12)},
			ui.Text(ui.Val("New thread"), ui.Style{Font: fonts.Headline}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close"), OnPress: func() { a.showNewThread.Set(false) }}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.All(16), Gap: 14},
				ui.Show(signal.Func(func() bool { return a.notice.Get() != "" }), func() ui.Node {
					return ui.Text(a.notice, ui.Style{Font: fonts.Footnote, Color: colors.Red})
				}),
				ui.Text(ui.Val("Project"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.Picker(ui.PickerProps{Items: projectLabels, Selected: a.newThreadProject, Label: ui.Val("Project")}),
				ui.Show(signal.Func(func() bool { return len(projects.Get()) == 0 }), func() ui.Node {
					return ui.Text(ui.Val("Add a project first."), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel})
				}),
				ui.Text(ui.Val("Title"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.newThreadTitle, Placeholder: ui.Val("Optional title")}),
				ui.Text(ui.Val("First message (optional)"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.newThreadPrompt, Placeholder: ui.Val("What should the agent do?"),
					Multiline: true, Style: ui.Style{Lines: 5, MinHeight: ui.Pt(40)}}),
				ui.Text(ui.Val("Agent"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.Picker(ui.PickerProps{Items: optionLabels, Selected: a.newThreadModel, Label: ui.Val("Agent")}),
				ui.Text(ui.Val("Permission"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.Picker(ui.PickerProps{Items: ui.Val([]string{"Supervised", "Accept edits", "Auto", "Full access"}),
					Selected: a.newThreadPerm, Label: ui.Val("Permission")}),
				ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 10},
					ui.Column(ui.Style{Grow: 1, Gap: 1},
						ui.Text(ui.Val("New worktree"), ui.Style{Font: fonts.Footnote}),
						ui.Text(ui.Val("Run in an isolated checkout for this thread."),
							ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 2}),
					),
					ui.Toggle(ui.ToggleProps{Value: a.newThreadWorktree}),
				),
				ui.Text(ui.Val("Branch (optional)"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.newThreadBranch, Placeholder: ui.Val("feature/my-branch"),
					Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo}),
				ui.Text(ui.Val("Base ref (optional)"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.newThreadBase, Placeholder: ui.Val("origin/main"),
					Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo}),
				ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
					if len(projects.Get()) == 0 {
						return "Add project"
					}
					return "Create"
				}), Variant: ui.ButtonFilled, Disabled: signal.Func(func() bool { return a.connecting.Get() }), OnPress: create}),
			)),
	)
}

func newProjectView(a *app) ui.Node {
	create := func() {
		name := strings.TrimSpace(a.newProjectName.Peek())
		path := strings.TrimSpace(a.newProjectPath.Peek())
		if path == "" {
			a.notice.Set("Enter the folder's path on the environment.")
			return
		}
		if name == "" {
			name = path[strings.LastIndexAny(path, `/\`)+1:]
			if name == "" {
				name = path
			}
		}
		a.createProject(name, path)
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.All(12)},
			ui.Text(ui.Val("Add project"), ui.Style{Font: fonts.Headline}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close"), OnPress: func() { a.showNewProject.Set(false) }}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.All(16), Gap: 14},
				ui.Show(signal.Func(func() bool { return a.notice.Get() != "" }), func() ui.Node {
					return ui.Text(a.notice, ui.Style{Font: fonts.Footnote, Color: colors.Red})
				}),
				ui.Text(ui.Val("Name"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.newProjectName, Placeholder: ui.Val("Project name")}),
				ui.Text(ui.Val("Folder on the environment"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.TextField(ui.TextFieldProps{Value: a.newProjectPath, Placeholder: ui.Val("/path/to/folder"),
					Keyboard: ui.KeyboardURL, Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo, ReturnKey: ui.ReturnGo,
					OnSubmit: func() { create() }}),
				ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
					if a.showDirPicker.Get() {
						return "Hide folder browser"
					}
					return "Browse folders on the environment"
				}), Icon: ui.IconFolder, OnPress: func() {
					if a.showDirPicker.Peek() {
						a.showDirPicker.Set(false)
					} else {
						a.openDirPicker()
					}
				}}),
				ui.Show(a.showDirPicker, func() ui.Node { return dirsView(a) }),
				ui.Text(ui.Val("The path is on the machine running the environment, not this device."),
					ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Add project"), Variant: ui.ButtonFilled, OnPress: create}),
			)),
	)
}

func modelOptionsOf(providers []proto.Provider, models []proto.Model, custom map[string][]proto.Model, settings proto.Settings) []threadModelOption {
	var out []threadModelOption
	seen := map[[2]string]bool{}
	add := func(provider, model, label string) {
		key := [2]string{provider, model}
		if provider == "" || model == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, threadModelOption{Provider: provider, Model: model, Label: label})
	}
	for _, p := range providers {
		if !p.Installed {
			continue
		}
		listed := false
		for _, m := range models {
			if m.Provider == p.ID {
				listed = true
				add(p.ID, m.ID, p.Name+" · "+modelLabel(m))
			}
		}
		if !listed {
			for _, m := range p.Models {
				add(p.ID, m.ID, p.Name+" · "+modelLabel(m))
			}
		}
		for _, m := range custom[p.ID] {
			add(p.ID, m.ID, p.Name+" · "+modelLabel(m))
		}
	}
	if len(out) == 0 && settings.DefaultProvider != "" {
		add(settings.DefaultProvider, settings.DefaultModel,
			settings.DefaultProvider+" · "+settings.DefaultModel)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
