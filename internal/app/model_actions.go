package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// connectProfile connects to one saved profile, or re-establishes it
// after a failure. It is the data layer's single reconnection path.
func (s *Session) connectProfile(profile client.EnvironmentProfile) {
	wasConnected := s.connected
	if wasConnected {
		s.saveEnvironmentSession()
	}
	s.connectionAttempt++
	attempt := s.connectionAttempt
	s.connecting = true
	s.connErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 15*time.Second)
		defer cancel()
		c, st, err := client.ConnectEnvironment(ctx, profile)
		s.win.Update(func() {
			if attempt != s.connectionAttempt {
				return
			}
			s.connecting = false
			if err != nil {
				if client.IsAuthorizationError(err) {
					s.transportBlocked = true
				}
				if s.showCredentials {
					s.credentialError = friendlyError(err)
				} else {
					s.connErr = friendlyError(err)
				}
				if wasConnected {
					if client.IsAuthorizationError(err) {
						s.blockTransport(err)
					} else if !s.transportBlocked {
						s.startStream(s.client(), s.eventSeq)
					}
				}
				return
			}
			// A second address for an existing identity updates that session.
			// Never replace its in-memory drafts or create a duplicate stream.
			if s.manager != nil && s.manager.pending == s {
				environmentID := st.EnvironmentID
				if environmentID == "" {
					environmentID = profile.ID
				}
				if environmentID == "" {
					environmentID = client.LegacyEnvironmentID(c.BaseURL)
				}
				if existing := s.manager.sessions[environmentID]; existing != nil && existing != s {
					if err := existing.adoptEnvironment(profile, c, st); err != nil {
						s.connErr = i18n.T("Could not save environment: ") + err.Error()
						return
					}
					existing.connectionAttempt++
					existing.connecting = false
					existing.finishConnection(c, st, existing.connected)
					s.manager.closePending()
					return
				}
			}
			if err := s.adoptEnvironment(profile, c, st); err != nil {
				message := i18n.T("Could not save environment: ") + err.Error()
				if s.showCredentials {
					s.credentialError = message
				} else {
					s.connErr = message
				}
				if wasConnected && !s.transportBlocked {
					s.startStream(s.client(), s.eventSeq)
				}
				return
			}
			s.finishConnection(c, st, wasConnected)
		})
	}()
}

func (s *Session) finishConnection(c *client.Client, st proto.State, wasConnected bool) {
	s.mu.Lock()
	s.cli = c
	s.mu.Unlock()
	_ = s.store.SetItem("serverUrl", c.BaseURL)
	_ = s.store.SetItem("token", c.Token)
	s.serverURL = c.BaseURL
	s.token = c.Token
	s.applyState(st)
	if !wasConnected {
		s.beginSettingsDraft()
	}
	s.transportBlocked = false
	s.showCredentials = false
	s.credentialToken = ""
	s.credentialError = ""
	s.routeRecovery.enter(time.Now(), routeKind(s.profile.Routes, c.BaseURL))
	s.eventSeq = st.Seq
	s.markOnline()
	s.connected = true
	s.startStream(c, st.Seq)
	s.loadApprovals()
	if wasConnected {
		if thread := s.selected; thread != "" {
			// The new snapshot's sequence skips events missed while offline.
			// Refresh the transcript without replacing the unsent draft.
			s.loadDetail(thread)
		}
	}
	if thread, ok := s.store.GetItem("selectedThread"); !wasConnected && ok && thread != "" {
		for _, th := range st.Threads {
			if th.ID == thread {
				s.selectThread(thread)
				break
			}
		}
	}
}

func (s *Session) startStream(c *client.Client, after int64) {
	if s.transportBlocked || c == nil {
		return
	}
	s.mu.Lock()
	if s.streamCancel != nil {
		s.streamCancel()
	}
	ctx, cancel := context.WithCancel(s.lifetime)
	s.streamCancel = cancel
	s.mu.Unlock()
	go c.StreamWithReady(ctx, after, func() {
		s.win.Update(func() {
			if ctx.Err() == nil && s.client() == c {
				s.markOnline()
			}
		})
	}, func(e proto.Event) {
		s.win.Update(func() {
			if ctx.Err() != nil || s.client() != c {
				return
			}
			if e.Seq > 0 && e.Seq <= s.eventSeq {
				return
			}
			s.markOnline()
			s.applyEvent(e)
			if e.Seq > s.eventSeq {
				s.eventSeq = e.Seq
			}
		})
	}, func(err error) {
		s.win.Update(func() {
			if ctx.Err() != nil || s.client() != c {
				return
			}
			if client.IsAuthorizationError(err) {
				s.blockTransport(err)
				return
			}
			if errors.Is(err, client.ErrResyncRequired) {
				s.resyncEnvironment(c)
				return
			}
			s.offline = true
			s.tryAlternateRoute(c, false)
		})
	})
}

