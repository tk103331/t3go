package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui/chat"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

// app is the desktop client's state: the connection, the environment's
// snapshot and the selected thread's transcript.
type app struct {
	cx    *core.Scope
	store core.Storage

	// connection
	serverURL  *core.Signal[string]
	token      *core.Signal[string]
	connected  *core.Signal[bool]
	connecting *core.Signal[bool]
	offline    *core.Signal[bool]
	connErr    *core.Signal[string]

	// environment snapshot
	projects  *core.Signal[[]proto.Project]
	threads   *core.Signal[[]proto.Thread]
	providers *core.Signal[[]proto.Provider]
	models    *core.Signal[[]proto.Model]
	settings  *core.Signal[proto.Settings]

	showArchived *core.Signal[bool]

	// the open thread
	selected *core.Signal[string]
	detail   *core.Signal[proto.ThreadDetail]
	messages *core.Signal[[]proto.Message]
	running  *core.Signal[bool]
	draft    *core.Signal[string]
	queue    *core.Signal[[]string]
	queued   map[string][]string

	// dialogs
	showNewProject *core.Signal[bool]
	showNewThread  *core.Signal[bool]
	showSettings   *core.Signal[bool]
	showPalette    *core.Signal[bool]
	settingsDraft  *core.Signal[proto.Settings]
	settingsTitle  *core.Signal[string]
	showDiff       *core.Signal[bool]
	showRuns       *core.Signal[bool]
	diffText       *core.Signal[string]
	pendingDelete  *core.Signal[itemTarget]
	pendingRename  *core.Signal[itemTarget]
	renameName     *core.Signal[string]

	newProjectName *core.Signal[string]
	newProjectPath *core.Signal[string]

	newThreadTitle  *core.Signal[string]
	newThreadProv   *core.Signal[string]
	newThreadModel  *core.Signal[string]
	newThreadPerm   *core.Signal[proto.PermissionMode]
	newThreadWT     *core.Signal[bool]
	newThreadProj   *core.Signal[string]
	newThreadBranch *core.Signal[string]

	sidebarOpen  *core.Signal[bool]
	sidebarWidth *core.Signal[core.Points]

	conv *chat.ConversationState

	mu           sync.Mutex
	cli          *client.Client
	streamCancel context.CancelFunc
}

