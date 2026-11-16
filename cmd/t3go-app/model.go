package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/sdk/store"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/proto"
)

// app is the Asuka client's state. The transport and model survive view
// rebuilds; components only read signals and call these methods.
type app struct {
	lifetime context.Context
	cancel   context.CancelFunc

	mu       sync.Mutex
	cli      *client.Client
	stream   context.CancelFunc
	eventSeq int64
	attempt  int
	routeMu  sync.Mutex

	routeRecovery routeRecovery
	routeMonitor  sync.Once

	profiles      []client.EnvironmentProfile
	activeID      string
	defaultID     string
	pendingThread string
	envSnapshots  map[string]environmentSnapshot

	// connection
	connected  *signal.Signal[bool]
	connecting *signal.Signal[bool]
	offline    *signal.Signal[bool]
	connErr    *signal.Signal[string]
	notice     *signal.Signal[string]
	serverURL  *signal.Signal[string]
	token      *signal.Signal[string]
	formURL    *signal.Signal[string]
	formToken  *signal.Signal[string]
	envName    *signal.Signal[string]
	envHost    *signal.Signal[string]
	profilesV  *signal.Signal[[]client.EnvironmentProfile]
	activeV    *signal.Signal[string]
	envSnapV   *signal.Signal[[]environmentSnapshot]

	// environment
	projects     *signal.Signal[[]proto.Project]
	threads      *signal.Signal[[]proto.Thread]
	providers    *signal.Signal[[]proto.Provider]
	models       *signal.Signal[[]proto.Model]
	customModels *signal.Signal[map[string][]proto.Model]
	settings     *signal.Signal[proto.Settings]
	capabilities map[string]bool
	showArchived *signal.Signal[bool]
	showSettled  *signal.Signal[bool]
	pinned       *signal.Signal[map[string]bool]
	seen         *signal.Signal[map[string]time.Time]

	// open thread
	selected  *signal.Signal[string]
	detail    *signal.Signal[proto.ThreadDetail]
	messages  *signal.Signal[[]proto.Message]
	runOpen   *signal.Signal[map[string]bool]
	running   *signal.Signal[bool]
	draft     *signal.Signal[string]
	drafts    map[string]string
	queue     *signal.Signal[[]queuedPrompt]
	approvals *signal.Signal[[]proto.ApprovalRequest]
	queues    map[string][]queuedPrompt
	sending   map[string]bool

	// attachments
	attachments      *signal.Signal[[]proto.Attachment]
	attachmentDrafts map[string][]proto.Attachment
	attachmentBusy   *signal.Signal[bool]
	attachmentErr    *signal.Signal[string]

	// dialogs
	showConnect       *signal.Signal[bool]
	showConnections   *signal.Signal[bool]
	showNewThread     *signal.Signal[bool]
	showNewProject    *signal.Signal[bool]
	showSettings      *signal.Signal[bool]
	showCustomModel   *signal.Signal[bool]
	showSearch        *signal.Signal[bool]
	showPalette       *signal.Signal[bool]
	newThreadProject  *signal.Signal[int]
	newThreadTitle    *signal.Signal[string]
	newThreadPrompt   *signal.Signal[string]
	newThreadModel    *signal.Signal[int]
	newThreadPerm     *signal.Signal[int]
	newThreadWorktree *signal.Signal[bool]
	newThreadBranch   *signal.Signal[string]
	newThreadBase     *signal.Signal[string]
	newProjectName    *signal.Signal[string]
	newProjectPath    *signal.Signal[string]
	showDirPicker     *signal.Signal[bool]
	dirs              *signal.Signal[proto.HostDirs]
	dirsBusy          *signal.Signal[bool]
	dirsErr           *signal.Signal[string]
	newDirName        *signal.Signal[string]

	// settings and search
	settingsSection  *signal.Signal[int]
	settingsDraft    *signal.Signal[proto.Settings]
	settingsTitle    *signal.Signal[string]
	settingsAccent   *signal.Signal[string]
	settingsProvider *signal.Signal[int]
	settingsModel    *signal.Signal[int]
	settingsPerm     *signal.Signal[int]
	settingsTheme    *signal.Signal[int]
	settingsSave     *signal.Signal[bool]
	settingsErr      *signal.Signal[string]
	customModelFrom  *signal.Signal[string]
	customModelID    *signal.Signal[string]
	customModelName  *signal.Signal[string]
	customModelErr   *signal.Signal[string]
	providersBusy    *signal.Signal[bool]
	providersErr     *signal.Signal[string]
	searchQuery      *signal.Signal[string]
	searchHits       *signal.Signal[[]proto.MessageHit]
	searchBusy       *signal.Signal[bool]
	searchErr        *signal.Signal[string]
	paletteQuery     *signal.Signal[string]
	fileIndex        *signal.Signal[[]proto.WorkspaceEntry]
	agentCommands    *signal.Signal[[]proto.AgentCommand]
	composerBusy     *signal.Signal[bool]
	composerErr      *signal.Signal[string]
	composerThread   string
	usageReport      *signal.Signal[proto.UsageReport]
	usageDays        *signal.Signal[int]
	usageBusy        *signal.Signal[bool]
	usageErr         *signal.Signal[string]
	usageEnv         *signal.Signal[string]
	usageSeq         int
	showPRInbox      *signal.Signal[bool]
	prInboxEnv       *signal.Signal[string]
	prInboxState     *signal.Signal[string]
	prInboxRows      *signal.Signal[[]prInboxRow]
	prInboxBusy      *signal.Signal[bool]
	prInboxErr       *signal.Signal[string]
	prInboxSeq       int

	// right-hand surfaces
	rightOpen     *signal.Signal[bool]
	rightTab      *signal.Signal[int]
	rightBusy     *signal.Signal[bool]
	rightErr      *signal.Signal[string]
	filesPath     *signal.Signal[string]
	filesEntries  *signal.Signal[[]proto.WorkspaceEntry]
	fileSelected  *signal.Signal[string]
	fileView      *signal.Signal[proto.WorkspaceFile]
	diffText      *signal.Signal[string]
	workingDiff   *signal.Signal[proto.WorkingDiff]
	graph         *signal.Signal[[]proto.GraphCommit]
	pullRequests  *signal.Signal[[]proto.PullRequest]
	prDetail      *signal.Signal[string]
	prView        *signal.Signal[proto.PullRequestDetail]
	branches      *signal.Signal[[]proto.Branch]
	gitMessage    *signal.Signal[string]
	gitBase       *signal.Signal[string]
	gitBusy       *signal.Signal[bool]
	gitNote       *signal.Signal[string]
	prComment     *signal.Signal[string]
	browserURL    *signal.Signal[string]
	browserInput  *signal.Signal[string]
	previewList   *signal.Signal[[]proto.PreviewServer]
	previewDetail *signal.Signal[string]
	devices       *signal.Signal[[]proto.Device]
	deviceDetail  *signal.Signal[string]
	deviceID      *signal.Signal[string]
	deviceShot    *signal.Signal[string]
	terminals     *signal.Signal[[]proto.TerminalSession]
	terminalID    *signal.Signal[string]
	sideChat      *signal.Signal[proto.Thread]
	sideMessages  *signal.Signal[[]proto.Message]
	sideDraft     *signal.Signal[string]
	sideBusy      *signal.Signal[bool]
}