func (s *Session) disconnect() {
	if s.manager != nil {
		s.openSettings()
		s.setSettingsSection(i18n.T("environments"))
		// TODO(view): the environments management view owns the forget
		// flow (the forgetEnvironment signal in the old client).
		return
	}
	// Files the user attached but never sent go with the connection.
	pending := append([]proto.Attachment(nil), s.attachments...)
	for _, queue := range s.queued {
		for _, prompt := range queue {
			pending = append(pending, prompt.Attachments...)
		}
	}
	s.discardAttachments(pending)
	s.mu.Lock()
	if s.streamCancel != nil {
		s.streamCancel()
		s.streamCancel = nil
	}
	s.cli = nil
	s.mu.Unlock()
	s.store.RemoveItem("serverUrl")
	s.store.RemoveItem("token")
	s.connected = false
	s.offline = false
	s.transportBlocked = false
	s.connectionRecovered = false
	// TODO(view): dictation (speech recognition) is not available in MyGo yet.
	s.queued = map[string][]queuedPrompt{}
	s.queue = nil
	s.attachments = nil
	s.selected = ""
	s.messages = nil
	s.detail = proto.ThreadDetail{}
	s.serverURL = ""
}

// reload fetches the environment snapshot again.
func (s *Session) reload() {
	c := s.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 10*time.Second)
		defer cancel()
		st, err := c.State(ctx)
		if err != nil {
			return
		}
		s.win.Update(func() {
			if s.client() == c {
				s.applyState(st)
			}
		})
	}()
}

// openSideChat opens the selected thread's hidden side chat, creating it
// on first use.
func (s *Session) openSideChat() {
	id := s.selected
	if id == "" {
		return
	}
	if s.sideFor == id && s.sideChat.ID != "" {
		return
	}
	s.resetSideChat()
	s.sideFor = id
	s.sideBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		thread, err := c.CreateSideChat(ctx, id)
		if err != nil {
			return err
		}
		detail, err := c.ThreadDetail(ctx, thread.ID)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			if s.sideFor != id {
				return
			}
			running := false
			for _, run := range detail.Runs {
				if run.Status == proto.RunRunning {
					running = true
				}
			}
			drafts := s.attachmentDrafts
			s.sideChat = thread
			s.sideMessages = detail.Messages
			s.sideRunning = running
			s.sideBusy = false
			s.sideAttachments = append([]proto.Attachment(nil), drafts[thread.ID]...)
		})
		return nil
	}, func(err error) {
		s.sideBusy = false
		if err != nil {
			s.sideErr = friendlyError(err)
		}
	})
}

// resetSideChat clears the side chat's transcript and draft.
func (s *Session) resetSideChat() {
	s.sideChat = proto.Thread{}
	s.sideMessages = nil
	s.sideRunning = false
	s.sideDraft = ""
	s.sideErr = ""
	s.sideAttachments = nil
}

// sendSideChat asks the side chat a question.
func (s *Session) sendSideChat() {
	thread := s.sideChat
	text := strings.TrimSpace(s.sideDraft)
	if thread.ID == "" || text == "" || s.sideRunning {
		return
	}
	attachments := s.sideAttachments
	ids := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		ids = append(ids, attachment.ID)
	}
	s.sideBusy = true
	s.sideErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		run, err := c.SendWith(ctx, thread.ID, text, ids)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			s.sideDraft = ""
			s.sideBusy = false
			s.sideRunning = run.Status == proto.RunRunning
			s.sideAttachments = nil
			s.removeAttachmentDraft(thread.ID)
		})
		return nil
	}, func(err error) {
		s.sideBusy = false
		if err != nil {
			s.sideErr = friendlyError(err)
		}
	})
}