func newApp(cx *core.Scope) *app {
	a := &app{
		cx:    cx,
		store: cx.Storage(),

		serverURL:  core.NewSignal(cx, ""),
		token:      core.NewSignal(cx, ""),
		connected:  core.NewSignal(cx, false),
		connecting: core.NewSignal(cx, false),
		offline:    core.NewSignal(cx, false),
		connErr:    core.NewSignal(cx, ""),

		projects:  sliceSignal[proto.Project](cx),
		threads:   sliceSignal[proto.Thread](cx),
		providers: sliceSignal[proto.Provider](cx),
		models:    sliceSignal[proto.Model](cx),
		settings:  core.NewSignal(cx, proto.DefaultSettings()),

		showArchived: core.NewSignal(cx, false),

		selected: core.NewSignal(cx, ""),
		detail:   core.NewSignalFunc(cx, proto.ThreadDetail{}, func(proto.ThreadDetail, proto.ThreadDetail) bool { return false }),
		messages: sliceSignal[proto.Message](cx),
		running:  core.NewSignal(cx, false),
		draft:    core.NewSignal(cx, ""),
		queue:    sliceSignal[string](cx),
		queued:   map[string][]string{},

		showNewProject: core.NewSignal(cx, false),
		showNewThread:  core.NewSignal(cx, false),
		showSettings:   core.NewSignal(cx, false),
		showPalette:    core.NewSignal(cx, false),
		settingsDraft:  core.NewSignal(cx, proto.DefaultSettings()),
		settingsTitle:  core.NewSignal(cx, ""),
		showDiff:       core.NewSignal(cx, false),
		showRuns:       core.NewSignal(cx, false),
		diffText:       core.NewSignal(cx, ""),
		pendingDelete: core.NewSignalFunc(cx, itemTarget{}, func(a, b itemTarget) bool {
			return a == b
		}),
		pendingRename: core.NewSignalFunc(cx, itemTarget{}, func(a, b itemTarget) bool {
			return a == b
		}),
		renameName: core.NewSignal(cx, ""),

		newProjectName: core.NewSignal(cx, ""),
		newProjectPath: core.NewSignal(cx, ""),

		newThreadTitle:  core.NewSignal(cx, ""),
		newThreadProv:   core.NewSignal(cx, ""),
		newThreadModel:  core.NewSignal(cx, ""),
		newThreadPerm:   core.NewSignal(cx, proto.PermissionFullAccess),
		newThreadWT:     core.NewSignal(cx, false),
		newThreadProj:   core.NewSignal(cx, ""),
		newThreadBranch: core.NewSignal(cx, ""),

		sidebarOpen:  core.NewSignal(cx, true),
		sidebarWidth: core.NewSignal(cx, core.Points(260)),
	}
	a.conv = chat.NewConversationState(cx)
	a.bindKeys()
	// The sidebar keeps the size and state it had last time.
	if raw, ok := a.store.GetItem("sidebarOpen"); ok {
		a.sidebarOpen.Set(raw != "false")
	}
	if raw, ok := a.store.GetItem("sidebarWidth"); ok {
		if w, err := strconv.ParseFloat(raw, 64); err == nil && w > 0 {
			a.sidebarWidth.Set(core.Points(w))
		}
	}
	url, _ := a.store.GetItem("serverUrl")
	token, _ := a.store.GetItem("token")
	a.serverURL.Set(url)
	a.token.Set(token)
	if url != "" {
		a.connect(url, token)
	}
	return a
}

func sliceSignal[T any](cx *core.Scope) *core.Signal[[]T] {
	return core.NewSignalFunc(cx, []T(nil), func(a, b []T) bool { return false })
}

// connect validates the address by loading the environment's state.
func (a *app) connect(url, token string) {
	url = strings.TrimSpace(url)
	token = strings.TrimSpace(token)
	if url == "" {
		a.connErr.Set("Enter the server address.")
		return
	}
	a.connecting.Set(true)
	a.connErr.Set("")
	c := client.New(url, token)
	a.mu.Lock()
	a.cli = c
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := c.State(ctx)
		a.cx.Post(func() {
			a.connecting.Set(false)
			if err != nil {
				a.connErr.Set(friendlyError(err))
				return
			}
			_ = a.store.SetItem("serverUrl", c.BaseURL)
			_ = a.store.SetItem("token", c.Token)
			a.serverURL.Set(c.BaseURL)
			a.token.Set(c.Token)
			a.applyState(st)
			a.offline.Set(false)
			a.connected.Set(true)
			a.startStream(c, st.Seq)
		})
	}()
}

func (a *app) startStream(c *client.Client, after int64) {
	a.mu.Lock()
	if a.streamCancel != nil {
		a.streamCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.streamCancel = cancel
	a.mu.Unlock()
	go c.Stream(ctx, after, func(e proto.Event) {
		a.cx.Post(func() {
			a.offline.Set(false)
			a.applyEvent(e)
		})
	}, func(err error) {
		a.cx.Post(func() {
			if errors.Is(err, client.ErrUnauthorized) {
				a.connErr.Set("The token is no longer valid.")
				return
			}
			a.offline.Set(true)
		})
	})
}

func (a *app) disconnect() {
	a.mu.Lock()
	if a.streamCancel != nil {
		a.streamCancel()
		a.streamCancel = nil
	}
	a.cli = nil
	a.mu.Unlock()
	a.store.RemoveItem("serverUrl")
	a.store.RemoveItem("token")
	a.connected.Set(false)
	a.offline.Set(false)
	a.queued = map[string][]string{}
	a.queue.Set(nil)
	a.selected.Set("")
	a.messages.Set(nil)
	a.detail.Set(proto.ThreadDetail{})
	a.serverURL.Set("")
}

func (a *app) client() *client.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cli
}

