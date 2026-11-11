package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oneo-me/nsgo"
	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/proto"
)

func main() {
	nsgo.Run(Root, nsgo.WindowSize(1100, 760))
}

// Root builds the app: a connection screen until an environment answers,
// then the projects and threads in it.
func Root(cx *core.Scope) core.Node {
	a := newApp(cx)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if !a.connected.Get() {
			return a.connectView(cx)
		}
		return a.mainView(cx)
	})
}

func (a *app) connectView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		Padding: core.All(24), Background: colors.Background()}, Children: []core.Node{
		core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(400), Gap: 10}, Children: []core.Node{
			core.Text{Content: "T3Go", Style: core.Style{FontSize: 24, FontWeight: ui.Bold}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: "Connect to a T3Go environment to open its projects and threads.", Style: core.Style{FontSize: 13, LineHeight: 19},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.View{Style: core.Style{Height: core.Pt(6)}},
			ui.Input{Input: base.Input{Value: a.serverURL.Get, OnChange: a.serverURL.Set, Placeholder: "Server, e.g. http://127.0.0.1:4870",
				Label: "Server address", SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.connect(a.serverURL.Get(), a.token.Get()) }}},
			ui.Input{Input: base.Input{Value: a.token.Get, OnChange: a.token.Set, Placeholder: "Token, or paste a t3go:// pair link",
				Label: "Token", Masked: true, SubmitBehavior: core.SubmitBehaviorSubmit,
				OnSubmit: func(string) { a.connect(a.serverURL.Get(), a.token.Get()) }}},
			core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
			}},
			ui.Button{Text: func() string {
				if a.connecting.Get() {
					return "Connecting…"
				}
				return "Connect"
			}(), Variant: ui.ButtonPrimary, OnPress: func() { a.connect(a.serverURL.Get(), a.token.Get()) },
				Disabled: a.connecting.Get, Flex: 1},
			ui.Text("The token is in the environment's data directory, or run `t3go pair`.", 11.5, ui.Regular, colors.Faint),
		}},
	}}
}

func (a *app) mainView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	wide := cx.Env().Window().Width >= 760
	var layout core.Node
	if wide {
		layout = ui.SidebarShell{
			Sidebar: []core.Node{a.sidebar(cx)},
			Header:  []core.Node{a.contentHeader(cx)},
			Content: []core.Node{a.content(cx)},
			Open:    a.sidebarOpen.Get, OnOpenChange: a.setSidebarOpen,
			ToggleLabel: "Toggle sidebar",
			Width:       a.sidebarWidth.Get, OnWidthChange: a.setSidebarWidth,
			DefaultWidth: 260, MinWidth: 220, MaxWidth: 380,
			ResizeLabel: "Resize sidebar",
		}
	} else {
		layout = a.narrowView(cx)
	}
	body := layout
	if wide {
		body = core.View{Style: core.Style{Direction: core.Row, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0),
			MinHeight: core.Pt(0), Overflow: core.Hidden}, Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
				Overflow: core.Hidden}, Children: []core.Node{layout}},
			core.Show{When: a.rightOpen.Get, Then: func(cx *core.Scope) core.Node { return a.rightRail(cx) }},
		}}
	}
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
		Overflow: core.Hidden, Direction: core.Column, Background: colors.Background()}, Children: []core.Node{
		core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node { return a.errorBanner(cx) }},
		core.Show{When: a.offline.Get, Then: func(*core.Scope) core.Node { return a.offlineBanner(cx) }},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
			Overflow: core.Hidden}, Children: []core.Node{body, a.dialogs(cx)}},
	}}
}

// envName is what the environment calls itself, the brand until it is
// renamed in settings.
func (a *app) envName() string {
	if name := a.settings.Get().Title; name != "" {
		return name
	}
	return "T3Go"
}

// offlineBanner says the event stream dropped and is retrying.
func (a *app) offlineBanner(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "offline", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
		Padding: core.Symmetric(6, 12), Background: colors.Inset()}, Children: []core.Node{
		ui.Spinner{Size: 12, Label: "Reconnecting"},
		core.Text{Content: "Connection lost. Reconnecting…", Style: core.Style{FontSize: 12},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
	}}
}

// errorBanner shows an action's failure above the content, with a way to
// dismiss it.
func (a *app) errorBanner(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
		Padding: core.Symmetric(6, 12), Background: colors.Inset()}, Children: []core.Node{
		core.Text{Content: a.connErr.Get(), MaxLines: 3, Style: core.Style{FontSize: 12, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }},
		ui.Button{Text: "Dismiss", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: func() { a.connErr.Set("") }},
	}}
}

func (a *app) sidebar(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		core.View{Style: core.Style{Padding: core.Edges{Top: 4, Right: 10, Bottom: 2, Left: 10}}, Children: []core.Node{
			ui.Input{Input: base.Input{Value: a.sidebarQuery.Get, OnChange: a.sidebarQuery.Set,
				Placeholder: "Search threads", Label: "Search threads"}},
		}},
		core.View{Style: ui.FillRest, Children: []core.Node{
			ui.Sidebar{SectionsFunc: func() []ui.SidebarSection { return a.sidebarSections(cx) },
				Selected: a.selected.Get, OnSelect: a.selectThread},
		}},
		a.sidebarFooter(cx),
	}}
}

