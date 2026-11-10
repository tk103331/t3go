package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
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
	selected         *core.Signal[string]
	detail           *core.Signal[proto.ThreadDetail]
	messages         *core.Signal[[]proto.Message]
	running          *core.Signal[bool]
	draft            *core.Signal[string]
	queue            *core.Signal[[]queuedPrompt]
	queued           map[string][]queuedPrompt
	attachments      *core.Signal[[]proto.Attachment]
	attachmentDrafts *core.Signal[map[string][]proto.Attachment]
	dictating        *core.Signal[bool]
	speech           *core.SpeechSession
	expanded         *core.Signal[map[string]bool]
	approvals        *core.Signal[[]proto.ApprovalRequest]

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
	gitMessage     *core.Signal[string]
	gitBusy        *core.Signal[bool]
	gitNote        *core.Signal[string]
	gitPR          *core.Signal[string]
	branches       *core.Signal[[]proto.Branch]
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

	sidebarOpen      *core.Signal[bool]
	sidebarWidth     *core.Signal[core.Points]
	sidebarQuery     *core.Signal[string]
	sidebarCollapsed *core.Signal[map[string]bool]
	seen             *core.Signal[map[string]time.Time]
	drafts           *core.Signal[map[string]string]
	pinned           *core.Signal[map[string]bool]
	draftSave        func() bool

	fileIndex       *core.Signal[[]proto.WorkspaceEntry]
	fileIndexThread *core.Signal[string]
	fileIndexBusy   *core.Signal[bool]

	rightOpen    *core.Signal[bool]
	rightWidth   *core.Signal[core.Points]
	rightDrag    core.Points
	tabs         *core.Signal[[]string]
	surface      *core.Signal[string]
	filesPath    *core.Signal[string]
	filesEntries *core.Signal[[]proto.WorkspaceEntry]
	fileSelected *core.Signal[string]
	file         *core.Signal[proto.WorkspaceFile]
	rightErr     *core.Signal[string]
	rightBusy    *core.Signal[bool]
	pullRequest  *core.Signal[proto.PullRequest]
	prDetail     *core.Signal[string]
	graph        *core.Signal[[]proto.GraphCommit]
	browserURL   *core.Signal[string]
	browserInput *core.Signal[string]
	browserNote  *core.Signal[string]
	browserBack  *core.Signal[bool]
	browserNext  *core.Signal[bool]
	browserRef   *core.Ref

	terminalSession *core.Signal[proto.TerminalSession]
	terminals       *core.Signal[[]proto.TerminalSession]
	terminalBusy    *core.Signal[bool]
	terminalErr     *core.Signal[string]

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

		selected:         core.NewSignal(cx, ""),
		detail:           core.NewSignalFunc(cx, proto.ThreadDetail{}, func(proto.ThreadDetail, proto.ThreadDetail) bool { return false }),
		messages:         sliceSignal[proto.Message](cx),
		running:          core.NewSignal(cx, false),
		draft:            core.NewSignal(cx, ""),
		queue:            sliceSignal[queuedPrompt](cx),
		queued:           map[string][]queuedPrompt{},
		attachments:      sliceSignal[proto.Attachment](cx),
		attachmentDrafts: core.NewSignalFunc(cx, map[string][]proto.Attachment{}, func(map[string][]proto.Attachment, map[string][]proto.Attachment) bool { return false }),
		dictating:        core.NewSignal(cx, false),
		expanded:         core.NewSignalFunc(cx, map[string]bool{}, func(map[string]bool, map[string]bool) bool { return false }),
		approvals:        sliceSignal[proto.ApprovalRequest](cx),

		showNewProject: core.NewSignal(cx, false),
		showNewThread:  core.NewSignal(cx, false),
		showSettings:   core.NewSignal(cx, false),
		showPalette:    core.NewSignal(cx, false),
		settingsDraft:  core.NewSignal(cx, proto.DefaultSettings()),
		settingsTitle:  core.NewSignal(cx, ""),
		showDiff:       core.NewSignal(cx, false),
		showRuns:       core.NewSignal(cx, false),
		diffText:       core.NewSignal(cx, ""),
		gitMessage:     core.NewSignal(cx, ""),
		gitBusy:        core.NewSignal(cx, false),
		gitNote:        core.NewSignal(cx, ""),
		gitPR:          core.NewSignal(cx, ""),
		branches:       sliceSignal[proto.Branch](cx),
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

		sidebarOpen:      core.NewSignal(cx, true),
		sidebarWidth:     core.NewSignal(cx, core.Points(260)),
		sidebarQuery:     core.NewSignal(cx, ""),
		sidebarCollapsed: core.NewSignalFunc(cx, map[string]bool{}, func(map[string]bool, map[string]bool) bool { return false }),
		seen:             core.NewSignalFunc(cx, map[string]time.Time{}, func(map[string]time.Time, map[string]time.Time) bool { return false }),
		drafts:           core.NewSignalFunc(cx, map[string]string{}, func(map[string]string, map[string]string) bool { return false }),
		pinned:           core.NewSignalFunc(cx, map[string]bool{}, func(map[string]bool, map[string]bool) bool { return false }),
		rightOpen:        core.NewSignal(cx, false),
		rightWidth:       core.NewSignal(cx, core.Points(360)),
		tabs:             sliceSignal[string](cx),
		surface:          core.NewSignal(cx, ""),
		filesPath:        core.NewSignal(cx, ""),
		filesEntries:     sliceSignal[proto.WorkspaceEntry](cx),
		fileSelected:     core.NewSignal(cx, ""),
		file:             core.NewSignal(cx, proto.WorkspaceFile{}),
		rightErr:         core.NewSignal(cx, ""),
		rightBusy:        core.NewSignal(cx, false),
		pullRequest:      core.NewSignal(cx, proto.PullRequest{}),
		prDetail:         core.NewSignal(cx, ""),
		graph:            sliceSignal[proto.GraphCommit](cx),
		browserURL:       core.NewSignal(cx, ""),
		browserInput:     core.NewSignal(cx, ""),
		browserNote:      core.NewSignal(cx, ""),
		browserBack:      core.NewSignal(cx, false),
		browserNext:      core.NewSignal(cx, false),
		browserRef:       &core.Ref{},
		fileIndex:        sliceSignal[proto.WorkspaceEntry](cx),
		fileIndexThread:  core.NewSignal(cx, ""),
		fileIndexBusy:    core.NewSignal(cx, false),
		terminalSession:  core.NewSignal(cx, proto.TerminalSession{}),
		terminals:        sliceSignal[proto.TerminalSession](cx),
		terminalBusy:     core.NewSignal(cx, false),
		terminalErr:      core.NewSignal(cx, ""),
	}
	a.conv = chat.NewConversationState(cx)
	a.bindKeys()
	// The sidebar keeps the size and state it had last time.
	if raw, ok := a.store.GetItem("sidebarOpen"); ok {
		a.sidebarOpen.Set(raw != "false")
	}
	if raw, ok := a.store.GetItem("rightOpen"); ok {
		a.rightOpen.Set(raw != "false")
	}
	if raw, ok := a.store.GetItem("rightWidth"); ok {
		if w, err := strconv.ParseFloat(raw, 64); err == nil && w > 0 {
			a.rightWidth.Set(core.Points(w))
		}
	}
	if raw, ok := a.store.GetItem("attachmentDrafts"); ok {
		var drafts map[string][]proto.Attachment
		if json.Unmarshal([]byte(raw), &drafts) == nil {
			a.attachmentDrafts.Set(drafts)
		}
	}
	if raw, ok := a.store.GetItem("drafts"); ok {
		var drafts map[string]string
		if json.Unmarshal([]byte(raw), &drafts) == nil {
			a.drafts.Set(drafts)
		}
	}
	if raw, ok := a.store.GetItem("pinnedThreads"); ok {
		var pinned map[string]bool
		if json.Unmarshal([]byte(raw), &pinned) == nil {
			a.pinned.Set(pinned)
		}
	}
	if raw, ok := a.store.GetItem("sidebarCollapsed"); ok {
		var collapsed map[string]bool
		if json.Unmarshal([]byte(raw), &collapsed) == nil {
			a.sidebarCollapsed.Set(collapsed)
		}
	}
	if raw, ok := a.store.GetItem("sidebarWidth"); ok {
		if w, err := strconv.ParseFloat(raw, 64); err == nil && w > 0 {
			a.sidebarWidth.Set(core.Points(w))
		}
	}
	url, _ := a.store.GetItem("serverUrl")
	token, _ := a.store.GetItem("token")
	// A dev script can point a fresh profile at an environment.
	if value := strings.TrimSpace(os.Getenv("T3GO_SERVER")); value != "" {
		url = value
	}
	if value := strings.TrimSpace(os.Getenv("T3GO_TOKEN")); value != "" {
		token = value
	}
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
	// A pair link pasted into either field fills in both.
	if u, t, ok := client.ParsePairing(url); ok {
		url = u
		if token == "" {
			token = t
		}
	}
	if u, t, ok := client.ParsePairing(token); ok {
		if url == "" {
			url = u
		}
		token = t
	}
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
			a.loadApprovals()
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
	// Files the user attached but never sent go with the connection.
	pending := append([]proto.Attachment(nil), a.attachments.GetUntracked()...)
	for _, queue := range a.queued {
		for _, prompt := range queue {
			pending = append(pending, prompt.Attachments...)
		}
	}
	a.discardAttachments(pending)
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
	a.stopDictation()
	a.queued = map[string][]queuedPrompt{}
	a.queue.Set(nil)
	a.attachments.Set(nil)
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
	a.rememberThreads(st.Threads)
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
			a.markSeen(t)
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
			a.draft.Set("")
			a.attachments.Set(nil)
			a.messages.Set(nil)
			a.detail.Set(proto.ThreadDetail{})
		}
	case proto.EventApproval:
		if request, ok := decodeData[proto.ApprovalRequest](e.Data); ok && request.ID != "" {
			a.addApproval(request)
		}
	case proto.EventApprovalResolved:
		if resolved, ok := decodeData[map[string]any](e.Data); ok {
			if id, _ := resolved["id"].(string); id != "" {
				a.removeApproval(id)
			}
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

// loadApprovals picks up requests that were waiting before the stream
// started.
func (a *app) loadApprovals() {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		approvals, err := c.Approvals(ctx, "")
		if err != nil {
			return
		}
		a.cx.Post(func() {
			if len(approvals) == 0 && len(a.approvals.GetUntracked()) == 0 {
				return
			}
			a.approvals.Set(approvals)
		})
	}()
}