func (a *app) applyState(st proto.State) {
	a.applyTheme(st.Settings.Theme)
	// One rebuild for the whole snapshot: each signal on its own would
	// redraw the screen five times.
	a.cx.Batch(func() {
		a.projects.Set(st.Projects)
		a.threads.Set(st.Threads)
		a.providers.Set(st.Providers)
		a.models.Set(st.Models)
		a.settings.Set(st.Settings)
	})
}

// reload fetches the environment snapshot again.
func (a *app) reload() {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := c.State(ctx)
		if err != nil {
			return
		}
		a.cx.Post(func() { a.applyState(st) })
	}()
}

func (a *app) applyEvent(e proto.Event) {
	switch e.Type {
	case proto.EventThreadCreated, proto.EventThreadUpdated:
		t, ok := decodeData[proto.Thread](e.Data)
		if !ok {
			return
		}
		a.upsertThread(t)
		if a.selected.GetUntracked() == t.ID {
			d := a.detail.GetUntracked()
			d.Thread = t
			a.detail.Set(d)
			if t.Status != proto.ThreadRunning {
				a.running.Set(false)
				a.maybeFlush(t.ID)
			}
		}
	case proto.EventThreadDeleted:
		a.removeThread(e.ThreadID)
		if a.selected.GetUntracked() == e.ThreadID {
			a.selected.Set("")
			a.messages.Set(nil)
			a.detail.Set(proto.ThreadDetail{})
		}
	case proto.EventSettings:
		if settings, ok := decodeData[proto.Settings](e.Data); ok {
			a.applyTheme(settings.Theme)
			a.settings.Set(settings)
		}
	case proto.EventProject:
		if p, ok := decodeData[proto.Project](e.Data); ok && p.ID != "" {
			a.upsertProject(p)
			return
		}
		if gone, ok := decodeData[map[string]string](e.Data); ok {
			if id := gone["deleted"]; id != "" {
				a.removeProject(id)
			}
		}
	case proto.EventMessage:
		m, ok := decodeData[proto.Message](e.Data)
		if ok && m.ThreadID == a.selected.GetUntracked() {
			a.applyMessage(m)
		}
	case proto.EventMessageDelta:
		d, ok := decodeData[proto.MessageDelta](e.Data)
		if ok && e.ThreadID == a.selected.GetUntracked() {
			a.applyDelta(d)
		}
	case proto.EventRunStarted:
		if e.ThreadID == a.selected.GetUntracked() {
			a.running.Set(true)
		}
	case proto.EventRunFinished:
		if e.ThreadID == a.selected.GetUntracked() {
			a.running.Set(false)
			a.maybeFlush(e.ThreadID)
			if run, ok := decodeData[proto.Run](e.Data); ok {
				d := a.detail.GetUntracked()
				found := false
				for i := range d.Runs {
					if d.Runs[i].ID == run.ID {
						d.Runs[i] = run
						found = true
					}
				}
				if !found {
					d.Runs = append(d.Runs, run)
				}
				a.detail.Set(d)
			}
		}
	}
}

func (a *app) upsertThread(t proto.Thread) {
	list := a.threads.GetUntracked()
	next := make([]proto.Thread, len(list), len(list)+1)
	copy(next, list)
	for i := range next {
		if next[i].ID == t.ID {
			next[i] = t
			a.threads.Set(next)
			return
		}
	}
	a.threads.Set(append(next, t))
}

func (a *app) upsertProject(p proto.Project) {
	list := a.projects.GetUntracked()
	next := make([]proto.Project, len(list), len(list)+1)
	copy(next, list)
	for i := range next {
		if next[i].ID == p.ID {
			next[i] = p
			a.projects.Set(next)
			return
		}
	}
	a.projects.Set(append(next, p))
}

