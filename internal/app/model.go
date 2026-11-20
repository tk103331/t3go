package app

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

func settingsEqual(a, b proto.Settings) bool {
	return reflect.DeepEqual(a, b)
}

// Session is one environment's client: its connection, the environment's
// snapshot and the selected thread's transcript. Every field is read and
// written on the main thread; work on other goroutines reaches the state
// through the window's Update.
type Session struct {
	win        *mygo.Window
	lifetime   context.Context
	store      appStorage
	manager    *Manager
	profile    client.EnvironmentProfile
	appearance clientAppearance

	// narrow is set by the view when the window is too small for the
	// desktop layout (the compactSettings check of the old client).
	narrow bool

	queuePaused          bool
	routing              bool
	eventSeq             int64
	connectionAttempt    int
	inflight             map[string]queuedPrompt
	idempotentMessages   bool
	transportBlocked     bool
	routeRecovery        routeRecovery
	receiptChecks        map[string]bool
	showCredentials      bool
	credentialToken      string
	credentialError      string
	connectionRecovered  bool
	recoveryNoticeCancel func() bool
	appliedAccent        string

	// connection
	serverURL     string
	serverVersion string
	token         string
	connected     bool
	connecting    bool
	offline       bool
	connErr       string

	// environment snapshot
	projects  []proto.Project
	threads   []proto.Thread
	providers []proto.Provider
	models    []proto.Model
	// customModels are model ids the environment's CLIs accept but do not list.
	customModels        map[string][]proto.Model
	settings            proto.Settings
	providersBusy       bool
	providersErr        string
	providersSeq        int
	providerCatalog     []proto.ACPRegistryAgent
	providerCatalogBusy bool
	providerCatalogErr  string
	providerAddOpen     bool
	providerAddMode     string
	providerSearchQuery string
	providerLocalName   string
	providerLocalPath   string
	providerLocalArgs   string
	providerManageErr   string
	providerPrepareID   string
	providerTextDraft   map[string]string
	providerSeq         int
	accessLink          string
	accessQR            []byte
	accessErr           string
	relayAccess         proto.RelayAccessStatus
	relayBusy           bool
	relayErr            string
	relaySeq            int

	showArchived        bool
	showCustomModel     bool
	customModelProvider string
	customModelID       string
	customModelName     string
	customModelErr      string

	// side chat
	sideChat        proto.Thread
	sideMessages    []proto.Message
	sideRunning     bool
	sideDraft       string
	sideBusy        bool
	sideErr         string
	sideFor         string
	sideAttachments []proto.Attachment

	// message search
	searchHits  []proto.MessageHit
	searchBusy  bool
	searchErr   string
	searchSeq   int
	searchTimer func() bool

	// editor
	editorChoice string

	// usage
	usageReport proto.UsageReport
	usageDays   int
	usageBusy   bool
	usageErr    string
	usageSeq    int

	// local preview servers
	previewServers []proto.PreviewServer
	previewDetail  string
	previewErr     string
	previewBusy    bool

	// simulators
	devices        []proto.Device
	deviceDetail   string
	deviceSelected string
	deviceBusy     bool
	deviceErr      string
	deviceImage    []byte
	deviceLive     bool
	deviceTimer    func() bool
	showDevices    bool

	// the open thread
	selected         string
	detail           proto.ThreadDetail
	messages         []proto.Message
	running          bool
	draft            string
	queue            []queuedPrompt
	queued           map[string][]queuedPrompt
	attachments      []proto.Attachment
	attachmentDrafts map[string][]proto.Attachment
	expanded         map[string]bool
	approvals        []proto.ApprovalRequest

	// dialogs and page state
	showNewProject        bool
	showNewThread         bool
	showSettings          bool
	showPalette           bool
	settingsDraft         proto.Settings
	settingsTitle         string
	settingsSection       string
	settingsEnv           string
	settingsVisited       map[*Session]bool
	settingsSaveBusy      bool
	settingsSaveErr       string
	mobilePage            string
	mobileThreadSearch    bool
	mobileSettledClosed   bool
	mobileSettingsDetail  string
	mobileSurface         string
	mobileUsageEnv        string
	mobileUsageMetric     string
	mobileUsageDays       int
	mobilePREnv           string
	mobilePRState         string
	mobilePRRows          []mobilePullRequest
	mobilePRBusy          bool
	mobilePRErr           string
	mobilePRSeq           int
	newThreadEnv          string
	newThreadPrompt       string
	newThreadVisited      map[*Session]bool
	showDiff              bool
	mobileDiffMode        string
	mobileCommitOpen      bool
	mobileCommitNewBranch bool
	mobileCommitFiles     []string
	showRuns              bool
	pendingRestore        string
	showRestore           bool
	diffText              string
	workingDiff           string
	workingFiles          []proto.DiffFile
	diffSplit             bool
	diffDiscard           string
	editingMessage        proto.Message
	editDraft             string
	pullRequests          []proto.PullRequest
	prListDetail          string
	prView                proto.PullRequestDetail
	prViewNumber          int
	prViewBusy            bool
	prDeleteBranch        bool
	prNote                string
	showBranch            bool
	showBranches          bool
	// The conversation's menus keep their open state here, not in the
	// view tree: a message arriving or a connection changing rebuilds
	// the header, and a menu that lived in it would close under the
	// reader's finger. MyGo's PopoverMenuButton keeps its own state, so
	// these flags are only for menus the app opens itself.
	branchName      string
	promptName      string
	promptArgValues promptArgsState
	showPromptArgs  bool
	promptArgsDefs  []proto.AgentCommandArgument

	gitMessage string
	gitBusy    bool
	gitNote    string
	gitPR      string
	branches   []proto.Branch

	pendingDelete itemTarget
	pendingRename itemTarget
	renameName    string

	newProjectName   string
	newProjectPath   string
	newProjectRemote bool
	newProjectEnv    string
	projectBusy      bool

	// folders on the connected environment's machine
	showDirs   bool
	dirs       proto.HostDirs
	dirsBusy   bool
	dirsErr    string
	dirsSeq    int
	showNewDir bool
	newDirName string
	newDirBusy bool
	newDirErr  string
	newDirSeq  int

	newThreadTitle        string
	newThreadProv         string
	newThreadModel        string
	newThreadPerm         proto.PermissionMode
	newThreadWT           bool
	newThreadExistingWT   string
	newThreadBase         string
	threadBusy            bool
	newThreadProj         string
	newThreadFiles        []proto.Attachment
	newThreadBranches     []proto.Branch
	newThreadBranchesBusy bool
	newThreadBranchesErr  string
	newThreadBranchesSeq  int

	sidebarOpen      bool
	sidebarWidth     float32
	sidebarQuery     string
	sidebarCollapsed map[string]bool
	seen             map[string]time.Time
	drafts           map[string]string
	pinned           map[string]bool
	draftSave        func() bool

	fileIndex       []proto.WorkspaceEntry
	fileIndexThread string
	fileIndexBusy   bool

	agentCommands       []proto.AgentCommand
	agentCommandsThread string
	agentCommandsBusy   bool

	rightOpen    bool
	rightWidth   float32
	surface      string
	filesPath    string
	filesEntries []proto.WorkspaceEntry
	fileSelected string
	file         proto.WorkspaceFile
	rightErr     string
	rightBusy    bool
	pullRequest  proto.PullRequest
	prDetail     string
	graph        []proto.GraphCommit
	browserURL   string
	browserInput string
	browserNote  string
	browserBack  bool
	browserNext  bool
	browserWeb   *mygo.WebView

	terminalSession proto.TerminalSession
	terminals       []proto.TerminalSession
	terminalBusy    bool
	terminalErr     string
	terminalHub     *terminalHub

	// draftSave etc. keep values the view asks for between frames.
	lastThreadProject string

	mu           sync.Mutex
	cli          *client.Client
	streamCancel context.CancelFunc

	// View state the immediate-mode widgets keep between frames: the
	// scroll places of the lists and panels.
	sidebarList    ui.ListState
	sidebarRows    []inboxRow
	transcriptList ui.ListState
	tabs           []string
	paletteList    ui.ListState
	paletteRow     int

	// md is how the transcript renders a reply: the Markdown options with
	// the theme the conversation colors itself in.
	md markdown.Options

	// Dialog state the views own: which confirmations are open, what the
	// palette searches, and the file the diff dialog shows.
	renameOpen     bool
	deleteOpen     bool
	editOpen       bool
	paletteQuery   string
	diffFileIndex  int
	editMessageKey string
	deviceShot     string
	// deviceBitmapCache is the decoded screenshot, made once per shot.
	deviceBitmapCache *ui.Bitmap
	deviceBitmapKey   string
}