type queuedPrompt struct {
	Key           string
	ThreadID      string
	Text          string
	AttachmentIDs []string
	RequestID     string
	Uncertain     bool
}

func newApp() *app {
	ctx, cancel := context.WithCancel(context.Background())
	a := &app{
		lifetime: ctx, cancel: cancel,
		connected:         signal.New(false),
		connecting:        signal.New(false),
		offline:           signal.New(false),
		connErr:           signal.New(""),
		notice:            signal.New(""),
		serverURL:         signal.New(""),
		token:             signal.New(""),
		formURL:           signal.New(""),
		formToken:         signal.New(""),
		envName:           signal.New("T3Go"),
		envHost:           signal.New(""),
		profilesV:         signal.NewFunc([]client.EnvironmentProfile(nil), nil),
		activeV:           signal.New(""),
		envSnapV:          signal.NewFunc([]environmentSnapshot(nil), nil),
		projects:          signal.NewFunc([]proto.Project(nil), nil),
		threads:           signal.NewFunc([]proto.Thread(nil), nil),
		providers:         signal.NewFunc([]proto.Provider(nil), nil),
		models:            signal.NewFunc([]proto.Model(nil), nil),
		customModels:      signal.NewFunc(map[string][]proto.Model{}, nil),
		settings:          signal.New(proto.Settings{}),
		showArchived:      signal.New(false),
		showSettled:       signal.New(true),
		pinned:            signal.NewFunc(storedValue("pinnedThreads", map[string]bool{}), nil),
		seen:              signal.NewFunc(storedValue("threadSeen", map[string]time.Time{}), nil),
		selected:          signal.New(""),
		detail:            signal.NewFunc(proto.ThreadDetail{}, nil),
		messages:          signal.NewFunc([]proto.Message(nil), nil),
		runOpen:           signal.NewFunc(map[string]bool{}, nil),
		running:           signal.New(false),
		draft:             signal.New(""),
		queue:             signal.NewFunc([]queuedPrompt(nil), nil),
		approvals:         signal.NewFunc([]proto.ApprovalRequest(nil), nil),
		attachments:       signal.NewFunc([]proto.Attachment(nil), nil),
		attachmentBusy:    signal.New(false),
		attachmentErr:     signal.New(""),
		showConnect:       signal.New(false),
		showConnections:   signal.New(false),
		showNewThread:     signal.New(false),
		showNewProject:    signal.New(false),
		showSettings:      signal.New(false),
		showCustomModel:   signal.New(false),
		showSearch:        signal.New(false),
		showPalette:       signal.New(false),
		newThreadProject:  signal.New(0),
		newThreadTitle:    signal.New(""),
		newThreadPrompt:   signal.New(""),
		newThreadModel:    signal.New(0),
		newThreadPerm:     signal.New(3),
		newThreadWorktree: signal.New(false),
		newThreadBranch:   signal.New(""),
		newThreadBase:     signal.New(""),
		newProjectName:    signal.New(""),
		newProjectPath:    signal.New(""),
		showDirPicker:     signal.New(false),
		dirs:              signal.NewFunc(proto.HostDirs{}, nil),
		dirsBusy:          signal.New(false),
		dirsErr:           signal.New(""),
		newDirName:        signal.New(""),
		settingsSection:   signal.New(0),
		settingsDraft:     signal.NewFunc(proto.Settings{}, nil),
		settingsTitle:     signal.New(""),
		settingsAccent:    signal.New(""),
		settingsProvider:  signal.New(0),
		settingsModel:     signal.New(0),
		settingsPerm:      signal.New(3),
		settingsTheme:     signal.New(0),
		settingsSave:      signal.New(false),
		settingsErr:       signal.New(""),
		customModelFrom:   signal.New(""),
		customModelID:     signal.New(""),
		customModelName:   signal.New(""),
		customModelErr:    signal.New(""),
		providersBusy:     signal.New(false),
		providersErr:      signal.New(""),
		searchQuery:       signal.New(""),
		searchHits:        signal.NewFunc([]proto.MessageHit(nil), nil),
		searchBusy:        signal.New(false),
		searchErr:         signal.New(""),
		paletteQuery:      signal.New(""),
		fileIndex:         signal.NewFunc([]proto.WorkspaceEntry(nil), nil),
		agentCommands:     signal.NewFunc([]proto.AgentCommand(nil), nil),
		composerBusy:      signal.New(false),
		composerErr:       signal.New(""),
		usageReport:       signal.NewFunc(proto.UsageReport{}, nil),
		usageDays:         signal.New(7),
		usageBusy:         signal.New(false),
		usageErr:          signal.New(""),
		usageEnv:          signal.New(""),
		showPRInbox:       signal.New(false),
		prInboxEnv:        signal.New(""),
		prInboxState:      signal.New("OPEN"),
		prInboxRows:       signal.NewFunc([]prInboxRow(nil), nil),
		prInboxBusy:       signal.New(false),
		prInboxErr:        signal.New(""),
		rightOpen:         signal.New(false),
		rightTab:          signal.New(0),
		rightBusy:         signal.New(false),
		rightErr:          signal.New(""),
		filesPath:         signal.New(""),
		filesEntries:      signal.NewFunc([]proto.WorkspaceEntry(nil), nil),
		fileSelected:      signal.New(""),
		fileView:          signal.NewFunc(proto.WorkspaceFile{}, nil),
		diffText:          signal.New(""),
		workingDiff:       signal.NewFunc(proto.WorkingDiff{}, nil),
		graph:             signal.NewFunc([]proto.GraphCommit(nil), nil),
		pullRequests:      signal.NewFunc([]proto.PullRequest(nil), nil),
		prDetail:          signal.New(""),
		prView:            signal.NewFunc(proto.PullRequestDetail{}, nil),
		branches:          signal.NewFunc([]proto.Branch(nil), nil),
		gitMessage:        signal.New(""),
		gitBase:           signal.New(""),
		gitBusy:           signal.New(false),
		gitNote:           signal.New(""),
		prComment:         signal.New(""),
		browserURL:        signal.New(""),
		browserInput:      signal.New(""),
		previewList:       signal.NewFunc([]proto.PreviewServer(nil), nil),
		previewDetail:     signal.New(""),
		devices:           signal.NewFunc([]proto.Device(nil), nil),
		deviceDetail:      signal.New(""),
		deviceID:          signal.New(""),
		deviceShot:        signal.New(""),
		terminals:         signal.NewFunc([]proto.TerminalSession(nil), nil),
		terminalID:        signal.New(""),
		sideChat:          signal.NewFunc(proto.Thread{}, nil),
		sideMessages:      signal.NewFunc([]proto.Message(nil), nil),
		sideDraft:         signal.New(""),
		sideBusy:          signal.New(false),
		drafts:            storedValue("drafts", map[string]string{}),
		queues:            map[string][]queuedPrompt{},
		sending:           map[string]bool{},
		attachmentDrafts:  map[string][]proto.Attachment{},
		envSnapshots:      map[string]environmentSnapshot{},
	}
	saved := loadEnvironments()
	a.profiles = saved.Profiles
	a.activeID = saved.Active
	a.defaultID = saved.Default
	if a.defaultID == "" {
		a.defaultID = a.activeID
	}
	a.profilesV.Set(saved.Profiles)
	a.activeV.Set(saved.Active)
	a.customModels.Set(loadCustomModels(saved.Active))
	a.prepareConnection()
	a.refreshEnvironmentSnapshots()
	return a
}