// removeProject drops a project and the threads that went with it.
func (a *app) removeProject(id string) {
	list := a.projects.GetUntracked()
	next := make([]proto.Project, 0, len(list))
	for _, p := range list {
		if p.ID != id {
			next = append(next, p)
		}
	}
	var keptThreads []proto.Thread
	var dropped []string
	for _, t := range a.threads.GetUntracked() {
		if t.ProjectID == id {
			dropped = append(dropped, t.ID)
			continue
		}
		keptThreads = append(keptThreads, t)
	}
	a.cx.Batch(func() {
		a.projects.Set(next)
		a.threads.Set(keptThreads)
	})
	for _, id := range dropped {
		if a.selected.GetUntracked() == id {
			a.selected.Set("")
			a.messages.Set(nil)
			a.detail.Set(proto.ThreadDetail{})
			a.running.Set(false)
			break
		}
	}
}

func (a *app) removeThread(id string) {
	list := a.threads.GetUntracked()
	next := make([]proto.Thread, 0, len(list))
	for _, t := range list {
		if t.ID != id {
			next = append(next, t)
		}
	}
	a.threads.Set(next)
}

func (a *app) applyMessage(m proto.Message) {
	list := a.messages.GetUntracked()
	next := make([]proto.Message, len(list), len(list)+1)
	copy(next, list)
	for i := range next {
		if next[i].ID == m.ID {
			next[i] = m
			a.messages.Set(next)
			return
		}
	}
	a.messages.Set(append(next, m))
}

func (a *app) applyDelta(d proto.MessageDelta) {
	if d.MessageID == "" || d.Delta == "" {
		return
	}
	list := a.messages.GetUntracked()
	next := make([]proto.Message, len(list))
	copy(next, list)
	for i := range next {
		if next[i].ID == d.MessageID {
			next[i].Content += d.Delta
			a.messages.Set(next)
			return
		}
	}
}

func decodeData[T any](v any) (T, bool) {
	var out T
	if v == nil {
		return out, false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return out, false
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, false
	}
	return out, true
}

func friendlyError(err error) string {
	if errors.Is(err, client.ErrUnauthorized) {
		return "That token was rejected. Check the token and try again."
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "No server is listening at that address."
	case strings.Contains(msg, "no such host"):
		return "That host could not be found."
	case strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timeout"):
		return "The server did not answer in time."
	}
	return msg
}

// call runs an API action off the UI loop.
func (a *app) call(fn func(ctx context.Context, c *client.Client) error, done func(err error)) {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := fn(ctx, c)
		if done != nil {
			a.cx.Post(func() { done(err) })
		}
	}()
}

func (a *app) selectThread(id string) {
	a.selected.Set(id)
	a.messages.Set(nil)
	a.detail.Set(proto.ThreadDetail{})
	a.running.Set(false)
	a.refreshQueue()
	a.loadDetail(id)
}

func (a *app) loadDetail(id string) {
	c := a.client()
	if c == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		d, err := c.ThreadDetail(ctx, id)
		if err != nil {
			return
		}
		a.cx.Post(func() {
			if a.selected.GetUntracked() != id {
				return
			}
			a.detail.Set(d)
			a.messages.Set(d.Messages)
			a.running.Set(d.Thread.Status == proto.ThreadRunning)
			a.maybeFlush(id)
			a.newThreadProv.Set(d.Thread.Provider)
			a.newThreadModel.Set(d.Thread.Model)
			a.newThreadPerm.Set(d.Thread.Permission)
		})
	}()
}

func (a *app) send() {
	id := a.selected.GetUntracked()
	text := strings.TrimSpace(a.draft.GetUntracked())
	if id == "" || text == "" || a.client() == nil {
		return
	}
	a.draft.Set("")
	a.connErr.Set("")
	// A prompt sent while the agent works waits its turn here; the next
	// run finishes before the client posts it.
	if a.running.GetUntracked() {
		a.queued[id] = append(a.queued[id], text)
		a.refreshQueue()
		return
	}
	a.sendPrompt(id, text)
}