func (a *app) addApproval(request proto.ApprovalRequest) {
	list := a.approvals.GetUntracked()
	for _, existing := range list {
		if existing.ID == request.ID {
			return
		}
	}
	next := make([]proto.ApprovalRequest, len(list), len(list)+1)
	copy(next, list)
	a.approvals.Set(append(next, request))
}

func (a *app) removeApproval(id string) {
	list := a.approvals.GetUntracked()
	next := make([]proto.ApprovalRequest, 0, len(list))
	for _, request := range list {
		if request.ID != id {
			next = append(next, request)
		}
	}
	a.approvals.Set(next)
}

// resolveApproval answers the waiting tool call, closing its dialog now
// and reopening the question if the server refused.
func (a *app) resolveApproval(id string, allow bool, message string) {
	a.removeApproval(id)
	a.call(func(ctx context.Context, c *client.Client) error {
		return c.ResolveApproval(ctx, id, allow, message)
	}, func(err error) {
		if err != nil {
			a.connErr.Set(friendlyError(err))
			a.loadApprovals()
		}
	})
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
	if drafts := a.drafts.GetUntracked(); drafts[id] != "" {
		next := map[string]string{}
		for k, v := range drafts {
			if k != id {
				next[k] = v
			}
		}
		a.drafts.Set(next)
		if raw, err := json.Marshal(next); err == nil {
			_ = a.store.SetItem("drafts", string(raw))
		}
	}
	if pinned := a.pinned.GetUntracked(); pinned[id] {
		next := map[string]bool{}
		for k, v := range pinned {
			if k != id {
				next[k] = v
			}
		}
		a.pinned.Set(next)
		if raw, err := json.Marshal(next); err == nil {
			_ = a.store.SetItem("pinnedThreads", string(raw))
		}
	}
	if drafts := a.attachmentDrafts.GetUntracked(); len(drafts[id]) > 0 {
		next := map[string][]proto.Attachment{}
		for k, v := range drafts {
			if k != id {
				next[k] = v
			}
		}
		a.attachmentDrafts.Set(next)
		if raw, err := json.Marshal(next); err == nil {
			_ = a.store.SetItem("attachmentDrafts", string(raw))
		}
	}
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
	switching := id != a.selected.GetUntracked()
	if switching {
		if a.draftSave != nil {
			a.draftSave()
			a.draftSave = nil
		}
		a.persistDrafts()
		a.saveAttachmentDraft()
	}
	a.selected.Set(id)
	if switching {
		a.attachments.Set(append([]proto.Attachment(nil), a.attachmentDrafts.GetUntracked()[id]...))
		a.fileIndex.Set(nil)
		a.fileIndexThread.Set("")
	}
	a.draft.Set(a.drafts.GetUntracked()[id])
	for _, t := range a.threads.GetUntracked() {
		if t.ID == id {
			a.markSeen(t)
			break
		}
	}
	a.messages.Set(nil)
	a.detail.Set(proto.ThreadDetail{})
	a.running.Set(false)
	a.gitNote.Set("")
	a.gitPR.Set("")
	a.branches.Set(nil)
	a.filesPath.Set("")
	a.filesEntries.Set(nil)
	a.fileSelected.Set("")
	a.file.Set(proto.WorkspaceFile{})
	a.pullRequest.Set(proto.PullRequest{})
	a.prDetail.Set("")
	a.graph.Set(nil)
	a.terminalSession.Set(proto.TerminalSession{})
	a.terminals.Set(nil)
	a.terminalErr.Set("")
	a.rightErr.Set("")
	if surface := a.surface.GetUntracked(); surface != "" {
		a.openSurface(surface)
	}
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

// queuedPrompt is a message waiting for the live run to end.
type queuedPrompt struct {
	Text        string
	Attachments []proto.Attachment
}

func (a *app) send() {
	id := a.selected.GetUntracked()
	text := strings.TrimSpace(a.draft.GetUntracked())
	if id == "" || text == "" || a.client() == nil {
		return
	}
	attachments := a.attachments.GetUntracked()
	a.stopDictation()
	if a.draftSave != nil {
		a.draftSave()
		a.draftSave = nil
	}
	a.setDraft("")
	a.persistDrafts()
	a.attachments.Set(nil)
	a.saveAttachmentDraft()
	a.connErr.Set("")
	// A prompt sent while the agent works waits its turn here; the next
	// run finishes before the client posts it.
	if a.running.GetUntracked() {
		a.queued[id] = append(a.queued[id], queuedPrompt{Text: text, Attachments: attachments})
		a.refreshQueue()
		return
	}
	a.sendPrompt(id, text, attachments)
}

// sendPrompt posts a prompt to a thread that has no live run.
func (a *app) sendPrompt(threadID, text string, attachments []proto.Attachment) {
	ids := make([]string, 0, len(attachments))
	for _, a := range attachments {
		ids = append(ids, a.ID)
	}
	a.running.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.SendWith(ctx, threadID, text, ids)
		return err
	}, func(err error) {
		if err != nil {
			a.running.Set(false)
			a.connErr.Set(friendlyError(err))
			// Keep the prompt rather than losing it to a failed request.
			next := queuedPrompt{Text: text, Attachments: attachments}
			a.queued[threadID] = append([]queuedPrompt{next}, a.queued[threadID]...)
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
	a.sendPrompt(threadID, next.Text, next.Attachments)
}

// refreshQueue shows the open thread's queued prompts.
func (a *app) refreshQueue() {
	queue := a.queued[a.selected.GetUntracked()]
	if len(queue) == 0 {
		a.queue.Set(nil)
		return
	}
	next := make([]queuedPrompt, len(queue))
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
	removed := queue[i]
	a.queued[id] = append(queue[:i:i], queue[i+1:]...)
	a.refreshQueue()
	a.discardAttachments(removed.Attachments)
}

// pickAttachments opens the system's file picker for the composer.
func (a *app) pickAttachments() {
	if a.client() == nil {
		return
	}
	a.connErr.Set("")
	a.cx.PickFiles(core.PickFilesOptions{Multiple: true}, func(files []core.File, err error) {
		if err != nil {
			a.connErr.Set(err.Error())
			return
		}
		a.attachFiles(files)
	})
}

// attachPaths takes files dropped on the composer, as file:// URLs or
// paths.
func (a *app) attachPaths(uris []string) {
	files := make([]core.File, 0, len(uris))
	for _, uri := range uris {
		path := uri
		if u, err := url.Parse(uri); err == nil {
			if u.Scheme == "file" {
				path = u.Path
			} else if u.Scheme != "" {
				continue
			}
		}
		if unescaped, err := url.PathUnescape(path); err == nil {
			path = unescaped
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		mimeType := mime.TypeByExtension(filepath.Ext(path))
		files = append(files, core.File{Name: filepath.Base(path), MIME: mimeType, Size: st.Size(), Path: path})
	}
	a.attachFiles(files)
}

// attachFiles uploads picked files and shows them above the composer.
func (a *app) attachFiles(files []core.File) {
	c := a.client()
	if c == nil || len(files) == 0 {
		return
	}
	if a.client() != nil {
		a.connErr.Set("")
	}
	threadID := a.selected.GetUntracked()
	for _, f := range files {
		f := f
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			src, err := f.Open()
			if err != nil {
				a.cx.Post(func() { a.connErr.Set("Could not read " + f.Name + ": " + err.Error()) })
				return
			}
			attachment, uploadErr := c.Upload(ctx, f.Name, f.MIME, src)
			_ = src.Close()
			if f.Temporary && f.Path != "" {
				_ = os.Remove(f.Path)
			}
			a.cx.Post(func() {
				if uploadErr != nil {
					a.connErr.Set(friendlyError(uploadErr))
					return
				}
				if a.selected.GetUntracked() == threadID {
					a.attachments.Set(append(a.attachments.GetUntracked(), attachment))
					a.saveAttachmentDraft()
					return
				}
				a.addAttachmentDraft(threadID, attachment)
			})
		}()
	}
}

// isExpanded reports whether a transcript detail is open, falling back to
// def until the reader opens or closes it.
func (a *app) isExpanded(key string, def bool) bool {
	if open, ok := a.expanded.Get()[key]; ok {
		return open
	}
	return def
}

// setExpanded opens or closes a transcript detail.
func (a *app) setExpanded(key string, open bool) {
	next := make(map[string]bool, len(a.expanded.GetUntracked())+1)
	for k, v := range a.expanded.GetUntracked() {
		next[k] = v
	}
	next[key] = open
	a.expanded.Set(next)
}

// toggleDictation starts listening, or stops the session that is.
func (a *app) toggleDictation() {
	if a.speech != nil && !a.speech.Ended() {
		a.speech.Stop()
		return
	}
	if !a.cx.Env().CanRecognizeSpeech() {
		a.connErr.Set("This platform has no speech recognition.")
		return
	}
	a.connErr.Set("")
	base := strings.TrimSpace(a.draft.GetUntracked())
	session, err := a.cx.StartSpeech(core.SpeechOptions{
		Interim: true,
		OnStart: func() { a.dictating.Set(true) },
		OnResult: func(text string, final bool) {
			a.draft.Set(joinSpeech(base, text))
			if final {
				a.dictating.Set(false)
			}
		},
		OnEnd: func(_ string, err error) {
			a.dictating.Set(false)
			a.speech = nil
			if err != nil {
				var speechErr core.SpeechError
				if errors.As(err, &speechErr) && speechErr != core.SpeechNoSpeech {
					a.connErr.Set("Speech: " + string(speechErr))
				}
			}
		},
	})
	if err != nil {
		a.connErr.Set(err.Error())
		return
	}
	a.speech = session
	a.dictating.Set(!session.Ended())
}

// stopDictation ends a session that is still listening.
func (a *app) stopDictation() {
	if a.speech != nil && !a.speech.Ended() {
		a.speech.Stop()
	}
	a.dictating.Set(false)
}

// joinSpeech puts what was dictated after the draft already there.
func joinSpeech(base, text string) string {
	base = strings.TrimSpace(base)
	text = strings.TrimSpace(text)
	if text == "" {
		return base
	}
	if base == "" {
		return text
	}
	return base + " " + text
}

// removeAttachment drops a file from the composer and the environment.
func (a *app) removeAttachment(id string) {
	list := a.attachments.GetUntracked()
	next := make([]proto.Attachment, 0, len(list))
	for _, attachment := range list {
		if attachment.ID != id {
			next = append(next, attachment)
		}
	}
	a.attachments.Set(next)
	a.saveAttachmentDraft()
	a.discardAttachments([]proto.Attachment{{ID: id}})
}

// discardAttachments deletes uploaded files that were never sent.
func (a *app) discardAttachments(attachments []proto.Attachment) {
	c := a.client()
	if c == nil || len(attachments) == 0 {
		return
	}
	for _, attachment := range attachments {
		id := attachment.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = c.DeleteUpload(ctx, id)
		}()
	}
}

func (a *app) stop() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	// Stop ends the turn and what was queued behind it.
	queued := a.queued[id]
	a.queued[id] = nil
	a.refreshQueue()
	for _, prompt := range queued {
		a.discardAttachments(prompt.Attachments)
	}
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
	a.showDiff.Set(true)
	a.loadDiff(id, runID)
}