// prepareConnection applies the dev environment variables, the web's own
// origin and the login fragment before the first frame.
func (a *app) prepareConnection() {
	url, token := strings.TrimSpace(getenv("T3GO_SERVER")), strings.TrimSpace(getenv("T3GO_TOKEN"))
	if origin := pageOrigin(); origin != "" {
		if fragURL, fragToken := parseLoginFragment(pageFragment()); fragToken != "" {
			token = fragToken
			if fragURL != "" {
				url = fragURL
			}
		}
		if url == "" {
			url = origin
		}
	}
	if url != "" {
		if p, err := client.PairingProfile(url, token); err == nil {
			if p.ID == "" {
				p.ID = client.LegacyEnvironmentID(p.Routes[len(p.Routes)-1].URL)
			}
			p.Name = environmentHost(p)
			a.serverURL.Set(url)
			a.token.Set(token)
			a.formURL.Set(url)
			a.formToken.Set(token)
			a.connectProfile(p, true)
			return
		}
	}
	if a.activeID != "" {
		if p := a.findProfile(a.activeID); p.ID != "" {
			a.serverURL.Set(firstRoute(p))
			a.token.Set(p.Token)
			a.formURL.Set(firstRoute(p))
			a.formToken.Set(p.Token)
			a.connectProfile(p, true)
			return
		}
	}
	if a.activeID == "" && len(a.profiles) > 0 {
		p := a.findProfile(a.defaultID)
		if p.ID == "" {
			p = a.profiles[0]
		}
		a.activeID = p.ID
		a.activeV.Set(p.ID)
		a.serverURL.Set(firstRoute(p))
		a.token.Set(p.Token)
		a.formURL.Set(firstRoute(p))
		a.formToken.Set(p.Token)
		a.connectProfile(p, true)
		return
	}
	a.serverURL.Set(url)
	a.token.Set(token)
	a.formURL.Set(url)
	a.formToken.Set(token)
}