// sendPrompt posts a prompt to a thread that has no live run.
func (a *app) sendPrompt(threadID, text string) {
	a.running.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.Send(ctx, threadID, text)
		return err
	}, func(err error) {
		if err != nil {
			a.running.Set(false)
			a.connErr.Set(friendlyError(err))
			// Keep the prompt rather than losing it to a failed request.
			a.queued[threadID] = append([]string{text}, a.queued[threadID]...)
			a.refreshQueue()
		}
	})
}

// maybeFlush starts the thread's next queued prompt once it is idle and
// on screen.
func (a *app) maybeFlush(threadID string) {
	if threadID != a.selected.GetUntracked() || a.running.GetUntracked() {
		return
	}
	queue := a.queued[threadID]
	if len(queue) == 0 {
		return
	}
	next := queue[0]
	a.queued[threadID] = queue[1:]
	a.refreshQueue()
	a.sendPrompt(threadID, next)
}

// refreshQueue shows the open thread's queued prompts.
func (a *app) refreshQueue() {
	queue := a.queued[a.selected.GetUntracked()]
	if len(queue) == 0 {
		a.queue.Set(nil)
		return
	}
	next := make([]string, len(queue))
	copy(next, queue)
	a.queue.Set(next)
}

// dropQueued removes the queued prompt at i.
func (a *app) dropQueued(i int) {
	id := a.selected.GetUntracked()
	queue := a.queued[id]
	if i < 0 || i >= len(queue) {
		return
	}
	a.queued[id] = append(queue[:i:i], queue[i+1:]...)
	a.refreshQueue()
}

func (a *app) stop() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	// Stop ends the turn and what was queued behind it.
	a.queued[id] = nil
	a.refreshQueue()
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.Stop(ctx, id)
		return err
	}, func(error) {})
}

// openRuns lists the open thread's runs.
func (a *app) openRuns() {
	if a.selected.GetUntracked() == "" {
		return
	}
	a.connErr.Set("")
	a.showRuns.Set(true)
}

func (a *app) closeRuns() {
	a.showRuns.Set(false)
}

// openRunDiff shows what one run changed, from its checkpoint.
func (a *app) openRunDiff(runID string) {
	id := a.selected.GetUntracked()
	if id == "" || runID == "" {
		return
	}
	a.closeRuns()
	a.loadDiff(id, runID)
}

func (a *app) openDiff() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.loadDiff(id, "")
}

func (a *app) loadDiff(threadID, runID string) {
	a.diffText.Set("Loading diff…")
	a.showDiff.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		diff, err := c.Diff(ctx, threadID, runID)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			if diff == "" {
				diff = "This run made no changes."
			}
			a.diffText.Set(diff)
		})
		return nil
	}, func(err error) {
		if err != nil {
			a.diffText.Set(friendlyError(err))
		}
	})
}

func (a *app) createProject() {
	path := strings.TrimSpace(a.newProjectPath.GetUntracked())
	name := strings.TrimSpace(a.newProjectName.GetUntracked())
	if path == "" {
		return
	}
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.CreateProject(ctx, name, path)
		return err
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
			return
		}
		a.showNewProject.Set(false)
		a.newProjectName.Set("")
		a.newProjectPath.Set("")
		a.reload()
	})
}

func (a *app) browseProject() {
	a.cx.PickDirectories(core.PickDirectoriesOptions{}, func(dirs []core.Directory, err error) {
		if err != nil || len(dirs) == 0 {
			return
		}
		if path := strings.TrimSpace(dirs[0].Path); path != "" {
			a.newProjectPath.Set(path)
			if a.newProjectName.Get() == "" {
				parts := strings.Split(strings.TrimRight(path, "/"), "/")
				a.newProjectName.Set(parts[len(parts)-1])
			}
		}
	})
}