// runGitAction commits, pushes, or opens a pull request for the selected
// thread's checkout, then reports what happened in the Changes dialog.
func (a *app) runGitAction(action proto.GitAction) {
	id := a.selected.GetUntracked()
	c := a.client()
	if id == "" || c == nil || a.gitBusy.GetUntracked() {
		return
	}
	in := proto.GitActionInput{Action: action, Message: strings.TrimSpace(a.gitMessage.GetUntracked())}
	a.gitBusy.Set(true)
	a.gitNote.Set("Working…")
	a.gitPR.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := c.GitAction(ctx, id, in)
		a.cx.Post(func() {
			a.gitBusy.Set(false)
			if err != nil {
				a.gitNote.Set(friendlyError(err))
				return
			}
			if d := a.detail.GetUntracked(); d.Thread.ID == id {
				st := res.Status
				d.Git = &st
				a.detail.Set(d)
			}
			if res.CommitSHA != "" {
				a.gitMessage.Set("")
			}
			var parts []string
			if res.CommitSHA != "" {
				parts = append(parts, "Committed "+shortID(res.CommitSHA))
			}
			if res.NoChanges {
				parts = append(parts, "No changes to commit")
			}
			if res.Pushed {
				parts = append(parts, "Pushed "+res.Branch)
			}
			if res.PRURL != "" {
				a.gitPR.Set(res.PRURL)
				if res.PRExisted {
					parts = append(parts, "Pull request already open")
				} else {
					parts = append(parts, "Pull request created")
				}
			}
			if len(parts) == 0 {
				parts = append(parts, "Done")
			}
			a.gitNote.Set(strings.Join(parts, " · "))
			if action == proto.GitCommitPR {
				a.loadPullRequest(id)
			}
		})
	}()
}