func getenv(key string) string {
	return os.Getenv(key)
}

func firstRoute(p client.EnvironmentProfile) string {
	for _, r := range p.Routes {
		if r.Kind == "direct" {
			return r.URL
		}
	}
	if len(p.Routes) > 0 {
		return p.Routes[0].URL
	}
	return ""
}

func (a *app) findProfile(id string) client.EnvironmentProfile {
	for _, p := range a.profiles {
		if p.ID == id {
			return p
		}
	}
	return client.EnvironmentProfile{}
}

// connect parses what the connect form holds and connects to it.
func (a *app) connect(address, token string) {
	p, err := client.PairingProfile(address, token)
	if err != nil {
		a.connErr.Set(err.Error())
		return
	}
	if p.ID == "" {
		p.ID = client.LegacyEnvironmentID(p.Routes[len(p.Routes)-1].URL)
	}
	p.Name = environmentHost(p)
	a.connectProfile(p, true)
}

// handleIncomingURL connects from a t3go://pair link opened by the OS.
func (a *app) handleIncomingURL(raw string) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "t3go://pair") {
		return
	}
	p, err := client.PairingProfile(raw, "")
	if err != nil {
		a.connErr.Set(friendlyError(err))
		a.showConnect.Set(true)
		return
	}
	if p.ID == "" {
		p.ID = client.LegacyEnvironmentID(p.Routes[len(p.Routes)-1].URL)
	}
	p.Name = environmentHost(p)
	a.serverURL.Set(firstRoute(p))
	a.token.Set(p.Token)
	a.formURL.Set(firstRoute(p))
	a.formToken.Set(p.Token)
	a.showConnect.Set(false)
	a.connectProfile(p, true)
}

func (a *app) connectProfile(profile client.EnvironmentProfile, save bool) {
	a.attempt++
	attempt := a.attempt
	a.connecting.Set(true)
	a.connErr.Set("")
	a.notice.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 15*time.Second)
		defer cancel()
		c, st, err := client.ConnectEnvironment(ctx, profile)
		ui.Post(func() {
			if attempt != a.attempt {
				return
			}
			a.connecting.Set(false)
			if err != nil {
				a.connErr.Set(friendlyError(err))
				return
			}
			a.finishConnection(profile, c, st, save)
		})
	}()
}

func (a *app) finishConnection(profile client.EnvironmentProfile, c *client.Client, st proto.State, save bool) {
	oldID := a.activeID
	profile.Token = c.Token
	if st.EnvironmentID != "" {
		profile.ID = st.EnvironmentID
	}
	if profile.ID == "" {
		profile.ID = client.LegacyEnvironmentID(c.BaseURL)
	}
	if st.Hostname != "" {
		profile.Hostname = st.Hostname
	}
	if name := strings.TrimSpace(st.Name); name != "" && !strings.EqualFold(name, "T3Go") {
		profile.Name = name
	}
	if profile.Name == "" || strings.HasPrefix(profile.Name, "http") {
		profile.Name = environmentHost(profile)
	}
	a.mu.Lock()
	old := a.cli
	a.cli = c
	a.mu.Unlock()
	if old != c {
		a.stopStream()
	}
	a.activeID = profile.ID
	a.profiles = upsertProfile(a.profiles, profile)
	if a.defaultID == "" {
		a.defaultID = profile.ID
	}
	a.profilesV.Set(a.profiles)
	a.activeV.Set(a.activeID)
	a.customModels.Set(loadCustomModels(a.activeID))
	a.serverURL.Set(c.BaseURL)
	a.token.Set(c.Token)
	a.formURL.Set(c.BaseURL)
	a.formToken.Set(c.Token)
	a.envName.Set(environmentBaseName(profile))
	a.envHost.Set(environmentHost(profile))
	if save {
		a.saveProfiles()
	}
	a.refreshEnvironmentSnapshots()
	a.applyState(st)
	a.connected.Set(true)
	a.connecting.Set(false)
	a.offline.Set(false)
	a.connErr.Set("")
	a.showConnect.Set(false)
	a.eventSeq = st.Seq
	a.startStream(c, st.Seq)
	a.startRouteMonitor()
	a.routeRecovery.enter(time.Now(), routeKind(profile.Routes, c.BaseURL))
	a.syncActiveSnapshot()
	if oldID != "" && oldID != a.activeID {
		a.queues = map[string][]queuedPrompt{}
		a.attachmentDrafts = map[string][]proto.Attachment{}
		a.clearThread()
	}
	if pending := a.pendingThread; pending != "" {
		a.pendingThread = ""
		if slices.ContainsFunc(st.Threads, func(t proto.Thread) bool { return t.ID == pending }) {
			a.selectThread(pending)
		} else {
			a.notice.Set("That thread is no longer available in the environment.")
		}
	}
	if a.selected.Peek() == "" {
		if selected := storedValue("selectedThread", ""); selected != "" {
			for _, t := range st.Threads {
				if t.ID == selected {
					a.selectThread(selected)
					break
				}
			}
		}
	}
	if selected := a.selected.Peek(); selected != "" {
		a.loadApprovals(selected)
	}
}