// sidebarSections builds the project and thread groups under the current
// search, inside the sidebar's own scope so typing keeps the field still.
func (a *app) sidebarSections(cx *core.Scope) []ui.SidebarSection {
	colors := ui.ColorsOf(cx)
	projects := a.projects.Get()
	allThreads := a.threads.Get()
	showArchived := a.showArchived.Get()
	pinned := a.pinned.Get()
	query := strings.ToLower(strings.TrimSpace(a.sidebarQuery.Get()))
	var sections []ui.SidebarSection
	for _, p := range projects {
		p := p
		var threads []proto.Thread
		for _, t := range allThreads {
			if t.ProjectID != p.ID || (t.Archived && !showArchived) {
				continue
			}
			if query != "" && !threadMatches(t, query) {
				continue
			}
			threads = append(threads, t)
		}
		sort.SliceStable(threads, func(i, j int) bool {
			iPinned, jPinned := pinned[threads[i].ID], pinned[threads[j].ID]
			if iPinned != jPinned {
				return iPinned
			}
			return threads[i].UpdatedAt.After(threads[j].UpdatedAt)
		})
		var items []ui.SidebarItem
		for _, t := range threads {
			t := t
			archiveText := "Archive thread"
			if t.Archived {
				archiveText = "Unarchive thread"
			}
			pinText := "Pin thread"
			if a.threadPinned(t.ID) {
				pinText = "Unpin thread"
			}
			items = append(items, ui.SidebarItem{ID: t.ID, Text: t.Title, TestID: "thread-" + t.ID,
				Content: a.threadRow(cx, t, colors),
				Menu: []core.Node{
					ui.MenuItem{Text: pinText, TestID: "pin-thread-" + t.ID, OnSelect: func() { a.togglePin(t) }},
					ui.MenuItem{Text: "Rename thread…", TestID: "rename-thread-" + t.ID, OnSelect: func() { a.askRenameThread(t) }},
					ui.MenuItem{Text: archiveText, TestID: "archive-thread-" + t.ID, OnSelect: func() { a.setArchived(t, !t.Archived) }},
					ui.MenuItem{Text: "Delete thread", Variant: ui.MenuItemDanger,
						TestID: "delete-thread-" + t.ID, OnSelect: func() { a.askDeleteThread(t) }},
				}})
		}
		if query != "" && len(items) == 0 {
			continue
		}
		section := ui.SidebarSection{Title: p.Name, Items: items, Collapsible: true,
			Open:         func() bool { return !a.projectCollapsed(p.ID) },
			OnOpenChange: func(open bool) { a.setProjectCollapsed(p.ID, !open) },
			Trailing:     ui.Text(strconv.Itoa(len(items)), 10.5, ui.Regular, colors.Faint),
			TestID:       "project-" + p.ID,
		}
		section.Actions = ui.Menu{
			TestID:  "project-menu-" + p.ID,
			Trigger: ui.Button{Text: "⋯", Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Project actions", TestID: "project-actions-" + p.ID},
			Children: []core.Node{
				ui.MenuItem{Text: "New thread…", TestID: "new-thread-in-" + p.ID, OnSelect: func() { a.openNewThreadIn(p.ID) }},
				ui.MenuItem{Text: "Rename project…", TestID: "rename-project-" + p.ID, OnSelect: func() { a.askRenameProject(p) }},
				ui.MenuItem{Text: "Delete project", Variant: ui.MenuItemDanger,
					TestID: "delete-project-" + p.ID, OnSelect: func() { a.askDeleteProject(p) }},
			},
		}
		if len(items) == 0 {
			note := "No threads yet"
			for _, t := range allThreads {
				if t.ProjectID == p.ID {
					note = "No threads shown"
					break
				}
			}
			section.Note = ui.Text(note, 12, ui.Regular, colors.Faint)
		}
		sections = append(sections, section)
	}
	if len(sections) == 0 {
		note := "Add a project to start."
		switch {
		case query != "":
			note = "No threads match your search."
		case len(projects) > 0:
			note = "No threads yet."
		}
		sections = append(sections, ui.SidebarSection{Title: "Projects", Note: ui.Text(note, 12, ui.Regular, colors.Faint)})
	}
	return sections
}

// threadRow is one thread's line in the sidebar: its state, title, the
// last prompt and when it changed.
func (a *app) threadRow(cx *core.Scope, t proto.Thread, colors ui.Colors) core.Node {
	title := t.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled thread"
	}
	titleColor := colors.Text
	if t.Archived {
		titleColor = colors.Muted
	}
	head := []core.Node{}
	switch {
	case t.Status == proto.ThreadRunning:
		head = append(head, ui.Spinner{Size: 11, Label: "Running"})
	case t.Status == proto.ThreadFailed:
		head = append(head, ui.Icon(lucide.CircleAlert, 12, colors.Danger))
	case a.threadUnread(t):
		head = append(head, core.View{TestID: "unread-" + t.ID,
			Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: colors.Accent()}})
	case t.Archived:
		head = append(head, ui.Icon(lucide.Archive, 11, colors.Faint))
	}
	if a.threadPinned(t.ID) {
		head = append(head, core.View{TestID: "pinned-" + t.ID, Children: []core.Node{ui.Icon(lucide.Pin, 10, colors.Faint)}})
	}
	head = append(head,
		core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 12.5, FontWeight: ui.Semibold, FlexGrow: 1, FlexShrink: 1},
			StyleFunc: func() core.Style { return core.Style{Color: titleColor()} }},
		core.Text{Content: relativeTime(t.UpdatedAt), MaxLines: 1, Style: core.Style{FontSize: 10.5, FlexShrink: 0},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
	)
	rows := []core.Node{core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, MinWidth: core.Pt(0)}, Children: head}}
	draft := a.draftFor(t.ID)
	preview := oneLine(t.Summary, 80)
	previewColor := colors.Faint
	if draft != "" {
		preview, previewColor = "Draft: "+oneLine(draft, 72), colors.Accent
	} else if preview == "" {
		preview = t.Provider
	}
	line := []core.Node{}
	switch {
	case draft != "":
		line = append(line, ui.Icon(lucide.SquarePen, 10, colors.Accent))
	case t.WorktreePath != "":
		line = append(line, ui.Icon(lucide.GitBranch, 10, colors.Faint))
	}
	if strings.TrimSpace(preview) != "" {
		line = append(line, core.Text{Content: preview, MaxLines: 1, Style: core.Style{FontSize: 11, FlexGrow: 1, FlexShrink: 1},
			StyleFunc: func() core.Style { return core.Style{Color: previewColor()} }})
	}
	if len(line) > 0 {
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, MinWidth: core.Pt(0)}, Children: line})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Padding: core.Symmetric(3, 0)},
		Children: rows}
}