// shortID is the human-sized prefix of an id or hash.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// setDraft stores the composer's text for the selected thread and
// remembers it for the next visit.
func (a *app) setDraft(text string) {
	a.draft.Set(text)
	if strings.Contains(text, "@") {
		a.ensureFileIndex()
	}
	// The sidebar's draft marker can wait for a pause; updating it on
	// every key would rebuild the list under the composer.
	if a.draftSave != nil {
		a.draftSave()
	}
	a.draftSave = a.cx.AfterFunc(1200*time.Millisecond, func() {
		a.persistDrafts()
		a.draftSave = nil
	})
}

// persistDrafts records the selected thread's text and writes every
// thread's draft to the app's storage.
func (a *app) persistDrafts() {
	id := a.selected.GetUntracked()
	drafts := a.drafts.GetUntracked()
	next := make(map[string]string, len(drafts)+1)
	for k, v := range drafts {
		next[k] = v
	}
	if id != "" {
		if text := a.draft.GetUntracked(); strings.TrimSpace(text) == "" {
			delete(next, id)
		} else {
			next[id] = text
		}
	}
	a.drafts.Set(next)
	if raw, err := json.Marshal(next); err == nil {
		_ = a.store.SetItem("drafts", string(raw))
	}
}

// draftFor is a thread's unsent text when it is not the open one.
func (a *app) draftFor(id string) string {
	if id == a.selected.Get() {
		return ""
	}
	return a.drafts.Get()[id]
}