func upsertProfile(list []client.EnvironmentProfile, p client.EnvironmentProfile) []client.EnvironmentProfile {
	for i := range list {
		if list[i].ID == p.ID {
			next := slices.Clone(list)
			next[i] = p
			return next
		}
	}
	return append(slices.Clone(list), p)
}

func (a *app) saveProfiles() {
	saveEnvironments(savedEnvironments{Version: 1, Active: a.activeID, Default: a.defaultID, Profiles: a.profiles})
}

func (a *app) switchProfile(id string) {
	p := a.findProfile(id)
	if p.ID == "" || id == a.activeID && a.connected.Peek() {
		return
	}
	a.activeID = id
	a.activeV.Set(id)
	a.saveProfiles()
	a.connectProfile(p, true)
}

func (a *app) forgetProfile(id string) {
	var next []client.EnvironmentProfile
	for _, p := range a.profiles {
		if p.ID != id {
			next = append(next, p)
		}
	}
	a.profiles = next
	a.profilesV.Set(next)
	if a.activeID == id {
		a.customModels.Set(map[string][]proto.Model{})
	}
	if a.defaultID == id {
		a.defaultID = ""
		if len(next) > 0 {
			a.defaultID = next[0].ID
		}
	}
	if a.activeID == id {
		a.activeID = ""
		a.activeV.Set("")
		a.stopStream()
		a.mu.Lock()
		a.cli = nil
		a.mu.Unlock()
		a.connected.Set(false)
		a.offline.Set(false)
		a.clearThread()
		a.serverURL.Set("")
		a.token.Set("")
	}
	a.saveProfiles()
	a.emitEnvironmentSnapshots()
}

func (a *app) client() *client.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cli
}

func (a *app) stopStream() {
	a.mu.Lock()
	if a.stream != nil {
		a.stream()
		a.stream = nil
	}
	a.mu.Unlock()
}

func (a *app) startStream(c *client.Client, after int64) {
	if c == nil {
		return
	}
	a.mu.Lock()
	if a.stream != nil {
		a.stream()
	}
	ctx, cancel := context.WithCancel(a.lifetime)
	a.stream = cancel
	a.mu.Unlock()
	go c.StreamWithReady(ctx, after, func() {
		ui.Post(func() {
			if ctx.Err() == nil && a.client() == c {
				a.offline.Set(false)
			}
		})
	}, func(e proto.Event) {
		ui.Post(func() {
			if ctx.Err() != nil || a.client() != c {
				return
			}
			if e.Seq > 0 && e.Seq <= a.eventSeq {
				return
			}
			a.offline.Set(false)
			a.applyEvent(e)
			if e.Seq > a.eventSeq {
				a.eventSeq = e.Seq
			}
		})
	}, func(err error) {
		ui.Post(func() {
			if ctx.Err() != nil || a.client() != c {
				return
			}
			if errors.Is(err, client.ErrResyncRequired) {
				a.resync(c)
				return
			}
			if client.IsAuthorizationError(err) {
				a.connected.Set(false)
				a.connErr.Set(friendlyError(err))
				return
			}
			a.offline.Set(true)
			a.tryAlternateRoute(c, false)
		})
	})
}

// resync reloads the snapshot when too many events were missed.
func (a *app) resync(c *client.Client) {
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 15*time.Second)
		defer cancel()
		st, err := c.State(ctx)
		if err != nil {
			ui.Post(func() {
				if a.client() == c {
					a.offline.Set(true)
				}
			})
			return
		}
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.applyState(st)
			a.eventSeq = st.Seq
			a.startStream(c, st.Seq)
			if thread := a.selected.Peek(); thread != "" {
				a.loadDetail(thread)
			}
		})
	}()
}