// threadMatches reports whether a thread's title, prompt or branch matches
// the sidebar's search.
func threadMatches(t proto.Thread, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{t.Title, t.Summary, t.Branch, t.Provider, t.Model}, " "))
	return strings.Contains(haystack, query)
}

// relativeTime writes the compact age the sidebar shows.
func relativeTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	age := time.Since(at)
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	case at.Year() == time.Now().Year():
		return at.Local().Format("Jan 2")
	}
	return at.Local().Format("Jan 2006")
}

func (a *app) archivedLabel() string {
	if a.showArchived.Get() {
		return "Hide archived"
	}
	return "Archived"
}

func (a *app) sidebarFooter(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, Width: core.Pct(100), Padding: core.Symmetric(6, 4), Gap: 4, AlignItems: core.AlignCenter}, Children: []core.Node{
		ui.IconButton{Icon: lucide.FolderPlus, Label: "New project", TestID: "new-project", OnPress: a.openNewProject},
		ui.IconButton{Icon: lucide.MessageSquarePlus, Label: "New thread", TestID: "new-thread", OnPress: a.openNewThread},
		ui.Spacer{},
		ui.IconButton{IconFunc: func() *core.SVG {
			if a.showArchived.Get() {
				return lucide.ArchiveRestore()
			}
			return lucide.Archive()
		}, LabelFunc: a.archivedLabel, TestID: "toggle-archived", OnPress: a.toggleShowArchived},
		ui.IconButton{Icon: lucide.Search, Label: "Search", TestID: "open-palette", OnPress: a.openPalette},
		ui.IconButton{Icon: lucide.Settings, Label: "Settings", TestID: "settings", OnPress: a.openSettings},
	}}
}

func (a *app) contentHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	d := a.detail.Get()
	if d.Thread.ID == "" {
		return core.View{Style: core.Style{Height: core.Pt(32), Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
			ui.Text(a.envName(), 13, ui.Semibold, colors.Muted), ui.Spacer{}, a.rightToggle(),
		}}
	}
	meta := d.Thread.Provider
	if d.Thread.Model != "" {
		meta += " · " + d.Thread.Model
	}
	meta += " · " + string(d.Thread.Permission)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinWidth: core.Pt(0)}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, MinWidth: core.Pt(0), FlexGrow: 1, Gap: 1}, Children: []core.Node{
			core.Text{Content: d.Thread.Title, MaxLines: 1, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: meta, MaxLines: 1, Style: core.Style{FontSize: 11},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}},
		a.branchMenu(cx, d),
		ui.IconButton{Icon: lucide.Pencil, Label: "Rename thread", TestID: "rename-thread",
			OnPress: func() { a.askRenameThread(d.Thread) }},
		ui.IconButton{Icon: lucide.Trash, Label: "Delete thread", TestID: "delete-thread",
			OnPress: func() { a.askDeleteThread(d.Thread) }},
		ui.IconButton{Icon: lucide.Clock3, Label: "Run history", TestID: "history", OnPress: a.openRuns},
		ui.IconButton{Icon: lucide.GitCompare, Label: "Changes in this thread", TestID: "open-diff", OnPress: a.openDiff},
		a.rightToggle(),
	}}
}

// rightToggle shows or hides the surfaces panel.
func (a *app) rightToggle() core.Node {
	if a.cx.Env().Window().Width < 760 {
		return core.View{}
	}
	return ui.IconButton{Icon: lucide.PanelRight, Label: "Toggle right panel", TestID: "toggle-right-panel",
		Selected: a.rightOpen.Get, OnPress: func() { a.setRightOpen(!a.rightOpen.GetUntracked()) }}
}

// branchMenu is the header's branch control.
func (a *app) branchMenu(cx *core.Scope, detail proto.ThreadDetail) core.Node {
	branch := detail.Thread.Branch
	if detail.Git != nil && detail.Git.Branch != "" {
		branch = detail.Git.Branch
	}
	if branch == "" {
		branch = "detached"
	}
	return ui.Button{Text: branch, Icon: lucide.GitBranch, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
		Label: "Branch", TestID: "branch-menu", OnPress: func() { a.showBranches.Set(true) }}
}

func (a *app) content(cx *core.Scope) core.Node {
	if a.selected.Get() == "" {
		return a.welcome(cx)
	}
	return a.threadPane(cx)
}

func (a *app) welcome(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Padding: core.All(24), Gap: 12}, Children: []core.Node{
		ui.Text("T3Go", 20, ui.Bold, colors.Text),
		ui.Text("Pick a thread, or start one in a project.", 13, ui.Regular, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Gap: 8, JustifyContent: core.JustifyCenter}, Children: []core.Node{
			ui.Button{Text: "New project", Icon: lucide.FolderPlus, Variant: ui.ButtonSecondary, TestID: "welcome-new-project", OnPress: a.openNewProject},
			ui.Button{Text: "New thread", Icon: lucide.MessageSquarePlus, Variant: ui.ButtonPrimary, TestID: "welcome-new-thread", OnPress: a.openNewThread},
		}},
	}}
}

