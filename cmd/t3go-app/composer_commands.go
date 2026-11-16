package main

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/proto"
)

type appCommand struct {
	Name  string
	Title string
	Desc  string
	Icon  ui.Icon
	Run   func()
}

func (a *app) appCommands() []appCommand {
	return []appCommand{
		{Name: "new", Title: "New thread", Desc: "Start a conversation", Icon: ui.IconCompose,
			Run: func() { a.openNewThread("") }},
		{Name: "search", Title: "Search messages", Desc: "Find text across this environment", Icon: ui.IconSearch, Run: a.openSearch},
		{Name: "palette", Title: "Command palette", Desc: "Commands and recent threads", Icon: ui.IconMenu, Run: a.openPalette},
		{Name: "environments", Title: "Environments", Desc: "Switch and manage saved environments", Icon: ui.IconGlobe,
			Run: func() { a.openSettings(settingsEnvironments) }},
		{Name: "settings", Title: "Settings", Desc: "Environments, defaults, providers and usage", Icon: ui.IconSettings,
			Run: func() { a.openSettings(settingsGeneral) }},
		{Name: "project", Title: "New project", Desc: "Register a folder on the environment", Icon: ui.IconFolder,
			Run: func() { a.showNewProject.Set(true) }},
		{Name: "files", Title: "Files", Desc: "Browse the thread's checkout", Icon: ui.IconFolder,
			Run: func() { a.openSurface(surfaceFiles) }},
		{Name: "changes", Title: "Changes", Desc: "Working tree, commit and push", Icon: ui.IconDocument,
			Run: func() { a.openSurface(surfaceChanges) }},
		{Name: "pr", Title: "Pull request", Desc: "Checks, reviews and merge", Icon: ui.IconGlobe,
			Run: func() { a.openSurface(surfacePullRequest) }},
		{Name: "graph", Title: "Git graph", Desc: "Recent commits", Icon: ui.IconList,
			Run: func() { a.openSurface(surfaceGraph) }},
		{Name: "terminal", Title: "Terminal", Desc: "Open a shell in the checkout", Icon: ui.IconDocument,
			Run: func() { a.openSurface(surfaceTerminal) }},
		{Name: "preview", Title: "Preview", Desc: "Open a local development server", Icon: ui.IconGlobe,
			Run: func() { a.openSurface(surfacePreview) }},
		{Name: "devices", Title: "Devices", Desc: "Simulators and screenshots", Icon: ui.IconPhone,
			Run: func() { a.openSurface(surfaceDevices) }},
		{Name: "sidechat", Title: "Side chat", Desc: "Ask without changing the main thread", Icon: ui.IconMessage,
			Run: func() { a.openSurface(surfaceSideChat) }},
		{Name: "browser", Title: "Browser", Desc: "Open an in-app web page", Icon: ui.IconGlobe,
			Run: func() { a.openSurface(surfaceBrowser) }},
		{Name: "stop", Title: "Stop", Desc: "Stop the running turn", Icon: ui.IconPause, Run: a.stop},
		{Name: "archive", Title: "Archive thread", Desc: "Hide this thread from the inbox", Icon: ui.IconTrash,
			Run: func() {
				if thread := a.detail.Peek().Thread; thread.ID != "" {
					a.setThreadArchived(thread, true)
				}
			}},
		{Name: "fork", Title: "Fork thread", Desc: "Copy this thread into a new one", Icon: ui.IconCopy,
			Run: func() {
				if thread := a.detail.Peek().Thread; thread.ID != "" {
					a.forkThread(thread.ID, "", "")
				}
			}},
		{Name: "reload", Title: "Reload", Desc: "Refresh threads and projects", Icon: ui.IconRefresh, Run: a.reload},
	}
}

func (a *app) loadComposerContext(threadID string) {
	if threadID == "" {
		a.fileIndex.Set(nil)
		a.agentCommands.Set(nil)
		a.composerThread = ""
		return
	}
	if a.composerThread == threadID && (len(a.fileIndex.Peek()) > 0 || len(a.agentCommands.Peek()) > 0) {
		return
	}
	c := a.client()
	if c == nil {
		return
	}
	a.composerThread = threadID
	a.composerBusy.Set(true)
	a.composerErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		files, filesErr := c.FilesRecursive(ctx, threadID)
		commands, commandsErr := c.AgentCommands(ctx, threadID)
		ui.Post(func() {
			if a.client() != c || a.composerThread != threadID {
				return
			}
			a.composerBusy.Set(false)
			a.fileIndex.Set(files)
			a.agentCommands.Set(commands)
			switch {
			case filesErr != nil && commandsErr != nil:
				a.composerErr.Set(friendlyError(filesErr))
			case filesErr != nil:
				a.composerErr.Set("Files unavailable: " + friendlyError(filesErr))
			case commandsErr != nil:
				a.composerErr.Set("Commands unavailable: " + friendlyError(commandsErr))
			}
		})
	}()
}