// stopSideChat stops the side chat's turn.
func (s *Session) stopSideChat() {
	thread := s.sideChat
	if thread.ID == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.Stop(ctx, thread.ID)
		return err
	}, func(err error) {
		if err != nil {
			s.sideErr = friendlyError(err)
		}
	})
}

// pickSideAttachments opens the picker for the side chat's composer.
func (s *Session) pickSideAttachments() {
	if s.client() == nil || s.sideChat.ID == "" {
		return
	}
	s.sideErr = ""
	s.pickFiles(func(paths []string) {
		if len(paths) == 0 {
			return
		}
		s.attachSideFiles(filesFromPaths(paths))
	})
}

// attachSidePaths takes files dropped on the side chat's composer.
func (s *Session) attachSidePaths(uris []string) {
	s.attachSideFiles(filesFromPaths(droppedPaths(uris)))
}

// attachSideFiles uploads files for the side chat's next question.
func (s *Session) attachSideFiles(files []pickedFile) {
	c := s.client()
	thread := s.sideChat
	if c == nil || thread.ID == "" || len(files) == 0 {
		return
	}
	s.sideErr = ""
	for _, f := range files {
		f := f
		go func() {
			ctx, cancel := context.WithTimeout(s.lifetime, 60*time.Second)
			defer cancel()
			src, err := f.open()
			if err != nil {
				s.win.Update(func() { s.sideErr = i18n.T("Could not read ") + f.Name + ": " + err.Error() })
				return
			}
			attachment, uploadErr := c.Upload(ctx, f.Name, f.MIME, src)
			_ = src.Close()
			if f.Temporary && f.Path != "" {
				_ = os.Remove(f.Path)
			}
			s.win.Update(func() {
				if uploadErr != nil {
					s.sideErr = friendlyError(uploadErr)
					return
				}
				if s.sideChat.ID == thread.ID {
					s.sideAttachments = append(s.sideAttachments, attachment)
					s.saveSideAttachmentDraft()
					return
				}
				s.addAttachmentDraft(thread.ID, attachment)
			})
		}()
	}
}

// removeSideAttachment drops a staged file from the side chat.
func (s *Session) removeSideAttachment(id string) {
	files := s.sideAttachments
	next := make([]proto.Attachment, 0, len(files))
	for _, attachment := range files {
		if attachment.ID != id {
			next = append(next, attachment)
		}
	}
	s.sideAttachments = next
	s.saveSideAttachmentDraft()
}

// saveSideAttachmentDraft stores the side chat's staged files.
func (s *Session) saveSideAttachmentDraft() {
	thread := s.sideChat
	if thread.ID == "" {
		return
	}
	drafts := s.attachmentDrafts
	next := make(map[string][]proto.Attachment, len(drafts)+1)
	for key, value := range drafts {
		next[key] = value
	}
	if files := s.sideAttachments; len(files) > 0 {
		next[thread.ID] = append([]proto.Attachment(nil), files...)
	} else {
		delete(next, thread.ID)
	}
	s.attachmentDrafts = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("attachmentDrafts", string(raw))
	}
}

// removeAttachmentDraft forgets a thread's staged files.
func (s *Session) removeAttachmentDraft(threadID string) {
	if threadID == "" {
		return
	}
	drafts := s.attachmentDrafts
	if _, ok := drafts[threadID]; !ok {
		return
	}
	next := make(map[string][]proto.Attachment, len(drafts))
	for key, value := range drafts {
		if key != threadID {
			next[key] = value
		}
	}
	s.attachmentDrafts = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("attachmentDrafts", string(raw))
	}
}

// setSideModel changes the side chat's model.
func (s *Session) setSideModel(model proto.Model) {
	thread := s.sideChat
	if thread.ID == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		updated, err := c.UpdateThread(ctx, thread.ID, map[string]any{"model": model.ID, "provider": model.Provider})
		if err != nil {
			return err
		}
		s.win.Update(func() { s.sideChat = updated })
		return nil
	}, func(err error) {
		if err != nil {
			s.sideErr = friendlyError(err)
		}
	})
}