// narrowView is the phone layout: a list of threads, or the open thread
// with a way back to the list.
func (a *app) narrowView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() != "" {
		return core.View{Style: core.Style{FlexGrow: 1}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(8, 8), Gap: 8}, Children: []core.Node{
				ui.IconButton{Icon: lucide.ChevronLeft, Label: "Back to threads", OnPress: func() { a.selected.Set("") }},
				core.View{Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{a.contentHeader(cx)}},
			}},
			core.View{Style: ui.FillRest, Children: []core.Node{a.threadPane(cx)}},
		}}
	}
	return core.View{Style: core.Style{FlexGrow: 1, Direction: core.Column}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(8, 12), Gap: 8}, Children: []core.Node{
			ui.Text(a.envName(), 16, ui.Bold, colors.Text), ui.Spacer{},
			ui.IconButton{Icon: lucide.Search, Label: "Search", TestID: "open-palette", OnPress: a.openPalette},
			ui.IconButton{Icon: lucide.Settings, Label: "Settings", TestID: "settings", OnPress: a.openSettings},
		}},
		core.View{Style: ui.FillRest, Children: []core.Node{a.sidebar(cx)}},
	}}
}

func (a *app) dialogs(cx *core.Scope) core.Node {
	return core.Group{a.newProjectDialog(cx), a.newThreadDialog(cx), a.settingsDialog(cx), a.diffDialog(cx), a.runsDialog(cx),
		a.renameDialog(cx), a.deleteDialog(cx), a.paletteDialog(cx), a.approvalDialog(cx), a.discardDialog(cx),
		a.editMessageDialog(cx), a.newBranchDialog(cx), a.branchesDialog(cx), a.promptArgsDialog(cx),
		a.devicesDialog(cx)}
}

// approvalDialog asks about the first tool call a supervised run is
// waiting on.
func (a *app) approvalDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	approvals := a.approvals.Get()
	if len(approvals) == 0 {
		return ui.Dialog{Open: func() bool { return false }}
	}
	request := approvals[0]
	description := "A supervised agent wants to use " + request.Tool + "."
	for _, t := range a.threads.Get() {
		if t.ID == request.ThreadID {
			description = "In " + t.Title + ", the agent wants to use " + request.Tool + "."
			break
		}
	}
	var children []core.Node
	if raw, err := json.MarshalIndent(request.Input, "", "  "); err == nil && len(request.Input) > 0 {
		children = append(children, core.Scroll{Style: core.Style{MaxHeight: core.Pt(280), Background: colors.Inset(),
			CornerRadius: 8, Padding: core.All(10)}, Children: []core.Node{
			core.Text{Content: string(raw), Selectable: core.SelectableOn, Style: core.Style{FontSize: 11.5, LineHeight: 16, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}})
	}
	if len(approvals) > 1 {
		children = append(children, ui.Text(fmt.Sprintf("%d more waiting", len(approvals)-1), 11.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{
		Open:         func() bool { return len(a.approvals.Get()) > 0 },
		OnOpenChange: func(bool, base.Reason) {},
		Title:        "Allow tool call?",
		Description:  description,
		Children:     children,
		TestID:       "approval-dialog",
		Footer: []core.Node{
			ui.Button{Text: "Deny", Icon: lucide.X, Variant: ui.ButtonGhost, TestID: "approval-deny", OnPress: func() {
				a.resolveApproval(request.ID, false, "Denied by the person running T3Go.")
			}},
			ui.Button{Text: "Allow", Icon: lucide.Check, Variant: ui.ButtonPrimary, TestID: "approval-allow", OnPress: func() {
				a.resolveApproval(request.ID, true, "")
			}},
		},
	}
}

func closeDialog(sig *core.Signal[bool]) func(bool, base.Reason) {
	return func(open bool, _ base.Reason) { sig.Set(open) }
}

// paletteDialog searches the environment's threads and actions.
func (a *app) paletteDialog(cx *core.Scope) core.Node {
	return ui.CommandDialog{
		Open: a.showPalette.Get, OnOpenChange: closeDialog(a.showPalette),
		TestID: "palette",
		Command: ui.Command{
			Items: a.commandItems, OnSelect: a.runCommand,
			Placeholder: "Search threads and actions…", SearchLabel: "Search commands",
			Empty: "No threads or commands match.", Label: "Commands", TestID: "palette-command",
		},
	}
}

func (a *app) newProjectDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return ui.Dialog{Open: a.showNewProject.Get, OnOpenChange: closeDialog(a.showNewProject), Title: "New project",
		Description: "Register a directory the agent can work in.", Children: []core.Node{
			ui.Input{Input: base.Input{Value: a.newProjectName.Get, OnChange: a.newProjectName.Set, Placeholder: "Name (optional)", Label: "Project name"}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
				core.View{Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
					ui.Input{Input: base.Input{Value: a.newProjectPath.Get, OnChange: a.newProjectPath.Set, Placeholder: "/path/to/project", Label: "Project path"}},
				}},
				ui.Button{Text: "Browse…", Icon: lucide.FolderOpen, Variant: ui.ButtonSecondary, OnPress: a.browseProject},
			}},
			core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showNewProject.Set(false) }},
			ui.Button{Text: "Add project", Icon: lucide.FolderPlus, Variant: ui.ButtonPrimary, OnPress: a.createProject},
		}}
}

func (a *app) newThreadDialog(cx *core.Scope) core.Node {
	return ui.Dialog{Open: a.showNewThread.Get, OnOpenChange: closeDialog(a.showNewThread), Title: "New thread",
		Description: "The agent runs in this project's directory, or in a fresh worktree.", TestID: "new-thread-dialog",
		Children: []core.Node{core.Dynamic(a.newThreadBody)},
		Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showNewThread.Set(false) }},
			ui.Button{Text: "Create thread", Icon: lucide.MessageSquarePlus, Variant: ui.ButtonPrimary, TestID: "create-thread", OnPress: a.createThread},
		}}
}