func newSession(win *mygo.Window, shared appStorage, manager *Manager, profile client.EnvironmentProfile) *Session {
	var storage appStorage = shared
	if manager != nil {
		if manager.migrating && !manager.adding && len(manager.profiles) == 0 {
			profile.Legacy = true
		}
		id := profile.ID
		if id == "" {
			id = "pending"
		}
		storage = environmentStorage{base: manager.store, id: id, legacy: profile.Legacy}
	}
	s := &Session{
		win:      win,
		lifetime: managerLifetime,
		store:    storage,
		manager:  manager, profile: profile,
		inflight:          map[string]queuedPrompt{},
		transportBlocked:  false,
		receiptChecks:     map[string]bool{},
		queued:            map[string][]queuedPrompt{},
		settingsVisited:   map[*Session]bool{},
		newThreadVisited:  map[*Session]bool{},
		editorChoice:      "auto",
		usageDays:         30,
		deviceLive:        true,
		mobilePage:        "threads",
		mobileUsageMetric: "cost",
		mobileUsageDays:   30,
		mobilePRState:     "OPEN",
		mobileDiffMode:    "review",
		settingsSection:   "general",
		settingsEnv:       profile.ID,
		newThreadEnv:      profile.ID,
		newThreadPerm:     proto.PermissionFullAccess,
		newProjectEnv:     profile.ID,
		sidebarOpen:       true,
		sidebarWidth:      260,
		rightWidth:        360,
		attachmentDrafts:  map[string][]proto.Attachment{},
		customModels:      map[string][]proto.Model{},
		drafts:            map[string]string{},
		pinned:            map[string]bool{},
		sidebarCollapsed:  map[string]bool{},
		seen:              map[string]time.Time{},
		expanded:          map[string]bool{},
		providerTextDraft: map[string]string{},
		providerAddMode:   "registry",
		settings:          proto.DefaultSettings(),
		terminalHub:       newTerminalHub(),
	}
	if manager != nil {
		s.appearance = manager.appearance
	} else {
		s.appearance = loadClientAppearance(shared)
	}
	// The sidebar keeps the size and state it had last time.
	if raw, ok := s.store.GetItem("sidebarOpen"); ok {
		s.sidebarOpen = raw != "false"
	}
	if raw, ok := s.store.GetItem("settingsSection"); ok && raw != "" {
		s.settingsSection = raw
	}
	if raw, ok := s.store.GetItem("rightOpen"); ok {
		s.rightOpen = raw != "false"
	}
	s.restoreEnvironmentData()
	if raw, ok := s.store.GetItem("rightWidth"); ok {
		if w, err := strconv.ParseFloat(raw, 64); err == nil && w > 0 {
			s.rightWidth = float32(w)
		}
	}
	if raw, ok := s.store.GetItem("sidebarWidth"); ok {
		if w, err := strconv.ParseFloat(raw, 64); err == nil && w > 0 {
			s.sidebarWidth = float32(w)
		}
	}
	url, _ := s.store.GetItem("serverUrl")
	token, _ := s.store.GetItem("token")
	// A dev script can point a fresh profile at an environment.
	if value := strings.TrimSpace(os.Getenv("T3GO_SERVER")); value != "" {
		url = value
	}
	if value := strings.TrimSpace(os.Getenv("T3GO_TOKEN")); value != "" {
		token = value
	}
	// On the web the client is served by the environment, so its own
	// address is the server, and "#token=..." in the link logs in.
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
	s.serverURL = url
	s.token = token
	s.monitorDirect()
	if manager != nil && profile.ID != "" && len(profile.Routes) > 0 {
		s.serverURL = profile.Routes[0].URL
		s.token = profile.Token
		s.connectProfile(profile)
	} else if manager == nil || !manager.adding {
		if url != "" {
			s.connect(url, token)
		}
	} else {
		s.serverURL = ""
		s.token = ""
	}
	return s
}

