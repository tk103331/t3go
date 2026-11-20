package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
	"sort"
)

// Access belongs to the server running on this machine. A loopback direct
// route is the durable local profile signal; relay-only environments are
// always remote from the client's point of view.
func isLocalEnvironmentProfile(profile client.EnvironmentProfile) bool {
	for _, route := range profile.Routes {
		if route.Kind == "relay" {
			continue
		}
		raw := strings.TrimRight(strings.TrimSpace(route.URL), "/")
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return true
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	return false
}

// localSettingsTarget never follows the environment chips. Access and About
// stay bound to the active local machine even while a remote thread is open.
func (s *Session) localSettingsTarget() *Session {
	if s.manager == nil {
		if isLocalEnvironmentProfile(s.profile) {
			return s
		}
		return nil
	}
	for _, candidate := range []*Session{s.manager.app, s.manager.sessions[s.manager.defaultID]} {
		if candidate != nil && isLocalEnvironmentProfile(candidate.profile) {
			return candidate
		}
	}
	for _, profile := range s.manager.profiles {
		if !isLocalEnvironmentProfile(profile) {
			continue
		}
		if target := s.manager.sessions[profile.ID]; target != nil {
			return target
		}
	}
	return nil
}

func accessRouteMode(settings proto.Settings) (direct, relay bool) {
	switch strings.ToLower(strings.TrimSpace(settings.AccessRoutes)) {
	case "direct":
		return true, false
	case "relay":
		return false, true
	default:
		return true, true
	}
}

func setAccessRouteMode(settings *proto.Settings, direct, relay bool) {
	switch {
	case direct && relay:
		settings.AccessRoutes = "both"
	case direct:
		settings.AccessRoutes = "direct"
	case relay:
		settings.AccessRoutes = "relay"
	}
}

func firstAccessRoute(profile client.EnvironmentProfile, kind string) (client.Route, bool) {
	var fallback client.Route
	haveFallback := false
	for _, route := range profile.Routes {
		if route.Kind == kind {
			if kind != "direct" || !isLoopbackURL(route.URL) {
				return route, true
			}
			if !haveFallback {
				fallback, haveFallback = route, true
			}
		}
	}
	return fallback, haveFallback
}

func (s *Session) applyRelayAccess(status proto.RelayAccessStatus) {
	s.relayAccess = status
	if !status.Configured || strings.TrimSpace(status.Route) == "" {
		return
	}
	route := client.Route{URL: strings.TrimRight(status.Route, "/"), Kind: "relay"}
	found := false
	for _, existing := range s.profile.Routes {
		if existing.URL == route.URL {
			found = true
			break
		}
	}
	if !found {
		s.profile.Routes = append(s.profile.Routes, route)
		if s.manager != nil {
			s.manager.update(s.profile)
		}
	}
	s.refreshAccess()
}

func (s *Session) refreshRelayAccess() {
	if !s.connected || !isLocalEnvironmentProfile(s.profile) {
		return
	}
	c := s.client()
	if c == nil {
		return
	}
	s.relaySeq++
	seq := s.relaySeq
	s.relayErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 10*time.Second)
		defer cancel()
		status, err := c.RelayAccess(ctx)
		s.win.Update(func() {
			if seq != s.relaySeq {
				return
			}
			if err != nil {
				s.relayErr = friendlyError(err)
				return
			}
			s.applyRelayAccess(status)
		})
	}()
}

func (s *Session) configureRelay() {
	if s.relayBusy {
		return
	}
	c := s.client()
	if c == nil {
		s.relayErr = i18n.T("Connect the local environment before configuring Relay.")
		return
	}
	session, server, ok := s.relayAccountSession()
	if !ok {
		s.relayErr = i18n.T("Sign in from Account before connecting Relay.")
		return
	}
	in := proto.RelayAccessConfig{
		Server: server, UserToken: session.Token, DirectURL: s.localLANURL(),
		AllowHTTP: s.manager.accountAllowHTTP,
	}
	s.relayBusy = true
	s.relayErr = ""
	s.relaySeq++
	seq := s.relaySeq
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 45*time.Second)
		defer cancel()
		status, err := c.ConfigureRelay(ctx, in)
		s.win.Update(func() {
			s.relayBusy = false
			if seq != s.relaySeq {
				return
			}
			if err != nil {
				s.relayErr = friendlyError(err)
				return
			}
			s.settingsDraft.AccessRoutes = "both"
			s.applyRelayAccess(status)
			s.scheduleRelayStatusRefresh()
		})
	}()
}