func (a *app) createThread() {
	projectID := a.newThreadProj.GetUntracked()
	if projectID == "" {
		projectID = a.currentProject()
	}
	if projectID == "" {
		a.connErr.Set("Add a project first.")
		return
	}
	in := client.CreateThreadInput{
		ProjectID:  projectID,
		Title:      strings.TrimSpace(a.newThreadTitle.GetUntracked()),
		Provider:   a.newThreadProv.GetUntracked(),
		Model:      a.newThreadModel.GetUntracked(),
		Permission: a.newThreadPerm.GetUntracked(),
		Branch:     strings.TrimSpace(a.newThreadBranch.GetUntracked()),
		Worktree:   a.newThreadWT.GetUntracked(),
	}
	var created proto.Thread
	a.call(func(ctx context.Context, c *client.Client) error {
		t, err := c.CreateThread(ctx, in)
		created = t
		return err
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
			return
		}
		a.showNewThread.Set(false)
		a.newThreadTitle.Set("")
		a.newThreadBranch.Set("")
		a.reload()
		if created.ID != "" {
			a.selectThread(created.ID)
		}
	})
}

// currentProject is the project of the open thread, or the first one.
func (a *app) currentProject() string {
	selected := a.selected.GetUntracked()
	for _, t := range a.threads.GetUntracked() {
		if t.ID == selected {
			return t.ProjectID
		}
	}
	if projects := a.projects.GetUntracked(); len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

func (a *app) openNewProject() {
	a.connErr.Set("")
	a.showNewProject.Set(true)
}

// openNewThreadIn starts a thread in a named project, for a project's
// own action.
func (a *app) openNewThreadIn(projectID string) {
	a.openNewThread()
	if projectID != "" {
		a.newThreadProj.Set(projectID)
	}
}

func (a *app) openNewThread() {
	a.connErr.Set("")
	a.newThreadTitle.Set("")
	a.newThreadBranch.Set("")
	a.newThreadWT.Set(false)
	a.newThreadProj.Set(a.currentProject())
	settings := a.settings.GetUntracked()
	provider := settings.DefaultProvider
	model := settings.DefaultModel
	if provider == "" {
		for _, p := range a.providers.GetUntracked() {
			if p.Installed {
				provider = p.ID
				break
			}
		}
	}
	if model == "" {
		for _, m := range a.models.GetUntracked() {
			if m.Provider == provider {
				model = m.ID
				break
			}
		}
	}
	a.newThreadProv.Set(provider)
	a.newThreadModel.Set(model)
	perm := proto.PermissionMode(settings.DefaultPermission)
	if !perm.Valid() {
		perm = proto.PermissionFullAccess
	}
	a.newThreadPerm.Set(perm)
	a.showNewThread.Set(true)
}

// bindKeys gives the app its keyboard shortcuts.
func (a *app) bindKeys() {
	a.cx.OnKey("mod+k", func() {
		if a.connected.GetUntracked() {
			a.openPalette()
		}
	})
	a.cx.OnKey("mod+n", func() {
		if a.connected.GetUntracked() {
			a.openNewThread()
		}
	})
	a.cx.OnKey("mod+shift+n", func() {
		if a.connected.GetUntracked() {
			a.openNewProject()
		}
	})
	a.cx.OnKey("mod+,", func() {
		if a.connected.GetUntracked() {
			a.openSettings()
		}
	})
	a.cx.OnKey("mod+b", func() {
		if a.connected.GetUntracked() {
			a.setSidebarOpen(!a.sidebarOpen.GetUntracked())
		}
	})
}

// setSidebarOpen shows or hides the sidebar, and remembers it.
func (a *app) setSidebarOpen(open bool) {
	a.sidebarOpen.Set(open)
	_ = a.store.SetItem("sidebarOpen", strconv.FormatBool(open))
}

// setSidebarWidth resizes the sidebar, and remembers the size.
func (a *app) setSidebarWidth(w core.Points) {
	a.sidebarWidth.Set(w)
	_ = a.store.SetItem("sidebarWidth", strconv.FormatFloat(float64(w), 'f', 1, 64))
}

// openPalette searches the environment's threads and commands.
func (a *app) openPalette() {
	a.connErr.Set("")
	a.showPalette.Set(true)
}

func (a *app) closePalette() {
	a.showPalette.Set(false)
}

// runCommand runs the palette row with id, closing the palette first so
// the dialog or thread it opens has the screen.
func (a *app) runCommand(id string) {
	a.closePalette()
	switch {
	case id == "new-thread":
		a.openNewThread()
	case id == "new-project":
		a.openNewProject()
	case id == "settings":
		a.openSettings()
	case id == "toggle-archived":
		a.toggleShowArchived()
	case id == "toggle-sidebar":
		a.setSidebarOpen(!a.sidebarOpen.GetUntracked())
	case strings.HasPrefix(id, "thread:"):
		if threadID := strings.TrimPrefix(id, "thread:"); threadID != "" {
			a.selectThread(threadID)
		}
	}
}

// commandItems are the palette's rows: the app's actions, then the
// environment's threads.
func (a *app) commandItems() []base.CommandItem {
	projects := make(map[string]string, len(a.projects.Get()))
	for _, p := range a.projects.Get() {
		projects[p.ID] = p.Name
	}
	items := []base.CommandItem{
		{ID: "new-thread", Label: "New thread", Group: "Actions", Keywords: []string{"create", "start"}, Shortcut: "mod+n"},
		{ID: "new-project", Label: "New project", Group: "Actions", Keywords: []string{"create", "add", "folder"}, Shortcut: "mod+shift+n"},
		{ID: "settings", Label: "Settings", Group: "Actions", Keywords: []string{"defaults", "environment"}, Shortcut: "mod+,"},
		{ID: "toggle-archived", Label: a.archivedLabel(), Group: "View", Keywords: []string{"archive", "filter"}},
		{ID: "toggle-sidebar", Label: "Toggle sidebar", Group: "View", Keywords: []string{"panel"}, Shortcut: "mod+b"},
	}
	showArchived := a.showArchived.Get()
	for _, t := range a.threads.Get() {
		if t.Archived && !showArchived {
			continue
		}
		items = append(items, base.CommandItem{
			ID: "thread:" + t.ID, Label: t.Title, Description: projects[t.ProjectID], Group: "Threads",
			Keywords: []string{projects[t.ProjectID], t.Provider},
		})
	}
	return items
}

// openSettings starts a draft from the environment's settings, so closing
// the dialog keeps what was typed out of the saved values.
func (a *app) openSettings() {
	a.connErr.Set("")
	s := a.settings.GetUntracked()
	a.settingsDraft.Set(s)
	// The name has its own signal: a field follows it without rebuilding
	// the rest of the dialog on every keystroke.
	a.settingsTitle.Set(s.Title)
	a.showSettings.Set(true)
}

func (a *app) closeSettings() {
	a.showSettings.Set(false)
}

// saveSettingsDraft writes the dialog's draft, name included, into the
// environment.
func (a *app) saveSettingsDraft() {
	s := a.settingsDraft.GetUntracked()
	s.Title = a.settingsTitle.GetUntracked()
	a.saveSettings(s)
}

// applyTheme follows the environment's theme, system by default.
func (a *app) applyTheme(theme string) {
	switch theme {
	case "light":
		a.cx.SetAppearance(core.AppearanceLight)
	case "dark":
		a.cx.SetAppearance(core.AppearanceDark)
	default:
		a.cx.SetAppearance(core.AppearanceSystem)
	}
}

// saveSettings writes the draft and its values into the environment.
func (a *app) saveSettings(s proto.Settings) {
	a.settings.Set(s)
	a.applyTheme(s.Theme)
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.SaveSettings(ctx, s)
		return err
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
		}
	})
}