// setSidePermission changes the side chat's permission mode.
func (s *Session) setSidePermission(permission proto.PermissionMode) {
	thread := s.sideChat
	if thread.ID == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		updated, err := c.UpdateThread(ctx, thread.ID, map[string]any{"permission": string(permission)})
		if err != nil {
			return err
		}
		s.win.Update(func() { s.sideChat = updated })
		return nil
	}, func(err error) {
		if err != nil {
			s.sideErr = friendlyError(err)
		}
	})
}

// applySideDelta appends streamed text to a side chat message.
func (s *Session) applySideDelta(d proto.MessageDelta) {
	if d.MessageID == "" || d.Delta == "" {
		return
	}
	list := s.sideMessages
	next := make([]proto.Message, len(list))
	copy(next, list)
	for i := range next {
		if next[i].ID == d.MessageID {
			next[i].Content += d.Delta
			s.sideMessages = next
			return
		}
	}
}

// setEditor remembers where Open in editor sends a checkout.
func (s *Session) setEditor(choice string) {
	s.editorChoice = choice
	_ = s.store.SetItem("editorChoice", choice)
}

// openInEditor opens the selected thread's checkout in the editor.
func (s *Session) openInEditor() {
	id := s.selected
	if id == "" {
		return
	}
	s.connErr = ""
	choice := s.editorChoice
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.OpenEditor(ctx, id, choice)
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
		}
	})
}

// allModels is the providers' models plus the app's custom ones.
func (s *Session) allModels() []proto.Model {
	base := s.models
	custom := s.customModels
	out := make([]proto.Model, 0, len(base)+8)
	seen := map[[2]string]bool{}
	for _, model := range base {
		key := [2]string{model.Provider, model.ID}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, model)
	}
	providers := make([]string, 0, len(custom))
	for provider := range custom {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		for _, model := range custom[provider] {
			key := [2]string{provider, model.ID}
			if seen[key] {
				continue
			}
			seen[key] = true
			model.Provider = provider
			model.Custom = true
			out = append(out, model)
		}
	}
	return out
}

// customModelList returns the custom models of one provider.
func (s *Session) customModelList(provider string) []proto.Model {
	return s.customModels[provider]
}

// openCustomModelDialog asks for a model id to add to a provider.
func (s *Session) openCustomModelDialog(provider string) {
	s.customModelProvider = provider
	s.customModelID = ""
	s.customModelName = ""
	s.customModelErr = ""
	s.showCustomModel = true
}

// addCustomModel records a model id for a provider.
func (s *Session) addCustomModel() {
	provider := s.customModelProvider
	id := strings.TrimSpace(s.customModelID)
	name := strings.TrimSpace(s.customModelName)
	if provider == "" || id == "" {
		s.customModelErr = i18n.T("Enter the model id the CLI expects.")
		return
	}
	if name == "" {
		name = id
	}
	models := s.customModels
	next := make(map[string][]proto.Model, len(models)+1)
	for key, value := range models {
		next[key] = value
	}
	for _, model := range next[provider] {
		if model.ID == id {
			s.customModelErr = i18n.T("That model is already listed.")
			return
		}
	}
	next[provider] = append(next[provider], proto.Model{ID: id, Name: name, Provider: provider, Custom: true})
	s.customModels = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("customModels", string(raw))
	}
	s.showCustomModel = false
}

// removeCustomModel forgets a custom model id.
func (s *Session) removeCustomModel(provider, id string) {
	models := s.customModels
	next := make(map[string][]proto.Model, len(models))
	for key, value := range models {
		if key != provider {
			next[key] = value
			continue
		}
		kept := make([]proto.Model, 0, len(value))
		for _, model := range value {
			if model.ID != id {
				kept = append(kept, model)
			}
		}
		if len(kept) > 0 {
			next[key] = kept
		}
	}
	s.customModels = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("customModels", string(raw))
	}
}

// refreshProviders re-probes the agent CLIs, after a sign-in for example.
func (s *Session) refreshProviders() {
	if !s.settingsConnectionReady() {
		s.providersErr = i18n.T("Connect this environment before detecting its providers.")
		return
	}
	s.providersSeq++
	seq := s.providersSeq
	s.providersBusy = true
	s.providersErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		providers, models, err := c.Providers(ctx)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			if seq != s.providersSeq {
				return
			}
			s.providers = providers
			s.models = models
		})
		return nil
	}, func(err error) {
		if seq != s.providersSeq {
			return
		}
		s.providersBusy = false
		if err != nil {
			s.providersErr = friendlyError(err)
		}
	})
}