func (a *app) applyState(st proto.State) {
	a.capabilities = map[string]bool{}
	for _, c := range st.Capabilities {
		a.capabilities[c] = true
	}
	a.projects.Set(st.Projects)
	a.threads.Set(st.Threads)
	a.providers.Set(st.Providers)
	a.models.Set(st.Models)
	a.settings.Set(st.Settings)
	a.applyAppearance(st.Settings)
	if name := strings.TrimSpace(st.Name); name != "" && !strings.EqualFold(name, "T3Go") {
		a.envName.Set(name)
	}
	if st.Hostname != "" {
		a.envHost.Set(st.Hostname)
	}
	if selected := a.selected.Peek(); selected != "" && !slices.ContainsFunc(st.Threads, func(t proto.Thread) bool { return t.ID == selected }) {
		a.clearThread()
	}
	a.syncActiveSnapshot()
}

func (a *app) reload() {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 10*time.Second)
		defer cancel()
		st, err := c.State(ctx)
		if err != nil {
			return
		}
		ui.Post(func() {
			if a.client() == c {
				a.applyState(st)
			}
		})
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
		if a.selected.Peek() == t.ID {
			d := a.detail.Peek()
			d.Thread = t
			a.detail.Set(d)
			running := t.Status == proto.ThreadRunning
			a.running.Set(running)
			if !running {
				a.pump()
			}
		}
	case proto.EventThreadDeleted:
		a.removeThread(e.ThreadID)
		a.dropQueue(e.ThreadID)
		if a.selected.Peek() == e.ThreadID {
			a.clearThread()
		}
	case proto.EventMessage:
		m, ok := decodeData[proto.Message](e.Data)
		if ok {
			if m.ThreadID == a.selected.Peek() {
				a.applyMessage(m)
			}
			a.applySideMessage(m)
		}
	case proto.EventMessageDelta:
		d, ok := decodeData[proto.MessageDelta](e.Data)
		if ok {
			if e.ThreadID == a.selected.Peek() {
				a.applyDelta(d)
			}
			a.applySideDelta(d)
		}
	case proto.EventRunStarted:
		if e.ThreadID == a.selected.Peek() {
			a.running.Set(true)
		}
		if e.ThreadID == a.sideChat.Peek().ID {
			a.sideBusy.Set(true)
		}
	case proto.EventRunFinished:
		if e.ThreadID == a.sideChat.Peek().ID {
			a.sideBusy.Set(false)
		}
		if e.ThreadID == a.selected.Peek() {
			a.running.Set(false)
			if run, ok := decodeData[proto.Run](e.Data); ok {
				d := a.detail.Peek()
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
			a.pump()
		}
	case proto.EventApproval:
		if request, ok := decodeData[proto.ApprovalRequest](e.Data); ok && request.ID != "" && request.ThreadID == a.selected.Peek() {
			a.addApproval(request)
		}
	case proto.EventApprovalResolved:
		if e.ThreadID == a.selected.Peek() {
			resolved, ok := decodeData[map[string]any](e.Data)
			if !ok {
				return
			}
			if id, _ := resolved["id"].(string); id != "" {
				a.removeApproval(id)
			}
		}
	case proto.EventSettings:
		if settings, ok := decodeData[proto.Settings](e.Data); ok {
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
	}
}

func (a *app) upsertThread(t proto.Thread) {
	list := a.threads.Peek()
	next := slices.Clone(list)
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
	list := a.threads.Peek()
	next := list[:0]
	for _, t := range list {
		if t.ID != id {
			next = append(next, t)
		}
	}
	a.threads.Set(slices.Clone(next))
}

func (a *app) upsertProject(p proto.Project) {
	list := a.projects.Peek()
	next := slices.Clone(list)
	for i := range next {
		if next[i].ID == p.ID {
			next[i] = p
			a.projects.Set(next)
			return
		}
	}
	a.projects.Set(append(next, p))
}

func (a *app) removeProject(id string) {
	list := a.projects.Peek()
	next := list[:0]
	for _, p := range list {
		if p.ID != id {
			next = append(next, p)
		}
	}
	a.projects.Set(slices.Clone(next))
}