// itemTarget is a thread or project a dialog acts on.
type itemTarget struct {
	Kind string // "thread" or "project"
	ID   string
	Name string
}

func (a *app) askDeleteThread(t proto.Thread) {
	if t.ID == "" {
		return
	}
	a.connErr.Set("")
	a.pendingDelete.Set(itemTarget{Kind: "thread", ID: t.ID, Name: t.Title})
}

func (a *app) askDeleteProject(p proto.Project) {
	if p.ID == "" {
		return
	}
	a.connErr.Set("")
	a.pendingDelete.Set(itemTarget{Kind: "project", ID: p.ID, Name: p.Name})
}

func (a *app) cancelDelete() {
	a.pendingDelete.Set(itemTarget{})
}

// confirmDelete removes the pending thread or project, after the dialog
// asked. A project takes its threads with it.
func (a *app) confirmDelete() {
	target := a.pendingDelete.GetUntracked()
	if target.Kind == "" {
		return
	}
	a.pendingDelete.Set(itemTarget{})
	var removed []string
	if target.Kind == "project" {
		for _, t := range a.threads.GetUntracked() {
			if t.ProjectID == target.ID {
				removed = append(removed, t.ID)
			}
		}
	}
	a.call(func(ctx context.Context, c *client.Client) error {
		if target.Kind == "project" {
			return c.DeleteProject(ctx, target.ID)
		}
		return c.DeleteThread(ctx, target.ID)
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
			return
		}
		if target.Kind == "project" {
			if a.selected.GetUntracked() != "" {
				for _, id := range removed {
					if id == a.selected.GetUntracked() {
						a.selected.Set("")
						a.messages.Set(nil)
						a.detail.Set(proto.ThreadDetail{})
						break
					}
				}
			}
			a.reload()
			return
		}
		a.removeThread(target.ID)
		if a.selected.GetUntracked() == target.ID {
			a.selected.Set("")
			a.messages.Set(nil)
			a.detail.Set(proto.ThreadDetail{})
			a.running.Set(false)
		}
	})
}