// newThreadBody reads the dialog's signals in its own scope, so changing a
// provider or model does not rebuild the dialog itself.
func (a *app) newThreadBody(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	provider := a.newThreadProv.Get()
	var providerButtons []core.Node
	for _, p := range a.providers.Get() {
		p := p
		variant := ui.ButtonSecondary
		if p.ID == provider {
			variant = ui.ButtonPrimary
		}
		label := p.Name
		if !p.Installed {
			label += " (missing)"
		}
		providerButtons = append(providerButtons, ui.Button{Text: label, Variant: variant, Size: ui.SizeSmall,
			Disabled: func() bool { return !p.Installed }, OnPress: func() {
				a.newThreadProv.Set(p.ID)
				for _, m := range a.models.Get() {
					if m.Provider == p.ID {
						a.newThreadModel.Set(m.ID)
						break
					}
				}
			}})
	}
	var modelItems []core.Node
	modelLabel := "Choose model"
	for _, m := range a.models.Get() {
		if m.Provider != provider {
			continue
		}
		m := m
		if m.ID == a.newThreadModel.Get() {
			modelLabel = m.Name
		}
		modelItems = append(modelItems, ui.MenuItem{Text: m.Name, TestID: "new-thread-model-" + m.ID,
			Checked:  func() bool { return a.newThreadModel.Get() == m.ID },
			OnSelect: func() { a.newThreadModel.Set(m.ID) }})
	}
	var modelField core.Node = ui.Text("No models for this provider", 12, ui.Regular, colors.Faint)
	if len(modelItems) > 0 {
		modelField = ui.Menu{TestID: "new-thread-model",
			Trigger:   ui.Button{Text: modelLabel, Variant: ui.ButtonSecondary, Size: ui.SizeSmall},
			MaxHeight: 320, Children: modelItems}
	}
	permissions := []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits, proto.PermissionAuto, proto.PermissionFullAccess}
	var permButtons []core.Node
	for _, p := range permissions {
		p := p
		variant := ui.ButtonSecondary
		if p == a.newThreadPerm.Get() {
			variant = ui.ButtonPrimary
		}
		permButtons = append(permButtons, ui.Button{Text: string(p), Variant: variant, Size: ui.SizeSmall, OnPress: func() { a.newThreadPerm.Set(p) }})
	}
	worktree := "Off"
	if a.newThreadWT.Get() {
		worktree = "On"
	}
	projects := a.projects.Get()
	projectID := a.newThreadProj.Get()
	var projectButtons []core.Node
	for _, pr := range projects {
		pr := pr
		variant := ui.ButtonSecondary
		if pr.ID == projectID {
			variant = ui.ButtonPrimary
		}
		projectButtons = append(projectButtons, ui.Button{Text: pr.Name, Variant: variant, Size: ui.SizeSmall,
			TestID: "new-thread-project-" + pr.ID, OnPress: func() { a.newThreadProj.Set(pr.ID) }})
	}
	fields := []core.Node{
		ui.Input{Input: base.Input{Value: a.newThreadTitle.Get, OnChange: a.newThreadTitle.Set, Placeholder: "What is this thread about? (optional)", Label: "Thread title"}},
	}
	if len(projects) > 1 {
		fields = append(fields,
			ui.Text("Project", 11, ui.Semibold, colors.Muted),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: projectButtons})
	}
	fields = append(fields,
		ui.Text("Provider", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: providerButtons},
		ui.Text("Model", 11, ui.Semibold, colors.Muted),
		modelField,
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
			ui.Text("Own worktree", 12, ui.Regular, colors.Muted), ui.Spacer{},
			ui.Button{Text: worktree, Variant: ui.ButtonSecondary, Size: ui.SizeSmall, TestID: "toggle-worktree", OnPress: func() {
				a.newThreadWT.Set(!a.newThreadWT.GetUntracked())
			}},
		}},
		ui.Text("Permission", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: permButtons},
	)
	if a.newThreadWT.Get() {
		fields = append(fields, ui.Input{Input: base.Input{Value: a.newThreadBranch.Get, OnChange: a.newThreadBranch.Set,
			Placeholder: "t3go/<slug>", Label: "Branch (optional)"}})
	}
	fields = append(fields, core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
		return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
	}})
	return core.Scroll{Style: core.Style{MaxHeight: core.Pct(60)},
		Children: []core.Node{core.View{Style: core.Style{Direction: core.Column, Gap: 12,
			Padding: core.Edges{Right: 16}}, Children: fields}}}
}

func (a *app) settingsDialog(cx *core.Scope) core.Node {
	return ui.Dialog{Open: a.showSettings.Get, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			a.closeSettings()
		}
	}, Title: "Settings", Description: "Defaults for new threads in this environment.", TestID: "settings-dialog",
		Children: []core.Node{core.Dynamic(a.settingsBody)},
		Footer: []core.Node{
			ui.Button{Text: "Disconnect", Variant: ui.ButtonGhost, TestID: "disconnect", OnPress: func() { a.closeSettings(); a.disconnect() }},
			ui.Spacer{},
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, OnPress: a.closeSettings},
			ui.Button{Text: "Save", Icon: lucide.Check, Variant: ui.ButtonPrimary, TestID: "settings-save", OnPress: func() {
				a.closeSettings()
				a.saveSettingsDraft()
			}},
		}}
}