func slashQuery(draft string) (string, bool) {
	trimmed := strings.TrimSpace(draft)
	if !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\n") {
		return "", false
	}
	query := strings.TrimPrefix(trimmed, "/")
	if strings.ContainsAny(query, " \t") {
		return "", false
	}
	return query, true
}

func activeMention(draft string) (int, string, bool) {
	at := strings.LastIndex(draft, "@")
	if at < 0 {
		return 0, "", false
	}
	if at > 0 {
		before := rune(draft[at-1])
		if !unicode.IsSpace(before) && before != '(' && before != '[' {
			return 0, "", false
		}
	}
	query := draft[at+1:]
	if strings.ContainsAny(query, " \t\n,)]") {
		return 0, "", false
	}
	return at, query, true
}

type composerSuggestion struct {
	Key     string
	Label   string
	Detail  string
	Icon    ui.Icon
	Run     func()
	IsFile  bool
	Command *appCommand
	Agent   *proto.AgentCommand
}

func (a *app) composerSuggestions() []composerSuggestion {
	draft := a.draft.Get()
	if query, ok := slashQuery(draft); ok {
		query = strings.ToLower(query)
		var out []composerSuggestion
		appNames := map[string]bool{}
		for _, command := range a.appCommands() {
			command := command
			appNames[command.Name] = true
			if query != "" && !strings.Contains(strings.ToLower(command.Name), query) &&
				!strings.Contains(strings.ToLower(command.Title), query) {
				continue
			}
			out = append(out, composerSuggestion{
				Key:     "app:" + command.Name,
				Label:   "/" + command.Name,
				Detail:  command.Title + " · " + command.Desc,
				Icon:    command.Icon,
				Command: &command,
			})
		}
		for _, command := range a.agentCommands.Get() {
			command := command
			if appNames[command.Name] {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(command.Name), query) &&
				!strings.Contains(strings.ToLower(command.Description), query) {
				continue
			}
			out = append(out, composerSuggestion{
				Key:    "agent:" + command.Source + ":" + command.Name,
				Label:  "/" + command.Name,
				Detail: command.Kind + " · " + command.Source,
				Icon:   ui.IconDocument,
				Agent:  &command,
			})
		}
		if len(out) > 8 {
			out = out[:8]
		}
		return out
	}
	_, query, ok := activeMention(draft)
	if !ok {
		return nil
	}
	query = strings.ToLower(query)
	var out []composerSuggestion
	for _, entry := range a.fileIndex.Get() {
		if entry.Directory {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Path), query) {
			continue
		}
		path := entry.Path
		out = append(out, composerSuggestion{
			Key:    "file:" + path,
			Label:  "@" + path,
			Detail: humanSize(entry.Size),
			Icon:   ui.IconDocument,
			IsFile: true,
			Run: func() {
				draft := a.draft.Peek()
				if at, _, ok := activeMention(draft); ok {
					a.draft.Set(draft[:at] + "@" + path + " ")
				}
			},
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func (a *app) composerSuggestionView() ui.Node {
	items := signal.NewMemoFunc(func() []composerSuggestion { return a.composerSuggestions() }, nil)
	return ui.Show(signal.Func(func() bool { return len(items.Get()) > 0 }), func() ui.Node {
		return ui.View(ui.ViewProps{
			Style: ui.Style{Padding: ui.Symmetric(3, 2)},
		}, ui.Column(ui.Style{Gap: 2, Padding: ui.All(4), CornerRadius: 10,
			Background: colors.Background, BorderWidth: 1, BorderColor: colors.Separator},
			ui.For(items, func(item composerSuggestion) string { return item.Key },
				func(item signal.Accessor[composerSuggestion], _ signal.Accessor[int]) ui.Node {
					return ui.Dynamic(func() ui.Node {
						suggestion := item.Get()
						return ui.View(ui.ViewProps{
							OnPress: func() {
								if suggestion.IsFile {
									if suggestion.Run != nil {
										suggestion.Run()
									}
									return
								}
								if suggestion.Agent != nil {
									a.draft.Set("/" + suggestion.Agent.Name + " ")
									return
								}
								if suggestion.Command != nil {
									a.draft.Set("")
									suggestion.Command.Run()
								}
							},
							Style: ui.Style{Padding: ui.Symmetric(5, 7), CornerRadius: 7},
						}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 7},
							ui.Symbol(suggestion.Icon, ui.Style{Color: colors.Accent}),
							ui.Text(ui.Val(suggestion.Label), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Grow: 1, Lines: 1}),
							ui.Text(ui.Val(suggestion.Detail), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1}),
						))
					})
				}),
		))
	})
}