func (s *Session) applyEvent(e proto.Event) {
	switch e.Type {
	case proto.EventThreadCreated, proto.EventThreadUpdated:
		t, ok := decodeData[proto.Thread](e.Data)
		if !ok {
			return
		}
		if t.SideChat {
			if s.sideChat.ID == t.ID {
				s.sideChat = t
			}
			return
		}
		s.upsertThread(t)
		if s.selected == t.ID {
			if s.manager == nil || s.manager.app == s {
				s.markSeen(t)
			}
			d := s.detail
			d.Thread = t
			s.detail = d
			if t.Status != proto.ThreadRunning {
				s.running = false
				s.maybeFlush(t.ID)
			}
		}
	case proto.EventThreadDeleted:
		if side := s.sideChat; side.ID != "" && side.ID == e.ThreadID {
			s.sideFor = ""
			s.resetSideChat()
			return
		}
		s.removeThread(e.ThreadID)
		if s.selected == e.ThreadID {
			s.selected = ""
			s.draft = ""
			s.attachments = nil
			s.messages = nil
			s.detail = proto.ThreadDetail{}
		}
	case proto.EventApproval:
		if request, ok := decodeData[proto.ApprovalRequest](e.Data); ok && request.ID != "" {
			s.addApproval(request)
		}
	case proto.EventApprovalResolved:
		if resolved, ok := decodeData[map[string]any](e.Data); ok {
			if id, _ := resolved["id"].(string); id != "" {
				s.removeApproval(id)
			}
		}
	case proto.EventSettings:
		if settings, ok := decodeData[proto.Settings](e.Data); ok {
			s.settings = settings
		}
	case proto.EventTerminalExit:
		s.loadTerminalSessions()
	case proto.EventProject:
		if p, ok := decodeData[proto.Project](e.Data); ok && p.ID != "" {
			s.upsertProject(p)
			return
		}
		if gone, ok := decodeData[map[string]string](e.Data); ok {
			if id := gone["deleted"]; id != "" {
				s.removeProject(id)
			}
		}
	case proto.EventMessage:
		m, ok := decodeData[proto.Message](e.Data)
		if ok && m.ThreadID == s.sideChat.ID && m.ThreadID != "" {
			s.sideMessages = upsertProto(s.sideMessages, m)
		}
		if ok && m.ThreadID == s.selected {
			s.applyMessage(m)
		}
	case proto.EventMessageDelta:
		d, ok := decodeData[proto.MessageDelta](e.Data)
		if ok && e.ThreadID == s.sideChat.ID && e.ThreadID != "" {
			s.applySideDelta(d)
		}
		if ok && e.ThreadID == s.selected {
			s.applyDelta(d)
		}
	case proto.EventRunStarted:
		if e.ThreadID == s.sideChat.ID && e.ThreadID != "" {
			s.sideRunning = true
		}
		if e.ThreadID == s.selected {
			s.running = true
		}
	case proto.EventRunFinished:
		if e.ThreadID == s.sideChat.ID && e.ThreadID != "" {
			s.sideRunning = false
		}
		if e.ThreadID == s.selected {
			s.running = false
			s.maybeFlush(e.ThreadID)
			if run, ok := decodeData[proto.Run](e.Data); ok {
				d := s.detail
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
				s.detail = d
			}
		}
	}
}

// loadApprovals picks up requests that were waiting before the stream
// started.
func (s *Session) loadApprovals() {
	c := s.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 10*time.Second)
		defer cancel()
		approvals, err := c.Approvals(ctx, "")
		if err != nil {
			return
		}
		s.win.Update(func() {
			if len(approvals) == 0 && len(s.approvals) == 0 {
				return
			}
			s.approvals = approvals
		})
	}()
}

func (s *Session) addApproval(request proto.ApprovalRequest) {
	list := s.approvals
	for _, existing := range list {
		if existing.ID == request.ID {
			return
		}
	}
	next := make([]proto.ApprovalRequest, len(list), len(list)+1)
	copy(next, list)
	s.approvals = append(next, request)
}