// threadPinned reports whether a thread is pinned to its project's top.
func (a *app) threadPinned(id string) bool {
	return a.pinned.Get()[id]
}

// togglePin pins or unpins a thread.
func (a *app) togglePin(t proto.Thread) {
	next := make(map[string]bool, len(a.pinned.GetUntracked())+1)
	for k, v := range a.pinned.GetUntracked() {
		next[k] = v
	}
	if next[t.ID] {
		delete(next, t.ID)
	} else {
		next[t.ID] = true
	}
	a.pinned.Set(next)
	if raw, err := json.Marshal(next); err == nil {
		_ = a.store.SetItem("pinnedThreads", string(raw))
	}
}

// addAttachmentDraft stages a file for a thread that is not open, as
// when an upload finishes after the reader switched away.
func (a *app) addAttachmentDraft(threadID string, attachment proto.Attachment) {
	if threadID == "" {
		return
	}
	drafts := a.attachmentDrafts.GetUntracked()
	next := make(map[string][]proto.Attachment, len(drafts)+1)
	for key, value := range drafts {
		next[key] = value
	}
	next[threadID] = append(next[threadID], attachment)
	a.attachmentDrafts.Set(next)
	if raw, err := json.Marshal(next); err == nil {
		_ = a.store.SetItem("attachmentDrafts", string(raw))
	}
}