func (a *app) applyMessage(m proto.Message) {
	list := a.messages.Peek()
	next := slices.Clone(list)
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
	list := a.messages.Peek()
	next := slices.Clone(list)
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

// selectThread saves the draft of the thread being left, loads the new
// transcript and remembers the choice.
func (a *app) selectThread(id string) {
	old := a.selected.Peek()
	if old != "" && old != id {
		a.drafts[old] = a.draft.Peek()
		a.attachmentDrafts[old] = slices.Clone(a.attachments.Peek())
		storeJSON("drafts", a.drafts)
	}
	a.selected.Set(id)
	_ = store.Set("selectedThread", id)
	a.draft.Set(a.drafts[id])
	a.attachments.Set(slices.Clone(a.attachmentDrafts[id]))
	a.detail.Set(proto.ThreadDetail{})
	a.messages.Set(nil)
	if t := a.threadByID(id); t.ID != "" {
		a.running.Set(t.Status == proto.ThreadRunning)
	} else {
		a.running.Set(false)
	}
	a.markThreadRead(id)
	a.queue.Set(a.queueForThread(id))
	a.approvals.Set(nil)
	if id != "" {
		a.loadDetail(id)
		a.loadApprovals(id)
		a.loadComposerContext(id)
	}
}

func (a *app) clearThread() {
	a.selected.Set("")
	a.detail.Set(proto.ThreadDetail{})
	a.messages.Set(nil)
	a.runOpen.Set(map[string]bool{})
	a.running.Set(false)
	a.queue.Set(nil)
	a.approvals.Set(nil)
	a.draft.Set("")
	a.attachments.Set(nil)
	a.fileIndex.Set(nil)
	a.agentCommands.Set(nil)
	a.composerThread = ""
}

func (a *app) threadByID(id string) proto.Thread {
	for _, t := range a.threads.Peek() {
		if t.ID == id {
			return t
		}
	}
	return proto.Thread{}
}

func (a *app) queueForThread(threadID string) []queuedPrompt {
	if threadID == "" {
		return nil
	}
	return slices.Clone(a.queues[threadID])
}

func (a *app) setQueue(threadID string, queue []queuedPrompt) {
	if threadID == "" {
		return
	}
	if len(queue) == 0 {
		delete(a.queues, threadID)
	} else {
		a.queues[threadID] = slices.Clone(queue)
	}
	if a.selected.Peek() == threadID {
		a.queue.Set(slices.Clone(queue))
	}
}

func (a *app) dropQueue(threadID string) {
	delete(a.queues, threadID)
	if a.selected.Peek() == threadID {
		a.queue.Set(nil)
	}
}

// closeThread returns to the thread list on a phone.
func (a *app) closeThread() {
	if id := a.selected.Peek(); id != "" {
		a.drafts[id] = a.draft.Peek()
		a.attachmentDrafts[id] = slices.Clone(a.attachments.Peek())
		storeJSON("drafts", a.drafts)
	}
	a.selected.Set("")
	_ = store.Set("selectedThread", "")
	a.detail.Set(proto.ThreadDetail{})
	a.messages.Set(nil)
	a.running.Set(false)
	a.approvals.Set(nil)
	a.queue.Set(nil)
	a.draft.Set("")
	a.attachments.Set(nil)
	a.fileIndex.Set(nil)
	a.agentCommands.Set(nil)
	a.composerThread = ""
}

func (a *app) loadDetail(id string) {
	c := a.client()
	if c == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		d, err := c.ThreadDetail(ctx, id)
		if err != nil {
			return
		}
		ui.Post(func() {
			if a.selected.Peek() != id || a.client() != c {
				return
			}
			a.detail.Set(d)
			a.messages.Set(d.Messages)
			if d.Thread.Status == proto.ThreadRunning {
				a.running.Set(true)
			}
			if d.Thread.Status != proto.ThreadRunning && !a.sending[id] && len(a.queueForThread(id)) > 0 {
				a.pump()
			}
		})
	}()
}

func (a *app) loadApprovals(threadID string) {
	c := a.client()
	if c == nil || threadID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 10*time.Second)
		defer cancel()
		approvals, err := c.Approvals(ctx, threadID)
		if err != nil {
			return
		}
		ui.Post(func() {
			if a.client() == c && a.selected.Peek() == threadID {
				a.approvals.Set(approvals)
			}
		})
	}()
}

func (a *app) addApproval(request proto.ApprovalRequest) {
	list := a.approvals.Peek()
	if slices.ContainsFunc(list, func(r proto.ApprovalRequest) bool { return r.ID == request.ID }) {
		return
	}
	a.approvals.Set(append(slices.Clone(list), request))
}

func (a *app) removeApproval(id string) {
	list := a.approvals.Peek()
	next := list[:0]
	for _, r := range list {
		if r.ID != id {
			next = append(next, r)
		}
	}
	a.approvals.Set(slices.Clone(next))
}

func (a *app) resolveApproval(id string, allow bool, message string) {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		err := c.ResolveApproval(ctx, id, allow, message)
		ui.Post(func() {
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.removeApproval(id)
		})
	}()
}

// send queues the draft. A prompt sent while a run is live waits for it
// to finish, then goes out in order.
func (a *app) send() {
	threadID := a.selected.Peek()
	text := strings.TrimSpace(a.draft.Peek())
	if threadID == "" || (text == "" && len(a.attachments.Peek()) == 0) || !a.connected.Peek() {
		return
	}
	attachments := a.attachments.Peek()
	var attachmentIDs []string
	for _, attachment := range attachments {
		attachmentIDs = append(attachmentIDs, attachment.ID)
	}
	a.draft.Set("")
	a.drafts[threadID] = ""
	delete(a.attachmentDrafts, threadID)
	a.attachments.Set(nil)
	storeJSON("drafts", a.drafts)
	queue := append(a.queueForThread(threadID), queuedPrompt{
		Key:           idNew("queue"),
		ThreadID:      threadID,
		Text:          text,
		AttachmentIDs: attachmentIDs,
	})
	a.setQueue(threadID, queue)
	a.pump()
}