// settingsGeneral is the environment defaults page of settings.
func (a *app) settingsGeneral(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	draft := a.settingsDraft.Get()
	update := func(fn func(*proto.Settings)) {
		a.settingsDraft.Update(func(s proto.Settings) proto.Settings {
			fn(&s)
			return s
		})
	}
	provider := draft.DefaultProvider
	var providerButtons []core.Node
	for _, p := range a.providers.Get() {
		p := p
		variant := ui.ButtonSecondary
		if p.ID == provider {
			variant = ui.ButtonPrimary
		}
		label := p.Name
		if !p.Installed {
			label += " (missing)"
		}
		providerButtons = append(providerButtons, ui.Button{Text: label, Variant: variant, Size: ui.SizeSmall,
			Disabled: func() bool { return !p.Installed }, TestID: "settings-provider-" + p.ID, OnPress: func() {
				update(func(s *proto.Settings) {
					s.DefaultProvider = p.ID
					s.DefaultModel = ""
					for _, m := range a.models.GetUntracked() {
						if m.Provider == p.ID {
							s.DefaultModel = m.ID
							break
						}
					}
				})
			}})
	}
	var modelItems []core.Node
	modelLabel := "Choose model"
	for _, m := range a.models.Get() {
		if m.Provider != provider {
			continue
		}
		m := m
		if m.ID == draft.DefaultModel {
			modelLabel = m.Name
		}
		modelItems = append(modelItems, ui.MenuItem{Text: m.Name, TestID: "settings-model-" + m.ID,
			Checked:  func() bool { return a.settingsDraft.Get().DefaultModel == m.ID },
			OnSelect: func() { update(func(s *proto.Settings) { s.DefaultModel = m.ID }) }})
	}
	var modelField core.Node = ui.Text("No models for this provider", 12, ui.Regular, colors.Faint)
	if len(modelItems) > 0 {
		modelField = ui.Menu{TestID: "settings-model",
			Trigger:   ui.Button{Text: modelLabel, Variant: ui.ButtonSecondary, Size: ui.SizeSmall},
			MaxHeight: 320, Children: modelItems}
	}
	perm := proto.PermissionMode(draft.DefaultPermission)
	if !perm.Valid() {
		perm = proto.PermissionFullAccess
	}
	permissions := []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits, proto.PermissionAuto, proto.PermissionFullAccess}
	var permButtons []core.Node
	for _, p := range permissions {
		p := p
		variant := ui.ButtonSecondary
		if p == perm {
			variant = ui.ButtonPrimary
		}
		permButtons = append(permButtons, ui.Button{Text: string(p), Variant: variant, Size: ui.SizeSmall,
			TestID: "settings-perm-" + string(p), OnPress: func() {
				update(func(s *proto.Settings) { s.DefaultPermission = string(p) })
			}})
	}
	var themeButtons []core.Node
	for _, theme := range []string{"system", "light", "dark"} {
		theme := theme
		variant := ui.ButtonSecondary
		if theme == draft.Theme {
			variant = ui.ButtonPrimary
		}
		themeButtons = append(themeButtons, ui.Button{Text: theme, Variant: variant, Size: ui.SizeSmall,
			TestID: "settings-theme-" + theme, OnPress: func() {
				update(func(s *proto.Settings) { s.Theme = theme })
			}})
	}
	fields := []core.Node{
		ui.Input{Input: base.Input{Value: a.settingsTitle.Get, OnChange: a.settingsTitle.Set,
			Placeholder: "Environment name", Label: "Environment name"}},
		ui.Text("Default provider", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: providerButtons},
		ui.Text("Default model", 11, ui.Semibold, colors.Muted),
		modelField,
		ui.Text("Default permission", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: permButtons},
		ui.Text("Theme", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: themeButtons},
		core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Text{Content: "Server", Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.Text{Content: a.serverURL.Get(), MaxLines: 1, Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}},
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 12, Padding: core.Edges{Right: 16}}, Children: fields}
}

// promptArgsDialog asks for the arguments of an MCP prompt.
func (a *app) promptArgsDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	defs := a.promptArgsDefs
	var rows []core.Node
	for index, arg := range defs {
		index, arg := index, arg
		label := arg.Name
		if arg.Required {
			label += " *"
		}
		placeholder := arg.Description
		if placeholder == "" {
			placeholder = "Value"
		}
		rows = append(rows, core.Dynamic(func(cx *core.Scope) core.Node {
			return ui.Input{Input: base.Input{
				Value: func() string {
					state := a.promptArgValues.Get()
					if index < len(state.Values) {
						return state.Values[index]
					}
					return ""
				},
				OnChange: func(value string) {
					state := a.promptArgValues.GetUntracked()
					if index < len(state.Values) {
						state.Values[index] = value
						a.promptArgValues.Set(state)
					}
				},
				Placeholder: placeholder, Label: label, TestID: "prompt-arg-" + arg.Name,
			}}
		}))
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("This prompt takes no arguments.", 11.5, ui.Regular, colors.Faint))
	}
	name := a.promptName.Get()
	return ui.Dialog{Open: a.showPromptArgs.Get, OnOpenChange: closeDialog(a.showPromptArgs),
		Title: "Run " + name, Description: "Fill in the prompt's arguments; empty ones are skipped.",
		TestID: "prompt-args-dialog", Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: rows},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showPromptArgs.Set(false) }},
			ui.Button{Text: "Insert", Variant: ui.ButtonPrimary, TestID: "confirm-prompt-args",
				OnPress: a.submitPromptArgs},
		}}
}

// branchesDialog switches, creates or pulls the thread's branch.
func (a *app) branchesDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	var rows []core.Node
	for _, ref := range a.branches.Get() {
		if ref.Remote {
			continue
		}
		ref := ref
		label := ref.Name
		if ref.Current {
			label += "  ✓"
		}
		rows = append(rows, ui.Button{Text: label, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
			TestID: "header-branch-" + ref.Name, Flex: 1, OnPress: func() {
				a.showBranches.Set(false)
				a.switchBranch(ref.Name)
			}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No local branches.", 11.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{Open: a.showBranches.Get, OnOpenChange: closeDialog(a.showBranches), Title: "Branches",
		Description: "Switch this thread's checkout, or make a new branch.", TestID: "branches-dialog",
		Children: []core.Node{core.Scroll{Style: core.Style{MaxHeight: core.Pt(300)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 3, Padding: core.Edges{Right: 12}}, Children: rows},
		}}}, Footer: []core.Node{
			ui.Button{Text: "Pull", Icon: lucide.RefreshCw, Variant: ui.ButtonGhost, TestID: "branch-pull",
				OnPress: func() { a.showBranches.Set(false); a.pullBranch() }},
			ui.Button{Text: "New branch…", Icon: lucide.GitBranch, Variant: ui.ButtonSecondary, TestID: "branch-new",
				OnPress: func() { a.showBranches.Set(false); a.openNewBranch() }},
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, OnPress: func() { a.showBranches.Set(false) }},
		}}
}