// saveAttachmentDraft stores the open thread's staged files.
func (a *app) saveAttachmentDraft() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	drafts := a.attachmentDrafts.GetUntracked()
	next := make(map[string][]proto.Attachment, len(drafts)+1)
	for key, value := range drafts {
		next[key] = value
	}
	if files := a.attachments.GetUntracked(); len(files) > 0 {
		next[id] = append([]proto.Attachment(nil), files...)
	} else {
		delete(next, id)
	}
	a.attachmentDrafts.Set(next)
	if raw, err := json.Marshal(next); err == nil {
		_ = a.store.SetItem("attachmentDrafts", string(raw))
	}
}

// pasteAttachments takes images and files pasted into the composer.
func (a *app) pasteAttachments(event core.PasteEvent) {
	files := make([]core.File, 0, len(event.Files))
	for _, pasted := range event.Files {
		data := append([]byte(nil), pasted.Data...)
		name := strings.TrimSpace(pasted.Name)
		if name == "" {
			name = "pasted-" + time.Now().Format("20060102-150405")
			if exts, err := mime.ExtensionsByType(pasted.MIME); err == nil && len(exts) > 0 {
				name += exts[0]
			} else if strings.HasPrefix(pasted.MIME, "image/") {
				name += ".png"
			}
		}
		mimeType := pasted.MIME
		files = append(files, core.File{Name: name, MIME: mimeType, Size: int64(len(data)),
			Opener: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }})
	}
	a.attachFiles(files)
	if len(event.URIs) > 0 {
		a.attachPaths(event.URIs)
	}
}

// ensureFileIndex loads the checkout's files for @ mentions once.
func (a *app) ensureFileIndex() {
	id := a.selected.GetUntracked()
	if id == "" || a.fileIndexThread.GetUntracked() == id || a.fileIndexBusy.GetUntracked() {
		return
	}
	c := a.client()
	if c == nil {
		return
	}
	a.fileIndexBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		entries, err := c.FilesRecursive(ctx, id)
		a.cx.Post(func() {
			a.fileIndexBusy.Set(false)
			if err != nil || a.selected.GetUntracked() != id {
				return
			}
			a.fileIndex.Set(entries)
			a.fileIndexThread.Set(id)
		})
	}()
}

// rememberThreads records the threads a snapshot first shows as seen, so
// only later completions mark one unread.
func (a *app) rememberThreads(threads []proto.Thread) {
	seen := a.seen.GetUntracked()
	next := make(map[string]time.Time, len(seen)+len(threads))
	for id, at := range seen {
		next[id] = at
	}
	for _, t := range threads {
		if _, ok := next[t.ID]; !ok {
			next[t.ID] = t.UpdatedAt
		}
	}
	a.seen.Set(next)
}

// markSeen records that the reader has the thread's latest activity.
func (a *app) markSeen(t proto.Thread) {
	seen := a.seen.GetUntracked()
	if at, ok := seen[t.ID]; ok && !t.UpdatedAt.After(at) {
		return
	}
	next := make(map[string]time.Time, len(seen)+1)
	for id, at := range seen {
		next[id] = at
	}
	next[t.ID] = t.UpdatedAt
	a.seen.Set(next)
}

// threadUnread reports whether a thread finished work after the reader
// last saw it.
func (a *app) threadUnread(t proto.Thread) bool {
	if t.ID == a.selected.Get() || t.Status == proto.ThreadRunning {
		return false
	}
	at, ok := a.seen.Get()[t.ID]
	return ok && t.UpdatedAt.After(at)
}

// projectCollapsed reports whether a project's threads are folded away.
func (a *app) projectCollapsed(id string) bool {
	return a.sidebarCollapsed.Get()[id]
}

// setProjectCollapsed folds a project's threads and remembers the choice.
func (a *app) setProjectCollapsed(id string, collapsed bool) {
	next := make(map[string]bool, len(a.sidebarCollapsed.GetUntracked())+1)
	for k, v := range a.sidebarCollapsed.GetUntracked() {
		next[k] = v
	}
	next[id] = collapsed
	a.sidebarCollapsed.Set(next)
	if raw, err := json.Marshal(next); err == nil {
		_ = a.store.SetItem("sidebarCollapsed", string(raw))
	}
}

