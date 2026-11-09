package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

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
	connErr    *core.Signal[string]

	// environment snapshot
	projects  *core.Signal[[]proto.Project]
	threads   *core.Signal[[]proto.Thread]
	providers *core.Signal[[]proto.Provider]
	models    *core.Signal[[]proto.Model]
	settings  *core.Signal[proto.Settings]

	// the open thread
	selected *core.Signal[string]
	detail   *core.Signal[proto.ThreadDetail]
	messages *core.Signal[[]proto.Message]
	running  *core.Signal[bool]
	draft    *core.Signal[string]

	// dialogs
	showNewProject *core.Signal[bool]
	showNewThread  *core.Signal[bool]
	showSettings   *core.Signal[bool]
	showDiff       *core.Signal[bool]
	diffText       *core.Signal[string]
	pendingDelete  *core.Signal[deleteTarget]

	newProjectName *core.Signal[string]
	newProjectPath *core.Signal[string]

	newThreadTitle *core.Signal[string]
	newThreadProv  *core.Signal[string]
	newThreadModel *core.Signal[string]
	newThreadPerm  *core.Signal[proto.PermissionMode]
	newThreadWT    *core.Signal[bool]

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
		connErr:    core.NewSignal(cx, ""),

		projects:  sliceSignal[proto.Project](cx),
		threads:   sliceSignal[proto.Thread](cx),
		providers: sliceSignal[proto.Provider](cx),
		models:    sliceSignal[proto.Model](cx),
		settings:  core.NewSignal(cx, proto.DefaultSettings()),

		selected: core.NewSignal(cx, ""),
		detail:   core.NewSignalFunc(cx, proto.ThreadDetail{}, func(proto.ThreadDetail, proto.ThreadDetail) bool { return false }),
		messages: sliceSignal[proto.Message](cx),
		running:  core.NewSignal(cx, false),
		draft:    core.NewSignal(cx, ""),

		showNewProject: core.NewSignal(cx, false),
		showNewThread:  core.NewSignal(cx, false),
		showSettings:   core.NewSignal(cx, false),
		showDiff:       core.NewSignal(cx, false),
		diffText:       core.NewSignal(cx, ""),
		pendingDelete: core.NewSignalFunc(cx, deleteTarget{}, func(a, b deleteTarget) bool {
			return a == b
		}),

		newProjectName: core.NewSignal(cx, ""),
		newProjectPath: core.NewSignal(cx, ""),

		newThreadTitle: core.NewSignal(cx, ""),
		newThreadProv:  core.NewSignal(cx, ""),
		newThreadModel: core.NewSignal(cx, ""),
		newThreadPerm:  core.NewSignal(cx, proto.PermissionFullAccess),
		newThreadWT:    core.NewSignal(cx, false),

		sidebarOpen:  core.NewSignal(cx, true),
		sidebarWidth: core.NewSignal(cx, core.Points(260)),
	}
	a.conv = chat.NewConversationState(cx)
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
			a.connected.Set(true)
			a.startStream(c)
		})
	}()
}

func (a *app) startStream(c *client.Client) {
	a.mu.Lock()
	if a.streamCancel != nil {
		a.streamCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.streamCancel = cancel
	a.mu.Unlock()
	go c.Stream(ctx, 0, func(e proto.Event) {
		a.cx.Post(func() { a.applyEvent(e) })
	}, func(err error) {
		a.cx.Post(func() {
			if errors.Is(err, client.ErrUnauthorized) {
				a.connErr.Set("The token is no longer valid.")
			}
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
	a.projects.Set(st.Projects)
	a.threads.Set(st.Threads)
	a.providers.Set(st.Providers)
	a.models.Set(st.Models)
	a.settings.Set(st.Settings)
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
			}
		}
	case proto.EventThreadDeleted:
		a.removeThread(e.ThreadID)
		if a.selected.GetUntracked() == e.ThreadID {
			a.selected.Set("")
			a.messages.Set(nil)
			a.detail.Set(proto.ThreadDetail{})
		}
	case proto.EventProject:
		a.reload()
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
			a.newThreadProv.Set(d.Thread.Provider)
			a.newThreadModel.Set(d.Thread.Model)
			a.newThreadPerm.Set(d.Thread.Permission)
		})
	}()
}

func (a *app) send() {
	id := a.selected.GetUntracked()
	text := strings.TrimSpace(a.draft.GetUntracked())
	c := a.client()
	if id == "" || text == "" || c == nil {
		return
	}
	a.draft.Set("")
	a.connErr.Set("")
	a.running.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.Send(ctx, id, text)
		return err
	}, func(err error) {
		if err != nil {
			a.running.Set(false)
			a.connErr.Set(friendlyError(err))
		}
	})
}

func (a *app) stop() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.Stop(ctx, id)
		return err
	}, func(error) {})
}

func (a *app) openDiff() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.diffText.Set("Loading diff…")
	a.showDiff.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		diff, err := c.Diff(ctx, id, "")
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
	projectID := a.currentProject()
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

func (a *app) openNewThread() {
	a.connErr.Set("")
	a.newThreadTitle.Set("")
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

func (a *app) saveSettings(s proto.Settings) {
	a.settings.Set(s)
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.SaveSettings(ctx, s)
		return err
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
		}
	})
}

// deleteTarget is a thread or project waiting for delete confirmation.
type deleteTarget struct {
	Kind string // "thread" or "project"
	ID   string
	Name string
}

func (a *app) askDeleteThread(t proto.Thread) {
	if t.ID == "" {
		return
	}
	a.connErr.Set("")
	a.pendingDelete.Set(deleteTarget{Kind: "thread", ID: t.ID, Name: t.Title})
}

func (a *app) askDeleteProject(p proto.Project) {
	if p.ID == "" {
		return
	}
	a.connErr.Set("")
	a.pendingDelete.Set(deleteTarget{Kind: "project", ID: p.ID, Name: p.Name})
}

func (a *app) cancelDelete() {
	a.pendingDelete.Set(deleteTarget{})
}

// confirmDelete removes the pending thread or project, after the dialog
// asked. A project takes its threads with it.
func (a *app) confirmDelete() {
	target := a.pendingDelete.GetUntracked()
	if target.Kind == "" {
		return
	}
	a.pendingDelete.Set(deleteTarget{})
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