func (a *app) askRenameThread(t proto.Thread) {
	if t.ID == "" {
		return
	}
	a.connErr.Set("")
	a.renameName.Set(t.Title)
	a.pendingRename.Set(itemTarget{Kind: "thread", ID: t.ID, Name: t.Title})
}

func (a *app) askRenameProject(p proto.Project) {
	if p.ID == "" {
		return
	}
	a.connErr.Set("")
	a.renameName.Set(p.Name)
	a.pendingRename.Set(itemTarget{Kind: "project", ID: p.ID, Name: p.Name})
}

func (a *app) cancelRename() {
	a.pendingRename.Set(itemTarget{})
}

// setArchived files a thread away or brings it back. Archiving the open
// thread closes it when the list hides archived ones.
func (a *app) setArchived(t proto.Thread, archived bool) {
	if t.ID == "" {
		return
	}
	a.connErr.Set("")
	var updated proto.Thread
	a.call(func(ctx context.Context, c *client.Client) error {
		u, err := c.UpdateThread(ctx, t.ID, map[string]any{"archived": archived})
		updated = u
		return err
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
			return
		}
		if updated.ID != "" {
			a.upsertThread(updated)
		}
		if archived && !a.showArchived.GetUntracked() && a.selected.GetUntracked() == t.ID {
			a.selected.Set("")
			a.messages.Set(nil)
			a.detail.Set(proto.ThreadDetail{})
			a.running.Set(false)
		}
	})
}

func (a *app) toggleShowArchived() {
	a.showArchived.Set(!a.showArchived.GetUntracked())
}

// confirmRename saves the pending thread's or project's new name.
func (a *app) confirmRename() {
	target := a.pendingRename.GetUntracked()
	name := strings.TrimSpace(a.renameName.GetUntracked())
	if target.Kind == "" || name == "" || name == target.Name {
		a.cancelRename()
		return
	}
	a.pendingRename.Set(itemTarget{})
	var updated proto.Thread
	a.call(func(ctx context.Context, c *client.Client) error {
		if target.Kind == "project" {
			_, err := c.UpdateProject(ctx, target.ID, name, "")
			return err
		}
		t, err := c.UpdateThread(ctx, target.ID, map[string]any{"title": name})
		updated = t
		return err
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
			return
		}
		if target.Kind == "project" {
			a.reload()
			return
		}
		if updated.ID != "" {
			a.upsertThread(updated)
		}
		if a.selected.GetUntracked() == target.ID {
			d := a.detail.GetUntracked()
			d.Thread = updated
			a.detail.Set(d)
		}
	})
}