// managerLifetime is the client process's lifetime, the context every
// environment session's requests run under. The window's close cancels
// the sessions themselves.
var managerLifetime = context.Background()

// connect accepts an explicit address or a paired list of routes.
// connectProfile connects to one saved profile.
func (s *Session) connect(url, token string) {
	p, err := client.PairingProfile(url, token)
	if err != nil {
		s.connErr = err.Error()
		return
	}
	s.connectProfile(p)
}

func (s *Session) client() *client.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cli
}

func (s *Session) applyState(st proto.State) {
	s.idempotentMessages = slices.Contains(st.Capabilities, proto.CapabilityIdempotentMessages)
	if hostname := strings.TrimSpace(st.Hostname); hostname != s.profile.Hostname || !sameSystemInfo(st.System, s.profile.System) {
		s.profile.Hostname = hostname
		s.profile.System = st.System
		if s.manager != nil {
			s.manager.update(s.profile)
		}
	}
	s.projects = st.Projects
	s.threads = st.Threads
	s.providers = st.Providers
	s.models = st.Models
	s.settings = st.Settings
	s.serverVersion = st.ServerVersion
	s.rememberThreads(st.Threads)
}

// restoreEnvironmentData loads the execution-specific data of this
// environment again, so adding another route cannot replace its existing
// drafts with empty maps.
func (s *Session) restoreEnvironmentData() {
	s.editorChoice = "auto"
	if editor, ok := s.store.GetItem("editorChoice"); ok && strings.TrimSpace(editor) != "" {
		s.editorChoice = strings.TrimSpace(editor)
	} else if s.manager != nil {
		// Import the old shared preference once; subsequent changes belong
		// only to this machine, where the editor is actually launched.
		if editor, ok := s.manager.store.GetItem("editorChoice"); ok && strings.TrimSpace(editor) != "" {
			s.editorChoice = strings.TrimSpace(editor)
			_ = s.store.SetItem("editorChoice", strings.TrimSpace(editor))
		}
	}
	s.customModels = storedValue(s.store, "customModels", map[string][]proto.Model{})
	s.attachmentDrafts = storedValue(s.store, "attachmentDrafts", map[string][]proto.Attachment{})
	s.drafts = storedValue(s.store, "drafts", map[string]string{})
	s.pinned = storedValue(s.store, "pinnedThreads", map[string]bool{})
	s.sidebarCollapsed = storedValue(s.store, "sidebarCollapsed", map[string]bool{})
	s.queued = storedValue(s.store, "pendingMessages", map[string][]queuedPrompt{})
	s.queuePaused = len(s.queued) > 0
}

// setClientLanguageUnused keeps the i18n helper referenced until the
// settings view wires it up.
var _ = i18n.T