// Registration returns before the WebSocket tunnel finishes connecting. Poll a
// few times so Access can move from Connecting to Online without reopening it.
func (s *Session) scheduleRelayStatusRefresh() {
	for _, delay := range []time.Duration{750 * time.Millisecond, 2 * time.Second, 5 * time.Second} {
		go func(delay time.Duration) {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-s.lifetime.Done():
				return
			case <-timer.C:
			}
			s.win.Update(func() {
				if s.settingsSection == "access" {
					s.refreshRelayAccess()
				}
			})
		}(delay)
	}
}

func relayAccessStateLabel(status proto.RelayAccessStatus) string {
	switch strings.ToLower(strings.TrimSpace(status.State)) {
	case "connecting":
		return i18n.T("Connecting")
	case "retrying":
		return i18n.T("Reconnecting")
	case "active":
		return "Online"
	case "error":
		return i18n.T("Connection error")
	default:
		return "Configured"
	}
}

// refreshAccess builds the pairing link from the current environment's saved
// identity and routes. The link is only rendered after the environment's
// Access switch has been enabled.
func (s *Session) refreshAccess() {
	s.accessLink = ""
	s.accessQR = nil
	s.accessErr = ""
	if !s.settingsDraft.AccessEnabled {
		return
	}
	if !isLocalEnvironmentProfile(s.profile) {
		s.accessErr = i18n.T("Access is only available for the environment running on this machine.")
		return
	}
	direct, relay := accessRouteMode(s.settingsDraft)
	link, err := client.PairingLinkOptions(s.profile, direct, relay)
	if err != nil {
		s.accessErr = err.Error()
		return
	}
	png, err := qrcode.Encode(link, qrcode.Medium, 320)
	if err != nil {
		s.accessErr = i18n.T("Could not draw pairing QR code: ") + err.Error()
		return
	}
	s.accessLink = link
	s.accessQR = png
}

// accessRouteWarning explains when a pairing link's direct route only
// works on this machine.
func accessRouteWarning(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Query().Get("url"))
	parsed, err := url.Parse(host)
	if err != nil {
		return ""
	}
	name := strings.ToLower(parsed.Hostname())
	if name == "localhost" || name == "127.0.0.1" || name == "::1" {
		return i18n.T("This direct route only works on this machine. Turn on LAN access for devices on the same Wi-Fi, or configure Relay for access across networks.")
	}
	return ""
}

// localLANURL is the address a phone on the same network can use. It is
// deliberately separate from Relay: LAN pairing uses the environment token
// and does not require an account.
func (s *Session) localLANURL() string {
	if s.manager == nil || !s.manager.lan || s.manager.local == nil ||
		s.manager.local.lanIP == "" {
		return ""
	}
	u, err := url.Parse(s.manager.local.baseURL)
	if err != nil || u.Port() == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(s.manager.local.lanIP, u.Port())
}

func (s *Session) hasLANAccessCard() bool {
	return s.manager != nil && bundledServerPath() != "" && strings.TrimSpace(os.Getenv("T3GO_SERVER")) == ""
}

func (s *Session) settingsTarget() *Session {
	if s.manager == nil {
		return s
	}
	if settingsUsesLocalTarget(s.settingsSection) {
		return s.localSettingsTarget()
	}
	return s.manager.sessions[s.settingsEnv]
}

func settingsUsesLocalTarget(section string) bool {
	return section == "access" || section == "about"
}