func (a *app) openPalette() {
	a.paletteQuery.Set("")
	a.showPalette.Set(true)
}

type paletteItem struct {
	Key    string
	Title  string
	Detail string
	Icon   ui.Icon
	Run    func()
}

func (a *app) paletteItems() []paletteItem {
	query := strings.ToLower(strings.TrimSpace(a.paletteQuery.Get()))
	match := func(value string) bool {
		return query == "" || strings.Contains(strings.ToLower(value), query)
	}
	var out []paletteItem
	for _, command := range a.appCommands() {
		command := command
		if !match(command.Name + " " + command.Title + " " + command.Desc) {
			continue
		}
		out = append(out, paletteItem{
			Key:    "command:" + command.Name,
			Title:  command.Title,
			Detail: command.Desc,
			Icon:   command.Icon,
			Run: func() {
				a.showPalette.Set(false)
				command.Run()
			},
		})
	}
	for _, thread := range a.threads.Get() {
		thread := thread
		if thread.SideChat || !match(threadTitle(thread)+" "+thread.Summary) {
			continue
		}
		out = append(out, paletteItem{
			Key:    "thread:" + thread.ID,
			Title:  threadTitle(thread),
			Detail: a.statusText(thread),
			Icon:   ui.IconMessage,
			Run: func() {
				a.showPalette.Set(false)
				a.selectThread(thread.ID)
			},
		})
	}
	for _, snapshot := range a.remoteEnvironmentThreads() {
		for _, thread := range snapshot.Threads {
			thread := thread
			if thread.SideChat || !match(threadTitle(thread)+" "+snapshot.Name+" "+thread.Summary) {
				continue
			}
			out = append(out, paletteItem{
				Key:    "remote:" + snapshot.ID + ":" + thread.ID,
				Title:  threadTitle(thread),
				Detail: snapshot.Name + " · " + thread.Model,
				Icon:   ui.IconGlobe,
				Run: func() {
					a.showPalette.Set(false)
					a.openEnvironmentThread(snapshot.ID, thread.ID)
				},
			})
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func paletteView(a *app) ui.Node {
	items := signal.NewMemoFunc(func() []paletteItem { return a.paletteItems() }, nil)
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(8, 12)},
			ui.Text(ui.Val("Command palette"), ui.Style{Font: fonts.Headline, Grow: 1}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close palette"),
				OnPress: func() { a.showPalette.Set(false) }}),
		),
		ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(4, 10)}},
			ui.TextField(ui.TextFieldProps{Value: a.paletteQuery, Placeholder: ui.Val("Search commands and threads"),
				ReturnKey: ui.ReturnSearch}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.Edges{Bottom: 16}}, ui.For(items,
				func(item paletteItem) string { return item.Key },
				func(item signal.Accessor[paletteItem], _ signal.Accessor[int]) ui.Node {
					return ui.Dynamic(func() ui.Node {
						entry := item.Get()
						return ui.View(ui.ViewProps{
							OnPress: entry.Run,
							Style:   ui.Style{Padding: ui.Symmetric(9, 12)},
						}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 9},
							ui.Symbol(entry.Icon, ui.Style{Color: colors.Accent}),
							ui.Column(ui.Style{Grow: 1, Gap: 1},
								ui.Text(ui.Val(entry.Title), ui.Style{Font: fonts.Footnote.Weighted(fonts.Medium), Lines: 1}),
								ui.Text(ui.Val(entry.Detail), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1}),
							),
							ui.Symbol(ui.IconForward, ui.Style{Color: colors.SecondaryLabel}),
						))
					})
				})),
		),
	)
}