// setRightOpen shows or hides the right panel and remembers it.
func (a *app) setRightOpen(open bool) {
	a.rightOpen.Set(open)
	value := "false"
	if open {
		value = "true"
	}
	_ = a.store.SetItem("rightOpen", value)
	if open {
		a.openSurface(a.surface.GetUntracked())
	}
}

// openTerminal starts or reattaches the thread's shell.
func (a *app) openTerminal() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	previous := a.terminalSession.GetUntracked().ID
	a.terminalBusy.Set(true)
	a.terminalErr.Set("")
	a.call(func(ctx context.Context, c *client.Client) error {
		sessions, err := c.Terminals(ctx, id)
		if err != nil {
			return err
		}
		live := false
		for _, session := range sessions {
			live = live || !session.Exited
		}
		if !live {
			session, err := c.EnsureTerminal(ctx, id)
			if err != nil {
				return err
			}
			sessions = append(sessions, session)
		}
		active := sessions[len(sessions)-1]
		for _, session := range sessions {
			if session.ID == previous && !session.Exited {
				active = session
				break
			}
		}
		a.cx.Post(func() {
			a.cx.Batch(func() {
				a.terminals.Set(sessions)
				a.terminalSession.Set(active)
				a.terminalBusy.Set(false)
			})
		})
		return nil
	}, func(err error) {
		a.terminalBusy.Set(false)
		if err != nil {
			a.terminalErr.Set(friendlyError(err))
		}
	})
}

// newTerminal opens another shell tab for the thread.
func (a *app) newTerminal() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.terminalBusy.Set(true)
	a.terminalErr.Set("")
	a.call(func(ctx context.Context, c *client.Client) error {
		session, err := c.NewTerminal(ctx, id)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			a.cx.Batch(func() {
				a.terminals.Set(append(a.terminals.GetUntracked(), session))
				a.terminalSession.Set(session)
				a.terminalBusy.Set(false)
			})
		})
		return nil
	}, func(err error) {
		a.terminalBusy.Set(false)
		if err != nil {
			a.terminalErr.Set(friendlyError(err))
		}
	})
}

// selectTerminal makes one shell tab active.
func (a *app) selectTerminal(session proto.TerminalSession) {
	a.terminalSession.Set(session)
}

// closeTerminalTab ends one shell and moves to the next tab.
func (a *app) closeTerminalTab(session proto.TerminalSession) {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.call(func(ctx context.Context, c *client.Client) error {
		_ = c.CloseTerminal(ctx, session.ID)
		a.cx.Post(func() {
			list := a.terminals.GetUntracked()
			next := make([]proto.TerminalSession, 0, len(list))
			for _, item := range list {
				if item.ID != session.ID {
					next = append(next, item)
				}
			}
			active := a.terminalSession.GetUntracked()
			if active.ID == session.ID {
				if len(next) > 0 {
					active = next[len(next)-1]
				} else {
					active = proto.TerminalSession{}
				}
			}
			a.cx.Batch(func() {
				a.terminals.Set(next)
				a.terminalSession.Set(active)
			})
		})
		return nil
	}, nil)
}

// restartTerminal closes the active shell and starts a fresh one in its
// place.
func (a *app) restartTerminal() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	old := a.terminalSession.GetUntracked()
	if old.ID == "" {
		a.newTerminal()
		return
	}
	a.terminalBusy.Set(true)
	a.terminalErr.Set("")
	a.call(func(ctx context.Context, c *client.Client) error {
		_ = c.CloseTerminal(ctx, old.ID)
		session, err := c.NewTerminal(ctx, id)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			list := a.terminals.GetUntracked()
			next := make([]proto.TerminalSession, 0, len(list))
			for _, item := range list {
				if item.ID == old.ID {
					next = append(next, session)
				} else {
					next = append(next, item)
				}
			}
			if len(next) == 0 {
				next = append(next, session)
			}
			a.cx.Batch(func() {
				a.terminals.Set(next)
				a.terminalSession.Set(session)
				a.terminalBusy.Set(false)
			})
		})
		return nil
	}, func(err error) {
		a.terminalBusy.Set(false)
		if err != nil {
			a.terminalErr.Set(friendlyError(err))
		}
	})
}

// goBrowser opens a URL in the right panel's web view.
func (a *app) goBrowser(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		a.browserNote.Set("Enter an http or https address.")
		return
	}
	a.browserNote.Set("")
	a.browserInput.Set(u.String())
	a.browserURL.Set(u.String())
}

// openSurface switches the right panel to a surface, or to the picker
// when name is empty.
func (a *app) openSurface(name string) {
	if name != "" {
		tabs := a.tabs.GetUntracked()
		found := false
		for _, tab := range tabs {
			if tab == name {
				found = true
				break
			}
		}
		if !found {
			a.tabs.Set(append(tabs, name))
		}
	}
	a.surface.Set(name)
	a.rightErr.Set("")
	switch name {
	case "files":
		a.loadFiles(a.filesPath.GetUntracked())
	case "diff":
		if id := a.selected.GetUntracked(); id != "" {
			a.loadDiff(id, "")
		}
	case "pr":
		a.loadPullRequest(a.selected.GetUntracked())
	case "graph":
		a.loadGraph(a.selected.GetUntracked())
	case "terminal":
		a.openTerminal()
	}
}