func (s *Session) prepareLocalSettingsTarget(section string) {
	if !settingsUsesLocalTarget(section) {
		return
	}
	target := s.localSettingsTarget()
	if target == nil {
		return
	}
	if s.settingsVisited == nil {
		s.settingsVisited = map[*Session]bool{}
	}
	if !s.settingsVisited[target] {
		target.beginSettingsDraft()
		s.settingsVisited[target] = true
	}
	s.settingsEnv = target.profile.ID
}

func (s *Session) settingsConnectionReady() bool {
	return s.connected && !s.offline && !s.transportBlocked && s.client() != nil
}

func (s *Session) beginSettingsDraft() {
	st := s.settings
	s.settingsDraft = st
	s.loadProviderTextDraft(st)
	s.settingsTitle = st.Title
}

func (s *Session) selectSettingsEnvironment(id string) {
	if s.manager == nil || id == s.settingsEnv || s.settingsSaveBusy {
		return
	}
	target := s.manager.sessions[id]
	if target == nil {
		return
	}
	if s.settingsVisited == nil {
		s.settingsVisited = map[*Session]bool{}
	}
	if !s.settingsVisited[target] {
		target.beginSettingsDraft()
		s.settingsVisited[target] = true
	}
	s.settingsEnv = id
	s.settingsSaveErr = ""
	s.loadSettingsSection(s.settingsSection)
}

func (s *Session) loadSettingsSection(section string) {
	target := s.settingsTarget()
	if target == nil {
		return
	}
	switch section {
	case "providers":
		target.refreshProviders()
	case "access":
		target.refreshRelayAccess()
		target.refreshAccess()
	case "usage":
		target.loadUsage(target.usageDays)
	}
}

// Keep errors with the window that initiated the request, even when its target
// is another environment. Only apply defaults after the server accepted them.
func (s *Session) saveTargetSettings(target *Session) {
	if s.settingsSaveBusy || !target.settingsConnectionReady() {
		return
	}
	target.syncProviderTextDraft()
	st := target.settingsDraft
	st.Title = target.settingsTitle
	s.settingsSaveBusy = true
	s.settingsSaveErr = ""
	var saved proto.Settings
	target.call(func(ctx context.Context, c *client.Client) error {
		var err error
		saved, err = c.SaveSettings(ctx, st)
		return err
	}, func(err error) {
		s.settingsSaveBusy = false
		if err != nil {
			s.settingsSaveErr = target.envName() + ": " + friendlyError(err)
			return
		}
		target.settings = saved
		s.closeSettings()
	})
}

func (s *Session) openEnvironmentSettings(id string) {
	if target := s.manager.sessions[id]; target != nil {
		if s.settingsVisited == nil {
			s.settingsVisited = map[*Session]bool{}
		}
		if !s.settingsVisited[target] {
			target.beginSettingsDraft()
			s.settingsVisited[target] = true
		}
		s.selectSettingsEnvironment(id)
		s.setSettingsSection("environment-defaults")
		if s.narrow {
			s.mobileSettingsDetail = "environment-defaults"
		}
	}
}

func (s *Session) runSettingsProviderCommand(target *Session, command string) {
	if !target.settingsConnectionReady() {
		target.providersErr = i18n.T("Connect this environment before running its setup command.")
		return
	}
	if target.selected == "" {
		s.setClipboard(command)
		target.providersErr = i18n.T("Command copied. Open a thread in ") + target.envName() + i18n.T(" to run it in that environment's terminal.")
		return
	}
	s.closeSettings()
	target.inEnvironment(func() { target.runProviderCommand(command) })()
}

func commandArgsText(args []string) string {
	return strings.Join(args, "\n")
}

// commandArgs parses one literal argument per line. Unlike a shell string,
// spaces and quotes remain part of the argument.
func commandArgs(text string) []string {
	var args []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			args = append(args, value)
		}
	}
	return args
}

func providerTextKeyACP(id, field string) string {
	return "acp:" + id + ":" + field
}