// devicesDialog picks a simulator to show.
func (a *app) devicesDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	devices := a.devices.Get()
	selected := a.deviceSelected.Get()
	var rows []core.Node
	for i, device := range devices {
		device, i := device, i
		label := device.Name
		if device.ID == selected {
			label += "  ✓"
		}
		rows = append(rows, base.Button{OnPress: func(core.PressEvent) {
			a.showDevices.Set(false)
			a.selectDevice(device)
		}, TestID: fmt.Sprintf("device-%s-%d", device.ID, i), Label: device.Name,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pct(100),
				Padding: core.Symmetric(5, 7), CornerRadius: 6, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
			}, Children: []core.Node{
				ui.Icon(lucide.MonitorSmartphone, 12, colors.Muted),
				core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: strings.ToLower(device.State), Style: core.Style{FontSize: 10},
					StyleFunc: func() core.Style {
						if device.State == "booted" {
							return core.Style{Color: colors.Up()}
						}
						return core.Style{Color: colors.Faint()}
					}},
			}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No simulators found.", 11.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{Open: a.showDevices.Get, OnOpenChange: closeDialog(a.showDevices), Title: "Devices",
		Description: "Pick a simulator to see its screen.", TestID: "devices-dialog",
		Children: []core.Node{core.Scroll{Style: core.Style{MaxHeight: core.Pt(300)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 2, Padding: core.Edges{Right: 12}}, Children: rows},
		}}}, Footer: []core.Node{
			ui.Button{Text: "Refresh", Icon: lucide.RefreshCw, Variant: ui.ButtonGhost, TestID: "devices-dialog-refresh",
				OnPress: a.loadDevices},
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, OnPress: func() { a.showDevices.Set(false) }},
		}}
}

// newBranchDialog asks for a branch to create and check out.
func (a *app) newBranchDialog(cx *core.Scope) core.Node {
	return ui.Dialog{Open: a.showBranch.Get, OnOpenChange: closeDialog(a.showBranch), Title: "New branch",
		Description: "Create a branch in this thread's checkout and switch to it.", TestID: "new-branch-dialog",
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			return ui.Input{Input: base.Input{Value: a.branchName.Get, OnChange: a.branchName.Set,
				Placeholder: "t3go/<slug>", Label: "Branch name"}}
		})}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showBranch.Set(false) }},
			ui.Button{Text: "Create branch", Icon: lucide.GitBranch, Variant: ui.ButtonPrimary, TestID: "confirm-new-branch",
				OnPress: a.createBranch},
		}}
}

func (a *app) diffDialog(cx *core.Scope) core.Node {
	return ui.Dialog{Open: a.showDiff.Get, OnOpenChange: closeDialog(a.showDiff), Title: "Changes",
		Description: "Commit, push, and open a pull request for this thread's checkout.", TestID: "diff-dialog",
		Children: []core.Node{core.Dynamic(a.diffBody)},
		Footer: []core.Node{
			ui.Button{Text: "Close", Variant: ui.ButtonSecondary, OnPress: func() { a.showDiff.Set(false) }},
		}}
}

// diffBody is the Changes dialog's status, source control actions and
// diff, rebuilt on its own so typing a commit message keeps the dialog
// still.
func (a *app) diffBody(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	git := a.detail.Get().Git
	var status core.Node
	switch {
	case git == nil:
		status = ui.Text("This thread is not in a git repository.", 12, ui.Regular, colors.Faint)
	case git.Error != "":
		status = ui.Text(git.Error, 12, ui.Regular, colors.Danger)
	default:
		branch := git.Branch
		if branch == "" {
			branch = "detached HEAD"
		}
		var detail []string
		if git.Dirty {
			detail = append(detail, "uncommitted changes")
		}
		if git.Ahead > 0 {
			detail = append(detail, fmt.Sprintf("%d ahead", git.Ahead))
		}
		if git.Behind > 0 {
			detail = append(detail, fmt.Sprintf("%d behind", git.Behind))
		}
		var branchField core.Node = ui.Text(branch, 12, ui.Semibold, colors.Text)
		if refs := a.branches.Get(); len(refs) > 1 {
			var items []core.Node
			for _, ref := range refs {
				ref := ref
				items = append(items, ui.MenuItem{Text: ref.Name, TestID: "git-branch-" + ref.Name,
					Checked:  func() bool { return ref.Current },
					OnSelect: func() { a.switchBranch(ref.Name) }})
			}
			branchField = ui.Menu{TestID: "git-branch",
				Trigger:   ui.Button{Text: branch, Icon: lucide.GitBranch, Variant: ui.ButtonGhost, Size: ui.SizeSmall},
				MaxHeight: 320, Children: items}
		}
		line := []core.Node{branchField}
		if len(detail) > 0 {
			line = append(line, core.Text{Content: strings.Join(detail, " · "), MaxLines: 1,
				Style:     core.Style{FontSize: 11.5, FlexShrink: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		}
		status = core.View{Style: core.Style{Direction: core.Column, MinWidth: core.Pt(0), Gap: 1}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, MinWidth: core.Pt(0)}, Children: line},
			core.Text{Content: git.Root, MaxLines: 1, Style: core.Style{FontSize: 11},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}}
	}
	buttons := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
		ui.Button{Text: "Commit", Icon: lucide.Check, Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
			TestID: "git-commit", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitCommit) }},
		ui.Button{Text: "Push", Icon: lucide.ArrowUp, Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
			TestID: "git-push", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitPush) }},
		ui.Button{Text: "Create PR", Icon: lucide.GitPullRequest, Variant: ui.ButtonPrimary, Size: ui.SizeSmall,
			TestID: "git-pr", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitCommitPR) }},
		core.Show{When: a.gitBusy.Get, Then: func(*core.Scope) core.Node { return ui.Spinner{Size: 14, Label: "Running git action"} }},
	}}
	fields := []core.Node{
		status,
		ui.Input{Input: base.Input{Value: a.gitMessage.Get, OnChange: a.gitMessage.Set,
			Placeholder: "Commit message (defaults to an update)", Label: "Commit message"}},
		buttons,
	}
	if note := a.gitNote.Get(); note != "" {
		fields = append(fields, core.Text{Content: note, Style: core.Style{FontSize: 11.5, LineHeight: 16},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }})
	}
	if pr := a.gitPR.Get(); pr != "" {
		fields = append(fields, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.Text{Content: pr, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Accent()} }},
			ui.Button{Text: "Open", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "git-open-pr",
				OnPress: a.openPR},
		}})
	}
	fields = append(fields, core.View{Style: core.Style{Height: core.Pt(340), MinHeight: core.Pt(180),
		Background: colors.Inset(), CornerRadius: 8, Overflow: core.Hidden},
		Children: []core.Node{a.diffNode(cx, a.diffText.Get())}})
	return core.View{Style: core.Style{Direction: core.Column, Gap: 10, MinWidth: core.Pt(0)}, Children: fields}
}