func (s *Session) removeApproval(id string) {
	list := s.approvals
	next := make([]proto.ApprovalRequest, 0, len(list))
	for _, request := range list {
		if request.ID != id {
			next = append(next, request)
		}
	}
	s.approvals = next
}

// resolveApproval answers the waiting tool call, closing its dialog now
// and reopening the question if the server refused.
func (s *Session) resolveApproval(id string, allow bool, message string) {
	s.removeApproval(id)
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.ResolveApproval(ctx, id, allow, message)
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			s.loadApprovals()
		}
	})
}

func (s *Session) upsertThread(t proto.Thread) {
	list := s.threads
	next := make([]proto.Thread, len(list), len(list)+1)
	copy(next, list)
	for i := range next {
		if next[i].ID == t.ID {
			next[i] = t
			s.threads = next
			return
		}
	}
	s.threads = append(next, t)
}

func (s *Session) upsertProject(p proto.Project) {
	list := s.projects
	next := make([]proto.Project, len(list), len(list)+1)
	copy(next, list)
	for i := range next {
		if next[i].ID == p.ID {
			next[i] = p
			s.projects = next
			return
		}
	}
	s.projects = append(next, p)
}

// removeProject drops a project and the threads that went with it.
func (s *Session) removeProject(id string) {
	list := s.projects
	next := make([]proto.Project, 0, len(list))
	for _, p := range list {
		if p.ID != id {
			next = append(next, p)
		}
	}
	var keptThreads []proto.Thread
	var dropped []string
	for _, t := range s.threads {
		if t.ProjectID == id {
			dropped = append(dropped, t.ID)
			continue
		}
		keptThreads = append(keptThreads, t)
	}
	s.projects = next
	s.threads = keptThreads
	for _, id := range dropped {
		if s.selected == id {
			s.selected = ""
			s.messages = nil
			s.detail = proto.ThreadDetail{}
			s.running = false
			break
		}
	}
}

func (s *Session) removeThread(id string) {
	list := s.threads
	next := make([]proto.Thread, 0, len(list))
	for _, t := range list {
		if t.ID != id {
			next = append(next, t)
		}
	}
	s.threads = next
	if drafts := s.drafts; drafts[id] != "" {
		next := map[string]string{}
		for k, v := range drafts {
			if k != id {
				next[k] = v
			}
		}
		s.drafts = next
		if raw, err := json.Marshal(next); err == nil {
			_ = s.store.SetItem("drafts", string(raw))
		}
	}
	if pinned := s.pinned; pinned[id] {
		next := map[string]bool{}
		for k, v := range pinned {
			if k != id {
				next[k] = v
			}
		}
		s.pinned = next
		if raw, err := json.Marshal(next); err == nil {
			_ = s.store.SetItem("pinnedThreads", string(raw))
		}
	}
	if drafts := s.attachmentDrafts; len(drafts[id]) > 0 {
		next := map[string][]proto.Attachment{}
		for k, v := range drafts {
			if k != id {
				next[k] = v
			}
		}
		s.attachmentDrafts = next
		if raw, err := json.Marshal(next); err == nil {
			_ = s.store.SetItem("attachmentDrafts", string(raw))
		}
	}
}

func (s *Session) applyMessage(m proto.Message) {
	list := s.messages
	next := make([]proto.Message, len(list), len(list)+1)
	copy(next, list)
	for i := range next {
		if next[i].ID == m.ID {
			next[i] = m
			s.messages = next
			return
		}
	}
	s.messages = append(next, m)
}

func (s *Session) applyDelta(d proto.MessageDelta) {
	if d.MessageID == "" || d.Delta == "" {
		return
	}
	list := s.messages
	next := make([]proto.Message, len(list))
	copy(next, list)
	for i := range next {
		if next[i].ID == d.MessageID {
			next[i].Content += d.Delta
			s.messages = next
			return
		}
	}
}

// call runs an API action off the main thread. The done callback runs on
// the main thread again, through the window's Update.
func (s *Session) call(fn func(ctx context.Context, c *client.Client) error, done func(err error)) {
	c := s.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 30*time.Second)
		defer cancel()
		err := fn(ctx, c)
		if done != nil {
			s.win.Update(func() { done(err) })
		}
	}()
}