func (s *Session) loadProviderTextDraft(settings proto.Settings) {
	values := map[string]string{}
	for _, configured := range settings.ACPProviders {
		values[providerTextKeyACP(configured.ID, "name")] = configured.Name
		values[providerTextKeyACP(configured.ID, "path")] = configured.CommandPath
		values[providerTextKeyACP(configured.ID, "args")] = commandArgsText(configured.CommandArgs)
	}
	s.providerTextDraft = values
}

func (s *Session) providerTextValue(key, fallback string) string {
	if value, ok := s.providerTextDraft[key]; ok {
		return value
	}
	return fallback
}

func (s *Session) setProviderTextValue(key, value string) {
	next := make(map[string]string, len(s.providerTextDraft)+1)
	for existingKey, existingValue := range s.providerTextDraft {
		next[existingKey] = existingValue
	}
	next[key] = value
	s.providerTextDraft = next
}

func (s *Session) syncProviderTextDraft() {
	settings := s.settingsDraft
	for index := range settings.ACPProviders {
		id := settings.ACPProviders[index].ID
		if value, ok := s.providerTextDraft[providerTextKeyACP(id, "name")]; ok {
			settings.ACPProviders[index].Name = strings.TrimSpace(value)
		}
		if value, ok := s.providerTextDraft[providerTextKeyACP(id, "path")]; ok {
			settings.ACPProviders[index].CommandPath = strings.TrimSpace(value)
		}
		if value, ok := s.providerTextDraft[providerTextKeyACP(id, "args")]; ok {
			settings.ACPProviders[index].CommandArgs = commandArgs(value)
		}
	}
	s.settingsDraft = settings
}

func (s *Session) updateProviderSettings(update func(*proto.Settings)) {
	settings := s.settingsDraft
	update(&settings)
	s.settingsDraft = settings
}

func (s *Session) projectTarget() *Session {
	if s.manager == nil {
		return s
	}
	return s.manager.sessions[s.newProjectEnv]
}

func (s *Session) selectProjectEnvironment(id string) {
	if s.projectBusy || id == s.newProjectEnv {
		return
	}
	s.newProjectEnv = id
	s.newProjectPath = ""
	s.newProjectRemote = true
	s.connErr = ""
	s.dirsSeq++
	s.newDirSeq++
	s.dirs = proto.HostDirs{}
	s.dirsBusy = false
	s.dirsErr = ""
	s.newDirBusy = false
	s.showDirs = false
	s.closeNewDir()
}

// Capture the selected client before starting IO. Completion belongs to the
// dialog; a removed target cancels IO without mutating another session.
func (s *Session) callProjectTarget(fn func(context.Context, *client.Client) error, done func(error)) {
	target := s.projectTarget()
	if target == nil || !target.connected || target.client() == nil {
		done(errors.New(i18n.T("Connect the selected environment in Settings first.")))
		return
	}
	if target.transportBlocked {
		done(errors.New(i18n.T("Update the selected environment's token in Settings first.")))
		return
	}
	c := target.client()
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 30*time.Second)
		stop := context.AfterFunc(target.lifetime, cancel)
		defer stop()
		defer cancel()
		err := fn(ctx, c)
		s.win.Update(func() { done(err) })
	}()
}

func (s *Session) threadKey(id string) string {
	if id == "" || s.manager == nil {
		return id
	}
	return s.profile.ID + "/" + id
}

func (s *Session) sidebarID(id string) string {
	if s.manager != nil && len(s.manager.profiles) > 1 {
		return s.profile.ID + "/" + id
	}
	return id
}

func (s *Session) selectSidebarThread(key string) {
	if s.manager == nil {
		if projectID, ok := strings.CutPrefix(key, "project:"); ok {
			s.openNewThreadIn(projectID)
			return
		}
		s.showNewThread = false
		s.selectThread(key)
		return
	}
	env, target, ok := strings.Cut(key, "/")
	if session := s.manager.sessions[env]; ok && session != nil {
		s.manager.activate(env)
		if projectID, isProject := strings.CutPrefix(target, "project:"); isProject {
			session.openNewThreadIn(projectID)
			return
		}
		s.showNewThread = false
		session.selectThread(target)
	}
}