// openPR opens the pull request a git action made.
func (a *app) openPR() {
	if pr := a.gitPR.GetUntracked(); pr != "" {
		a.cx.OpenURL(pr, nil)
	}
}

// runsDialog is the thread's run history, newest first, each with what it
// changed when it has a checkpoint.
func (a *app) runsDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	runs := a.detail.Get().Runs
	var rows []core.Node
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		meta := r.StartedAt.Local().Format("Jan 2 15:04")
		if r.EndedAt != nil {
			meta += " · " + r.EndedAt.Sub(r.StartedAt).Round(time.Second).String()
		}
		meta += " · " + r.Model
		if r.Usage != nil {
			if r.Usage.TotalTokens > 0 {
				meta += fmt.Sprintf(" · %d tokens", r.Usage.TotalTokens)
			}
			if r.Usage.CostUSD > 0 {
				meta += fmt.Sprintf(" · $%.2f", r.Usage.CostUSD)
			}
		}
		line := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.Text{Content: string(r.Status), MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: runColor(colors, r.Status)} }},
			core.Text{Content: meta, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}}
		if r.Checkpoint != "" {
			line.Children = append(line.Children, ui.Button{Text: "Diff", Variant: ui.ButtonGhost, Size: ui.SizeSmall,
				TestID: "run-diff-" + r.ID, OnPress: func() { a.openRunDiff(r.ID) }})
		}
		rows = append(rows, core.View{Style: core.Style{Gap: 3, Padding: core.Symmetric(8, 10), Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{
			line,
			core.Text{Content: r.Prompt, MaxLines: 2, Style: core.Style{FontSize: 12, LineHeight: 17},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No runs yet. Ask the agent to change something.", 12.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{Open: a.showRuns.Get, OnOpenChange: closeDialog(a.showRuns), Title: "History",
		Description: "Runs of this thread, newest first.", TestID: "runs-dialog",
		Children: []core.Node{
			core.Scroll{Style: core.Style{MaxHeight: core.Pt(420)}, Children: []core.Node{
				core.View{Style: core.Style{Gap: 6}, Children: rows},
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Close", Variant: ui.ButtonSecondary, OnPress: a.closeRuns},
		}}
}

func runColor(colors ui.Colors, status proto.RunStatus) core.Color {
	switch status {
	case proto.RunFailed:
		return colors.Danger()
	case proto.RunCancelled:
		return colors.Muted()
	case proto.RunRunning:
		return colors.Accent()
	}
	return colors.Text()
}

func (a *app) renameDialog(cx *core.Scope) core.Node {
	target := a.pendingRename.Get()
	title, label := "Rename", "Name"
	switch target.Kind {
	case "thread":
		title, label = "Rename thread", "Thread title"
	case "project":
		title, label = "Rename project", "Project name"
	}
	return ui.Dialog{
		Open: func() bool { return target.Kind != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				a.cancelRename()
			}
		},
		Title:       title,
		Description: "The new name is saved for everyone connected to this environment.",
		Children: []core.Node{
			ui.Input{Input: base.Input{Value: a.renameName.Get, OnChange: a.renameName.Set, Placeholder: label, Label: label,
				SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.confirmRename() }}},
		},
		Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: a.cancelRename},
			ui.Button{Text: "Save", Icon: lucide.Check, Variant: ui.ButtonPrimary, TestID: "confirm-rename", OnPress: a.confirmRename},
		},
	}
}

func (a *app) deleteDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	target := a.pendingDelete.Get()
	title, description := "Delete", ""
	switch target.Kind {
	case "thread":
		title = "Delete thread"
		name := target.Name
		if name == "" {
			name = "This thread"
		}
		description = name + " and its transcript will be removed. This cannot be undone."
	case "project":
		title = "Delete project"
		name := target.Name
		if name == "" {
			name = "This project"
		}
		description = name + " and all of its threads will be removed. This cannot be undone."
	}
	return ui.AlertDialog{
		Open: func() bool { return target.Kind != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				a.cancelDelete()
			}
		},
		Title:       title,
		Description: description,
		Actions: []ui.AlertAction{
			{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: a.cancelDelete, TestID: "cancel-delete"},
			{Text: "Delete", Variant: ui.ButtonPrimary, Color: func() core.Color { return colors.Danger() },
				OnPress: a.confirmDelete, TestID: "confirm-delete"},
		},
		TestID: "delete-dialog",
	}
}