// closeSurface closes a tab and moves to the one beside it.
func (a *app) closeSurface(name string) {
	tabs := a.tabs.GetUntracked()
	next := make([]string, 0, len(tabs))
	index := -1
	for i, tab := range tabs {
		if tab == name {
			index = i
			continue
		}
		next = append(next, tab)
	}
	a.tabs.Set(next)
	if a.surface.GetUntracked() != name {
		return
	}
	if index >= 0 && index < len(next) {
		a.openSurface(next[index])
	} else if len(next) > 0 {
		a.openSurface(next[len(next)-1])
	} else {
		a.surface.Set("")
	}
}

// setRightWidth resizes and remembers the surfaces panel.
func (a *app) setRightWidth(width core.Points) {
	minWidth, maxWidth := core.Points(300), core.Points(680)
	if available := a.cx.Env().Window().Width - 520; available < maxWidth {
		maxWidth = available
	}
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	if width < minWidth {
		width = minWidth
	}
	if width > maxWidth {
		width = maxWidth
	}
	a.rightWidth.Set(width)
	_ = a.store.SetItem("rightWidth", strconv.FormatFloat(float64(width), 'f', 1, 64))
}

// loadFiles lists a directory of the selected thread's checkout.
func (a *app) loadFiles(path string) {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.rightBusy.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		entries, err := c.Files(ctx, id, path)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			a.rightBusy.Set(false)
			a.filesPath.Set(path)
			a.filesEntries.Set(entries)
			a.fileSelected.Set("")
			a.file.Set(proto.WorkspaceFile{})
			a.rightErr.Set("")
		})
		return nil
	}, func(err error) {
		a.rightBusy.Set(false)
		if err != nil {
			a.rightErr.Set(friendlyError(err))
		}
	})
}

// openFile reads one file into the right panel's viewer.
func (a *app) openFile(path string) {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.rightBusy.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		file, err := c.File(ctx, id, path)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			a.rightBusy.Set(false)
			a.fileSelected.Set(path)
			a.file.Set(file)
			a.rightErr.Set("")
		})
		return nil
	}, func(err error) {
		a.rightBusy.Set(false)
		if err != nil {
			a.rightErr.Set(friendlyError(err))
		}
	})
}

// rawFileURL is where the panel loads an image or opens one outside the
// app.
func (a *app) rawFileURL(path string) string {
	c := a.client()
	if c == nil {
		return ""
	}
	return c.RawFileURL(a.selected.GetUntracked(), path)
}

// loadGraph reads the repository's commits for the graph surface.
func (a *app) loadGraph(id string) {
	if id == "" {
		return
	}
	a.rightBusy.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		commits, err := c.Graph(ctx, id)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			a.rightBusy.Set(false)
			a.graph.Set(commits)
			a.rightErr.Set("")
		})
		return nil
	}, func(err error) {
		a.rightBusy.Set(false)
		if err != nil {
			a.rightErr.Set(friendlyError(err))
		}
	})
}

// loadPullRequest reads the branch's open pull request.
func (a *app) loadPullRequest(id string) {
	if id == "" {
		return
	}
	a.rightBusy.Set(true)
	a.call(func(ctx context.Context, c *client.Client) error {
		pr, detail, err := c.PullRequest(ctx, id)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			a.rightBusy.Set(false)
			a.pullRequest.Set(pr)
			a.prDetail.Set(detail)
		})
		return nil
	}, func(err error) {
		a.rightBusy.Set(false)
		if err != nil {
			a.rightErr.Set(friendlyError(err))
		}
	})
}

// loadBranches fills the Changes dialog's branch picker.
func (a *app) loadBranches(threadID string) {
	a.call(func(ctx context.Context, c *client.Client) error {
		branches, err := c.Branches(ctx, threadID)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			if a.selected.GetUntracked() == threadID {
				a.branches.Set(branches)
			}
		})
		return nil
	}, func(err error) {
		if err != nil {
			a.gitNote.Set(friendlyError(err))
		}
	})
}

// switchBranch checks out another branch in the thread's checkout.
func (a *app) switchBranch(name string) {
	id := a.selected.GetUntracked()
	if id == "" || a.gitBusy.GetUntracked() {
		return
	}
	a.gitBusy.Set(true)
	a.gitNote.Set("Switching branch…")
	a.call(func(ctx context.Context, c *client.Client) error {
		branches, err := c.SwitchBranch(ctx, id, name)
		if err != nil {
			return err
		}
		a.cx.Post(func() {
			a.gitBusy.Set(false)
			a.branches.Set(branches)
			a.gitNote.Set("On " + name)
			a.loadDetail(id)
		})
		return nil
	}, func(err error) {
		if err != nil {
			a.gitBusy.Set(false)
			a.gitNote.Set(friendlyError(err))
		}
	})
}

func (a *app) openDiff() {
	id := a.selected.GetUntracked()
	if id == "" {
		return
	}
	a.showDiff.Set(true)
	a.loadDiff(id, "")
}

func (a *app) loadDiff(threadID, runID string) {
	a.diffText.Set("Loading diff…")
	a.loadBranches(threadID)
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