func (s *Session) inEnvironment(action func()) func() {
	return func() {
		if s.manager != nil {
			s.manager.activate(s.profile.ID)
		}
		action()
	}
}

func (s *Session) newThreadTarget() *Session {
	if s.manager == nil {
		return s
	}
	return s.manager.sessions[s.newThreadEnv]
}

func (s *Session) selectThreadEnvironment(id string) {
	if target := s.newThreadTarget(); target != nil && target.threadBusy {
		return
	}
	for _, session := range s.mobileSessions("") {
		if session.profile.ID == id {
			if s.newThreadVisited == nil {
				s.newThreadVisited = map[*Session]bool{}
			}
			if !s.newThreadVisited[session] {
				session.prepareNewThread()
				s.newThreadVisited[session] = true
			}
			s.newThreadEnv = id
			return
		}
	}
}

func (s *Session) mobileSessions(id string) []*Session {
	if s.manager == nil {
		return []*Session{s}
	}
	var sessions []*Session
	for _, p := range s.manager.profiles {
		if session := s.manager.sessions[p.ID]; session != nil && (id == "" || id == p.ID) {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

func (s *Session) loadMobileUsage() {
	for _, session := range s.mobileSessions(s.mobileUsageEnv) {
		session.loadUsage(s.mobileUsageDays)
	}
}

func (s *Session) loadMobilePullRequests() {
	s.mobilePRSeq++
	seq := s.mobilePRSeq
	type job struct {
		session                             *Session
		client                              *client.Client
		thread                              proto.Thread
		project, environment, environmentID string
	}
	var jobs []job
	var warnings []string
	for _, session := range s.mobileSessions(s.mobilePREnv) {
		name := session.envName()
		if !session.settingsConnectionReady() {
			warnings = append(warnings, name+" · "+session.connectionLabel())
			continue
		}
		projects := map[string]proto.Project{}
		for _, project := range session.projects {
			projects[project.ID] = project
		}
		seen := map[string]bool{}
		for _, thread := range session.threads {
			if thread.SideChat {
				continue
			}
			project := projects[thread.ProjectID]
			key := project.Path + "/" + thread.WorktreePath + ":" + thread.Branch
			if seen[key] {
				continue
			}
			seen[key] = true
			jobs = append(jobs, job{session, session.client(), thread, project.Name, name, session.profile.ID})
		}
	}
	s.mobilePRRows = nil
	s.mobilePRErr = strings.Join(warnings, "\n")
	s.mobilePRBusy = len(jobs) > 0
	if len(jobs) == 0 {
		return
	}
	// Bound CLI probes so a large inbox does not start a shell per thread.
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 30*time.Second)
		defer cancel()
		type result struct {
			rows    []mobilePullRequest
			warning string
		}
		results := make(chan result, len(jobs))
		limit := make(chan struct{}, 4)
		for _, task := range jobs {
			go func(j job) {
				select {
				case limit <- struct{}{}:
				case <-ctx.Done():
					results <- result{warning: j.environment + ": " + ctx.Err().Error()}
					return
				}
				defer func() { <-limit }()
				requestCtx, stop := context.WithCancel(ctx)
				stopLifetime := context.AfterFunc(j.session.lifetime, stop)
				defer stopLifetime()
				defer stop()
				prs, detail, err := j.client.PullRequests(requestCtx, j.thread.ID)
				r := result{}
				if err != nil {
					detail = friendlyError(err)
				}
				if detail != "" {
					r.warning = j.environment + " · " + j.project + ": " + detail
				}
				for _, pr := range prs {
					r.rows = append(r.rows, mobilePullRequest{j.environmentID, j.environment, j.project, pr})
				}
				results <- r
			}(task)
		}
		var rows []mobilePullRequest
		seen := map[string]bool{}
		for range jobs {
			r := <-results
			if r.warning != "" {
				warnings = append(warnings, r.warning)
			}
			for _, row := range r.rows {
				key := row.EnvironmentID + "/" + row.PR.URL
				if row.PR.URL == "" {
					key += row.Project + strconv.Itoa(row.PR.Number)
				}
				if !seen[key] {
					seen[key] = true
					rows = append(rows, row)
				}
			}
		}
		s.win.Update(func() {
			if seq != s.mobilePRSeq {
				return
			}
			s.mobilePRRows = rows
			s.mobilePRErr = strings.Join(warnings, "\n")
			s.mobilePRBusy = false
		})
	}()
}

// openMobileSurface opens a right-panel surface as the mobile conversation's
// full-screen sheet.
func (s *Session) openMobileSurface(name string) {
	s.openSurface(name)
	s.mobileSurface = name
}

func (s *Session) closeMobileThreadSearch() {
	s.sidebarQuery = ""
	s.mobileThreadSearch = false
}

// showSurface opens the right panel on a surface.
func (s *Session) showSurface(name string) {
	s.setRightOpen(true)
	s.openSurface(name)
}

// archiveCurrentThread archives the open thread.
func (s *Session) archiveCurrentThread() {
	id := s.selected
	for _, thread := range s.threads {
		if thread.ID == id {
			s.setArchived(thread, true)
			return
		}
	}
}

// removeMention takes one @ path out of the draft.
func (s *Session) removeMention(path string) {
	draft := s.draft
	draft = strings.Replace(draft, "@"+path, "", 1)
	s.setDraft(strings.Join(strings.Fields(draft), " "))
}

// insertMention replaces the mention being typed with one file.
func (s *Session) insertMention(path string) {
	draft := s.draft
	at, _, ok := activeMention(draft)
	if !ok {
		return
	}
	s.setDraft(draft[:at] + "@" + path + " ")
}

// setThreadSettled files a thread away from, or back into, the working
// inbox.
func (s *Session) setThreadSettled(id string, settled bool) {
	if id == "" {
		return
	}
	s.connErr = ""
	var updated proto.Thread
	s.call(func(ctx context.Context, c *client.Client) error {
		var err error
		updated, err = c.UpdateThread(ctx, id, map[string]any{"settled": settled})
		if err == nil && updated.Settled != settled {
			return errors.New(i18n.T("This environment needs a newer T3Go server to support Settle."))
		}
		return err
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.upsertThread(updated)
		detail := s.detail
		if detail.Thread.ID == id {
			detail.Thread = updated
			s.detail = detail
		}
	})
}

// mobilePullRequest is one pull request row in the mobile inbox, named by
// the environment and project it belongs to.
type mobilePullRequest struct {
	EnvironmentID, Environment, Project string
	PR                                  proto.PullRequest
}

// preferredRegistryDistribution picks the registry distribution the add
// button installs by default.
func preferredRegistryDistribution(agent proto.ACPRegistryAgent) string {
	for _, distribution := range agent.Distributions {
		if distribution == "npx" || distribution == "uvx" || distribution == "binary" {
			return distribution
		}
	}
	return ""
}

// openProviderAdd opens the add-provider panel and primes the registry
// search.
func (s *Session) openProviderAdd() {
	s.providerAddOpen = true
	s.providerManageErr = ""
	s.searchProviderCatalog(false)
}

// searchProviderCatalog queries the environment's ACP Registry cache.
func (s *Session) searchProviderCatalog(refresh bool) {
	if !s.settingsConnectionReady() {
		s.providerCatalogErr = i18n.T("Connect this environment to search the ACP Registry.")
		return
	}
	s.providerSeq++
	seq := s.providerSeq
	query := s.providerSearchQuery
	s.providerCatalogBusy = true
	s.providerCatalogErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		agents, err := c.ACPRegistry(ctx, query, refresh)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			if seq != s.providerSeq {
				return
			}
			sort.SliceStable(agents, func(i, j int) bool {
				return strings.ToLower(agents[i].Name) < strings.ToLower(agents[j].Name)
			})
			s.providerCatalog = agents
			s.providerCatalogBusy = false
		})
		return nil
	}, func(err error) {
		if seq != s.providerSeq {
			return
		}
		s.win.Update(func() {
			s.providerCatalogBusy = false
			if err != nil {
				s.providerCatalogErr = friendlyError(err)
			}
		})
	})
}