func (a *app) pump() {
	if !a.connected.Peek() || a.running.Peek() {
		return
	}
	threadID := a.selected.Peek()
	if threadID == "" || a.sending[threadID] {
		return
	}
	queue := a.queueForThread(threadID)
	if len(queue) == 0 {
		return
	}
	c := a.client()
	if c == nil {
		return
	}
	prompt := queue[0]
	a.setQueue(threadID, queue[1:])
	if a.capabilities[proto.CapabilityIdempotentMessages] && prompt.RequestID == "" {
		prompt.RequestID = idNew("send")
	}
	a.sending[threadID] = true
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		_, err := c.SendWithRequest(ctx, threadID, prompt.Text, prompt.AttachmentIDs, prompt.RequestID)
		ui.Post(func() {
			delete(a.sending, threadID)
			if err != nil {
				a.setQueue(threadID, append([]queuedPrompt{prompt}, a.queueForThread(threadID)...))
				a.notice.Set(friendlyError(err))
				return
			}
			if a.selected.Peek() == threadID {
				a.running.Set(true)
			} else {
				a.pump()
			}
			a.notice.Set("")
		})
	}()
}

func (a *app) removeQueued(key string) {
	threadID := a.selected.Peek()
	queue := a.queueForThread(threadID)
	index := slices.IndexFunc(queue, func(p queuedPrompt) bool { return p.Key == key })
	if index < 0 {
		return
	}
	a.setQueue(threadID, append(slices.Clone(queue[:index]), queue[index+1:]...))
}

func (a *app) stop() {
	id := a.selected.Peek()
	c := a.client()
	if id == "" || c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		if _, err := c.Stop(ctx, id); err != nil {
			ui.Post(func() { a.notice.Set(friendlyError(err)) })
		}
	}()
}

func (a *app) createThread(in client.CreateThreadInput, prompt string) {
	c := a.client()
	if c == nil {
		return
	}
	prompt = strings.TrimSpace(prompt)
	if in.Title == "" && prompt != "" {
		in.Title = prompt
		if len(in.Title) > 72 {
			in.Title = in.Title[:72]
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		thread, err := c.CreateThread(ctx, in)
		ui.Post(func() {
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.upsertThread(thread)
			a.showNewThread.Set(false)
			a.newThreadTitle.Set("")
			a.newThreadPrompt.Set("")
			a.selectThread(thread.ID)
			if prompt != "" {
				a.draft.Set(prompt)
				a.send()
			}
		})
	}()
}

// openNewThread resets the new-thread sheet with the environment's
// defaults and the first project selected.
func (a *app) openNewThread(projectID string) {
	a.notice.Set("")
	projects := a.projects.Peek()
	index := 0
	for i, p := range projects {
		if p.ID == projectID {
			index = i
			break
		}
	}
	a.newThreadProject.Set(index)
	a.newThreadTitle.Set("")
	a.newThreadPrompt.Set("")
	a.newThreadModel.Set(a.defaultModelIndex())
	a.newThreadPerm.Set(permissionIndex(proto.PermissionMode(a.settings.Peek().DefaultPermission)))
	a.newThreadWorktree.Set(false)
	a.newThreadBranch.Set("")
	a.newThreadBase.Set("")
	a.showNewThread.Set(true)
}

// modelOptions is the provider/model pairs the new-thread sheet offers.
func (a *app) modelOptions() []threadModelOption {
	return modelOptionsOf(a.providers.Peek(), a.models.Peek(), a.customModels.Peek(), a.settings.Peek())
}

type threadModelOption struct {
	Provider string
	Model    string
	Label    string
}

func (a *app) defaultModelIndex() int {
	s := a.settings.Peek()
	options := a.modelOptions()
	for i, o := range options {
		if o.Provider == s.DefaultProvider && o.Model == s.DefaultModel {
			return i
		}
	}
	return 0
}

func permissionIndex(mode proto.PermissionMode) int {
	switch mode {
	case proto.PermissionSupervised:
		return 0
	case proto.PermissionAcceptEdits:
		return 1
	case proto.PermissionAuto:
		return 2
	}
	return 3
}

func permissionAt(index int) proto.PermissionMode {
	switch index {
	case 0:
		return proto.PermissionSupervised
	case 1:
		return proto.PermissionAcceptEdits
	case 2:
		return proto.PermissionAuto
	}
	return proto.PermissionFullAccess
}

func (a *app) createProject(name, path string) {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		project, err := c.CreateProject(ctx, name, path)
		ui.Post(func() {
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.upsertProject(project)
			a.showNewProject.Set(false)
			a.newProjectName.Set("")
			a.newProjectPath.Set("")
			a.showNewThread.Set(true)
		})
	}()
}

func (a *app) close() {
	a.stopStream()
	a.cancel()
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

func idNew(prefix string) string { return id.New(prefix) }

func modelLabel(m proto.Model) string {
	if strings.TrimSpace(m.Name) != "" {
		return m.Name
	}
	return m.ID
}

func threadProvider(t proto.Thread) string {
	if t.Provider == "" {
		return "agent"
	}
	return t.Provider
}

func (a *app) providerName(id string) string {
	for _, p := range a.providers.Peek() {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

func (a *app) statusText(t proto.Thread) string {
	switch t.Status {
	case proto.ThreadRunning:
		return "Running"
	case proto.ThreadFailed:
		return "Failed"
	}
	if t.Archived {
		return "Archived"
	}
	return fmt.Sprintf("%s · %s", a.providerName(threadProvider(t)), modelLabel(proto.Model{ID: t.Model, Name: t.Model}))
}