// acpProviderID mints an id for a configured ACP provider that is unique
// among the ones the environment already has.
func acpProviderID(name, agentID string, existing []proto.ACPProviderSettings) string {
	baseID := strings.ToLower(strings.TrimSpace(agentID))
	if baseID == "" {
		baseID = strings.ToLower(strings.TrimSpace(name))
	}
	var cleaned strings.Builder
	for _, r := range baseID {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			cleaned.WriteRune(r)
		case r == '-', r == '_':
			cleaned.WriteRune('-')
		default:
			if cleaned.Len() > 0 && !strings.HasSuffix(cleaned.String(), "-") {
				cleaned.WriteByte('-')
			}
		}
	}
	id := "acp-" + strings.Trim(cleaned.String(), "-")
	if id == "acp-" {
		id = "acp-provider"
	}
	candidate := id
	for n := 2; ; n++ {
		used := false
		for _, configured := range existing {
			if configured.ID == candidate {
				used = true
				break
			}
		}
		if !used {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", id, n)
	}
}

// addRegistryProvider adds a registry agent to the environment's
// settings, preparing a binary distribution's install first.
func (s *Session) addRegistryProvider(agent proto.ACPRegistryAgent, distribution string) {
	if s.providerPrepareID != "" {
		return
	}
	settings := s.settingsDraft
	for _, configured := range settings.ACPProviders {
		if configured.Source == "registry" && configured.AgentID == agent.ID {
			s.providerManageErr = agent.Name + i18n.T(" is already configured for this environment.")
			return
		}
	}
	add := func() {
		s.updateProviderSettings(func(settings *proto.Settings) {
			settings.ACPProviders = append(settings.ACPProviders, proto.ACPProviderSettings{
				ID: acpProviderID(agent.Name, agent.ID, settings.ACPProviders), Name: agent.Name,
				Enabled: true, Source: "registry", AgentID: agent.ID, Version: agent.Version,
				Distribution: distribution,
			})
		})
		s.providerAddOpen = false
	}
	if distribution != "binary" {
		add()
		return
	}
	s.providerPrepareID = agent.ID
	s.providerManageErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.PrepareACPRegistry(ctx, agent.ID)
		return err
	}, func(err error) {
		s.win.Update(func() {
			s.providerPrepareID = ""
			if err != nil {
				s.providerManageErr = friendlyError(err)
				return
			}
			add()
		})
	})
}

// addLocalACPProvider adds a locally executable ACP agent.
func (s *Session) addLocalACPProvider() {
	name := strings.TrimSpace(s.providerLocalName)
	path := strings.TrimSpace(s.providerLocalPath)
	if name == "" {
		s.providerManageErr = i18n.T("Enter a provider name.")
		return
	}
	if path == "" {
		s.providerManageErr = i18n.T("Enter the local ACP executable.")
		return
	}
	s.updateProviderSettings(func(settings *proto.Settings) {
		settings.ACPProviders = append(settings.ACPProviders, proto.ACPProviderSettings{
			ID: acpProviderID(name, "", settings.ACPProviders), Name: name, Enabled: true, Source: "local",
			CommandPath: path, CommandArgs: commandArgs(s.providerLocalArgs),
		})
	})
	s.providerLocalName, s.providerLocalPath, s.providerLocalArgs = "", "", ""
	s.providerAddOpen = false
}

// providerSettingsSource names where a configured ACP provider came from.
func providerSettingsSource(source string) string {
	switch source {
	case "registry":
		return i18n.T("ACP Registry")
	case "local":
		return i18n.T("Local ACP")
	default:
		return source
	}
}
