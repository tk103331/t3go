package app

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/proto"
)

// queuedPrompt is a message waiting for the live run to end.
type queuedPrompt struct {
	Text        string
	Attachments []proto.Attachment
	Uncertain   bool
	RequestID   string `json:",omitempty"`
}

// itemTarget is a thread or project a dialog acts on.
type itemTarget struct {
	Kind string // "thread" or i18n.T("project")
	ID   string
	Name string
}

// promptArgsState holds the values typed for a prompt's arguments.
type promptArgsState struct {
	Values [8]string
	Count  int
}

func (s *Session) selectThread(id string) {
	switching := id != s.selected
	if switching {
		if s.draftSave != nil {
			s.draftSave()
			s.draftSave = nil
		}
		s.persistDrafts()
		s.saveAttachmentDraft()
	}
	s.selected = id
	if switching {
		s.terminalBridge.close()
		s.mobileSurface = ""
		s.attachments = append([]proto.Attachment(nil), s.attachmentDrafts[id]...)
		s.fileIndex = nil
		s.fileIndexThread = ""
		s.agentCommands = nil
		s.agentCommandsThread = ""
	}
	s.draft = s.drafts[id]
	for _, t := range s.threads {
		if t.ID == id {
			s.markSeen(t)
			break
		}
	}
	s.messages = nil
	s.detail = proto.ThreadDetail{}
	s.running = false
	s.gitNote = ""
	s.gitPR = ""
	s.branches = nil
	s.filesPath = ""
	s.filesEntries = nil
	s.fileSelected = ""
	s.file = proto.WorkspaceFile{}
	s.pullRequest = proto.PullRequest{}
	s.prDetail = ""
	s.pullRequests = nil
	s.prListDetail = ""
	s.resetSideChat()
	s.sideFor = ""
	s.prView = proto.PullRequestDetail{}
	s.prViewNumber = 0
	s.prNote = ""
	s.graph = nil
	s.workingDiff = ""
	s.workingFiles = nil
	s.terminalSession = proto.TerminalSession{}
	s.terminals = nil

	s.terminalErr = ""
	s.rightErr = ""
	if surface := s.surface; surface != "" {
		s.openSurface(surface)
	}
	s.refreshQueue()
	s.loadDetail(id)
	s.loadBranches(id)
}

func (s *Session) loadDetail(id string) {
	c := s.client()
	if c == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 20*time.Second)
		defer cancel()
		d, err := c.ThreadDetail(ctx, id)
		if err != nil {
			return
		}
		s.win.Update(func() {
			if s.selected != id || s.client() != c {
				return
			}
			s.detail = d
			s.messages = d.Messages
			s.running = d.Thread.Status == proto.ThreadRunning
			s.maybeFlush(id)
			s.newThreadProv = d.Thread.Provider
			s.newThreadModel = d.Thread.Model
			s.newThreadPerm = d.Thread.Permission
		})
	}()
}

func (s *Session) send() {
	id := s.selected
	text := strings.TrimSpace(s.draft)
	if id == "" || text == "" || s.client() == nil {
		return
	}
	attachments := s.attachments
	// TODO(view): dictation (speech recognition) is not available in MyGo yet.
	if s.draftSave != nil {
		s.draftSave()
		s.draftSave = nil
	}
	s.setDraft("")
	s.persistDrafts()
	s.attachments = nil
	s.saveAttachmentDraft()
	s.connErr = ""
	// A prompt sent while the agent works waits its turn here; the next
	// run finishes before the client posts it.
	_, submitting := s.inflight[id]
	if s.running || submitting {
		s.queued[id] = append(s.queued[id], queuedPrompt{Text: text, Attachments: attachments})
		s.refreshQueue()
		return
	}
	s.sendPrompt(id, text, attachments)
}

// sendPrompt posts a prompt to a thread that has no live run.
func (s *Session) sendPrompt(threadID, text string, attachments []proto.Attachment) {
	prompt := queuedPrompt{Text: text, Attachments: attachments}
	if s.idempotentMessages {
		prompt.RequestID = id.New("send")
	}
	s.sendRequest(threadID, prompt)
}

func (s *Session) sendRequest(threadID string, prompt queuedPrompt) {
	reconcile := prompt.Uncertain
	if s.client() == nil || s.transportBlocked {
		s.queued[threadID] = append([]queuedPrompt{prompt}, s.queued[threadID]...)
		s.queuePaused = true
		s.refreshQueue()
		return
	}
	attachments := prompt.Attachments
	ids := make([]string, 0, len(attachments))
	for _, a := range attachments {
		ids = append(ids, a.ID)
	}
	if threadID == s.selected {
		s.running = true
	}
	prompt.Uncertain = true
	s.inflight[threadID] = prompt
	if err := s.persistPendingMessages(); err != nil {
		delete(s.inflight, threadID)
		prompt.Uncertain = reconcile
		s.queued[threadID] = append([]queuedPrompt{prompt}, s.queued[threadID]...)
		s.queuePaused = true
		if threadID == s.selected {
			s.running = false
		}
		s.refreshQueue()
		s.connErr = i18n.T("Could not save the pending message. It has not been sent: ") + err.Error()
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		var err error
		reconcile, err = deliverPrompt(ctx, c, threadID, prompt, ids, s.idempotentMessages)
		return err
	}, func(err error) {
		delete(s.inflight, threadID)
		if err != nil {
			if threadID == s.selected {
				s.running = false
			}
			s.connErr = friendlyError(err)
			// Keep the prompt rather than losing it to a failed request.
			s.queued[threadID] = append([]queuedPrompt{prompt}, s.queued[threadID]...)
			s.queuePaused = true
			s.connErr = i18n.T("Delivery could not be confirmed. Check the thread before editing and resending this message.")
			if client.IsAuthorizationError(err) {
				s.blockTransport(err)
			}
			s.refreshQueue()
		} else {
			if reconcile && threadID == s.selected {
				s.loadDetail(threadID)
			} else {
				s.maybeFlush(threadID)
			}
		}
		s.persistPendingMessages()
	})
}

// deliverPrompt keeps a send recoverable across a dropped response or a
// transient relay failure. The receipt check prevents duplicate runs, and the
// retry reuses the request ID so a request that committed before the response
// was lost returns the existing run.
func deliverPrompt(ctx context.Context, c *client.Client, threadID string, prompt queuedPrompt,
	attachmentIDs []string, idempotent bool) (bool, error) {
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_, err := c.SendWithRequest(sendCtx, threadID, prompt.Text, attachmentIDs, prompt.RequestID)
	cancel()
	if err == nil {
		return false, nil
	}
	if prompt.RequestID == "" || !idempotent || !retryableDeliveryError(err) {
		return false, err
	}
	recovered := true
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 300 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, ctx.Err()
			case <-timer.C:
			}
		}
		receiptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, receiptErr := c.MessageReceipt(receiptCtx, threadID, prompt.RequestID)
		cancel()
		if receiptErr == nil {
			return recovered, nil
		}
		if client.IsAuthorizationError(receiptErr) {
			return false, receiptErr
		}
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err = c.SendWithRequest(sendCtx, threadID, prompt.Text, attachmentIDs, prompt.RequestID)
		cancel()
		if err == nil {
			return recovered, nil
		}
		if !retryableDeliveryError(err) {
			return false, err
		}
	}
	return false, err
}

func retryableDeliveryError(err error) bool {
	if client.IsAuthorizationError(err) {
		return false
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == 429 || apiErr.Status >= 500
	}
	return true
}

// maybeFlush starts the thread's next queued prompt once it is idle and
// on screen.
func (s *Session) maybeFlush(threadID string) {
	if threadID != s.selected || s.running || s.queuePaused || s.transportBlocked {
		return
	}
	queue := s.queued[threadID]
	if len(queue) == 0 || queue[0].Uncertain {
		return
	}
	if _, waiting := s.inflight[threadID]; waiting {
		return
	}
	next := queue[0]
	s.queued[threadID] = queue[1:]
	s.sendPrompt(threadID, next.Text, next.Attachments)
	s.refreshQueue()
}

// refreshQueue shows the open thread's queued prompts.
func (s *Session) refreshQueue() {
	s.persistPendingMessages()
	queue := s.queued[s.selected]
	if len(queue) == 0 {
		s.queue = nil
		return
	}
	next := make([]queuedPrompt, len(queue))
	copy(next, queue)
	s.queue = next
}

// checkQueuedDelivery reads only. A missing receipt is not proof that an
// earlier POST cannot still commit; the original key must be retained.
func (s *Session) checkQueuedDelivery(threadID, requestID string) {
	if requestID == "" || !s.idempotentMessages || s.client() == nil || s.transportBlocked {
		return
	}
	key := threadID + "/" + requestID
	if s.receiptChecks[key] {
		return
	}
	if s.receiptChecks == nil {
		s.receiptChecks = map[string]bool{}
	}
	s.receiptChecks[key] = true
	s.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.MessageReceipt(ctx, threadID, requestID)
		return err
	}, func(err error) {
		delete(s.receiptChecks, key)
		if err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.Status == 404 {
				s.connErr = i18n.T("No receipt yet. Retry the same request to confirm delivery without starting it twice.")
			} else if client.IsAuthorizationError(err) {
				s.blockTransport(err)
			} else {
				s.connErr = friendlyError(err)
			}
			return
		}
		for i, prompt := range s.queued[threadID] {
			if prompt.RequestID == requestID {
				queue := s.queued[threadID]
				s.queued[threadID] = append(queue[:i:i], queue[i+1:]...)
				break
			}
		}
		s.connErr = ""
		s.refreshQueue()
		if threadID == s.selected {
			s.loadDetail(threadID)
		}
	})
}

func (s *Session) retryQueuedRequest(threadID, requestID string) {
	if requestID == "" || !s.idempotentMessages || s.transportBlocked || s.client() == nil {
		return
	}
	if _, waiting := s.inflight[threadID]; waiting {
		return
	}
	for i, prompt := range s.queued[threadID] {
		if prompt.RequestID == requestID && prompt.Uncertain {
			queue := s.queued[threadID]
			s.queued[threadID] = append(queue[:i:i], queue[i+1:]...)
			s.connErr = ""
			s.sendRequest(threadID, prompt)
			s.refreshQueue()
			return
		}
	}
}

// dropQueued removes the queued prompt at i.
func (s *Session) dropQueued(i int) {
	id := s.selected
	queue := s.queued[id]
	if i < 0 || i >= len(queue) {
		return
	}
	removed := queue[i]
	s.queued[id] = append(queue[:i:i], queue[i+1:]...)
	s.refreshQueue()
	if !removed.Uncertain {
		s.discardAttachments(removed.Attachments)
	}
}

// pickAttachments opens the system's file picker for the composer.
func (s *Session) pickAttachments() {
	if s.client() == nil {
		return
	}
	s.connErr = ""
	s.pickFiles(func(paths []string) {
		if len(paths) == 0 {
			return
		}
		s.attachFiles(filesFromPaths(paths))
	})
}

// attachPaths takes files dropped on the composer, as file:// URLs or
// paths.
func (s *Session) attachPaths(uris []string) {
	s.attachFiles(filesFromPaths(droppedPaths(uris)))
}

// attachFiles uploads picked files and shows them above the composer.
func (s *Session) attachFiles(files []pickedFile) {
	c := s.client()
	if c == nil || len(files) == 0 {
		return
	}
	if s.client() != nil {
		s.connErr = ""
	}
	threadID := s.selected
	for _, f := range files {
		f := f
		go func() {
			ctx, cancel := context.WithTimeout(s.lifetime, 60*time.Second)
			defer cancel()
			src, err := f.open()
			if err != nil {
				s.win.Update(func() { s.connErr = i18n.T("Could not read ") + f.Name + ": " + err.Error() })
				return
			}
			attachment, uploadErr := c.Upload(ctx, f.Name, f.MIME, src)
			_ = src.Close()
			if f.Temporary && f.Path != "" {
				_ = os.Remove(f.Path)
			}
			s.win.Update(func() {
				if uploadErr != nil {
					s.connErr = friendlyError(uploadErr)
					return
				}
				if s.selected == threadID {
					s.attachments = append(s.attachments, attachment)
					s.saveAttachmentDraft()
					return
				}
				s.addAttachmentDraft(threadID, attachment)
			})
		}()
	}
}

// pickNewThreadAttachments stages files before the thread exists. The files
// belong to the environment, not to an existing thread, and move onto the new
// thread after creation.
func (s *Session) pickNewThreadAttachments() {
	if s.client() == nil {
		return
	}
	s.connErr = ""
	s.pickFiles(func(paths []string) {
		if len(paths) == 0 {
			return
		}
		s.attachNewThreadFiles(filesFromPaths(paths))
	})
}

func (s *Session) attachNewThreadPaths(uris []string) {
	s.attachNewThreadFiles(filesFromPaths(droppedPaths(uris)))
}

func (s *Session) attachNewThreadFiles(files []pickedFile) {
	c := s.client()
	if c == nil || len(files) == 0 {
		return
	}
	s.connErr = ""
	for _, f := range files {
		f := f
		go func() {
			ctx, cancel := context.WithTimeout(s.lifetime, 60*time.Second)
			defer cancel()
			src, err := f.open()
			if err != nil {
				s.win.Update(func() { s.connErr = i18n.T("Could not read ") + f.Name + ": " + err.Error() })
				return
			}
			attachment, uploadErr := c.Upload(ctx, f.Name, f.MIME, src)
			_ = src.Close()
			if f.Temporary && f.Path != "" {
				_ = os.Remove(f.Path)
			}
			s.win.Update(func() {
				if uploadErr != nil {
					s.connErr = friendlyError(uploadErr)
					return
				}
				s.newThreadFiles = append(s.newThreadFiles, attachment)
			})
		}()
	}
}

func (s *Session) removeNewThreadAttachment(id string) {
	files := s.newThreadFiles
	next := make([]proto.Attachment, 0, len(files))
	for _, attachment := range files {
		if attachment.ID != id {
			next = append(next, attachment)
		}
	}
	s.newThreadFiles = next
	s.discardAttachments([]proto.Attachment{{ID: id}})
}

func (s *Session) saveAttachmentDraftFor(threadID string, attachments []proto.Attachment) {
	drafts := s.attachmentDrafts
	next := make(map[string][]proto.Attachment, len(drafts)+1)
	for id, files := range drafts {
		next[id] = files
	}
	if len(attachments) == 0 {
		delete(next, threadID)
	} else {
		next[threadID] = append([]proto.Attachment(nil), attachments...)
	}
	s.attachmentDrafts = next
	raw, _ := json.Marshal(next)
	if s.store != nil {
		_ = s.store.SetItem("attachmentDrafts", string(raw))
	}
}

// isExpanded reports whether a transcript detail is open, falling back to
// def until the reader opens or closes it.
func (s *Session) isExpanded(key string, def bool) bool {
	if open, ok := s.expanded[key]; ok {
		return open
	}
	return def
}

// setExpanded opens or closes a transcript detail.
func (s *Session) setExpanded(key string, open bool) {
	next := make(map[string]bool, len(s.expanded)+1)
	for k, v := range s.expanded {
		next[k] = v
	}
	next[key] = open
	s.expanded = next
}

// TODO(view): dictation (speech recognition) is not available in MyGo yet;
// the composer's dictation button and the toggleDictation/stopDictation
// entry points of the old client are gone.

// removeAttachment drops a file from the composer and the environment.
func (s *Session) removeAttachment(id string) {
	list := s.attachments
	next := make([]proto.Attachment, 0, len(list))
	for _, attachment := range list {
		if attachment.ID != id {
			next = append(next, attachment)
		}
	}
	s.attachments = next
	s.saveAttachmentDraft()
	s.discardAttachments([]proto.Attachment{{ID: id}})
}

// discardAttachments deletes uploaded files that were never sent.
func (s *Session) discardAttachments(attachments []proto.Attachment) {
	c := s.client()
	if c == nil || len(attachments) == 0 {
		return
	}
	for _, attachment := range attachments {
		id := attachment.ID
		go func() {
			ctx, cancel := context.WithTimeout(s.lifetime, 10*time.Second)
			defer cancel()
			_ = c.DeleteUpload(ctx, id)
		}()
	}
}

func (s *Session) stop() {
	id := s.selected
	if id == "" {
		return
	}
	// Stop ends the turn and what was queued behind it.
	queued := s.queued[id]
	s.queued[id] = nil
	s.refreshQueue()
	for _, prompt := range queued {
		if !prompt.Uncertain {
			s.discardAttachments(prompt.Attachments)
		}
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.Stop(ctx, id)
		return err
	}, func(error) {})
}

// openRuns lists the open thread's runs.
func (s *Session) openRuns() {
	if s.selected == "" {
		return
	}
	s.connErr = ""
	s.showRuns = true
}

func (s *Session) closeRuns() {
	s.showRuns = false
}

// openRunDiff shows what one run changed, from its checkpoint.
func (s *Session) openRunDiff(runID string) {
	id := s.selected
	if id == "" || runID == "" {
		return
	}
	s.closeRuns()
	s.showDiff = true
	s.loadDiff(id, runID)
}

// askRestoreRun confirms reverting the checkout to a run's checkpoint.
func (s *Session) askRestoreRun(runID string) {
	if runID == "" {
		return
	}
	s.closeRuns()
	s.pendingRestore = runID
	s.showRestore = true
}

// restoreRun puts the working tree back to the selected checkpoint.
func (s *Session) restoreRun() {
	id := s.selected
	runID := s.pendingRestore
	if id == "" || runID == "" {
		return
	}
	s.showRestore = false
	s.call(func(ctx context.Context, c *client.Client) error {
		_, _, err := c.RestoreRun(ctx, id, runID)
		return err
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.pendingRestore = ""
		s.loadDetail(id)
		s.loadWorkingDiff(id)
	})
}

// runGitAction commits, pushes, or opens a pull request for the selected
// thread's checkout, then reports what happened in the Changes dialog.
func (s *Session) runGitAction(action proto.GitAction) {
	id := s.selected
	c := s.client()
	if id == "" || c == nil || s.gitBusy {
		return
	}
	in := proto.GitActionInput{Action: action, Message: strings.TrimSpace(s.gitMessage)}
	s.gitBusy = true
	s.gitNote = i18n.T("Working…")
	s.gitPR = ""
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 2*time.Minute)
		defer cancel()
		res, err := c.GitAction(ctx, id, in)
		s.win.Update(func() {
			s.gitBusy = false
			if err != nil {
				s.gitNote = friendlyError(err)
				return
			}
			if d := s.detail; d.Thread.ID == id {
				st := res.Status
				d.Git = &st
				s.detail = d
			}
			if res.CommitSHA != "" {
				s.gitMessage = ""
			}
			var parts []string
			if res.CommitSHA != "" {
				parts = append(parts, i18n.T("Committed ")+shortID(res.CommitSHA))
			}
			if res.NoChanges {
				parts = append(parts, i18n.T("No changes to commit"))
			}
			if res.Pushed {
				parts = append(parts, i18n.T("Pushed ")+res.Branch)
			}
			if res.PRURL != "" {
				s.gitPR = res.PRURL
				if res.PRExisted {
					parts = append(parts, i18n.T("Pull request already open"))
				} else {
					parts = append(parts, i18n.T("Pull request created"))
				}
			}
			if len(parts) == 0 {
				parts = append(parts, "Done")
			}
			s.gitNote = strings.Join(parts, " · ")
			if action == proto.GitCommitPR {
				s.loadPullRequest(id)
			}
		})
	}()
}

func (s *Session) openMobileCommit(newBranch bool) {
	files := s.workingFiles
	selected := make([]string, 0, len(files))
	for _, file := range files {
		selected = append(selected, file.Path)
	}
	s.mobileCommitFiles = selected
	s.mobileCommitNewBranch = newBranch
	s.mobileCommitOpen = true
	if id := s.selected; id != "" {
		s.loadWorkingDiff(id)
	}
}

func (s *Session) toggleMobileCommitFile(path string) {
	files := s.mobileCommitFiles
	for i, item := range files {
		if item == path {
			s.mobileCommitFiles = append(files[:i:i], files[i+1:]...)
			return
		}
	}
	s.mobileCommitFiles = append(files, path)
}

func (s *Session) setAllMobileCommitFiles(selectAll bool) {
	files := s.workingFiles
	selected := make([]string, 0, len(files))
	if selectAll {
		for _, file := range files {
			selected = append(selected, file.Path)
		}
	}
	s.mobileCommitFiles = selected
}

func (s *Session) confirmMobileCommit() {
	id := s.selected
	if id == "" || s.gitBusy {
		return
	}
	branch := strings.TrimSpace(s.branchName)
	if s.mobileCommitNewBranch && branch == "" {
		return
	}
	s.gitBusy = true
	s.gitNote = i18n.T("Working…")
	s.gitPR = ""
	newBranch := s.mobileCommitNewBranch
	files := s.mobileCommitFiles
	message := strings.TrimSpace(s.gitMessage)
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 2*time.Minute)
		defer cancel()
		c := s.client()
		if newBranch {
			if _, err := c.CreateBranch(ctx, id, branch); err != nil {
				s.win.Update(func() {
					s.gitBusy = false
					s.gitNote = friendlyError(err)
				})
				return
			}
		}
		res, err := c.GitAction(ctx, id, proto.GitActionInput{
			Action:  proto.GitCommitPush,
			Message: message,
			Files:   files,
		})
		s.win.Update(func() {
			s.gitBusy = false
			if err != nil {
				s.gitNote = friendlyError(err)
				return
			}
			s.mobileCommitOpen = false
			s.mobileCommitNewBranch = false
			s.branchName = ""
			s.gitMessage = ""
			if d := s.detail; d.Thread.ID == id {
				st := res.Status
				d.Git = &st
				s.detail = d
			}
			var parts []string
			if res.CommitSHA != "" {
				parts = append(parts, i18n.T("Committed ")+shortID(res.CommitSHA))
			}
			if res.NoChanges {
				parts = append(parts, i18n.T("No changes to commit"))
			}
			if res.Pushed {
				parts = append(parts, i18n.T("Pushed ")+res.Branch)
			}
			if len(parts) == 0 {
				parts = append(parts, "Done")
			}
			s.gitNote = strings.Join(parts, " · ")
			s.loadDetail(id)
			s.loadWorkingDiff(id)
		})
	}()
}

// setDraft stores the composer's text for the selected thread and
// remembers it for the next visit.
func (s *Session) setDraft(text string) {
	s.draft = text
	if strings.Contains(text, "@") {
		s.ensureFileIndex()
	}
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		s.ensureAgentCommands()
	}
	// The sidebar's draft marker can wait for a pause; updating it on
	// every key would rebuild the list under the composer.
	if s.draftSave != nil {
		s.draftSave()
	}
	s.draftSave = s.after(1200*time.Millisecond, func() {
		s.persistDrafts()
		s.draftSave = nil
	})
}

// persistDrafts records the selected thread's text and writes every
// thread's draft to the app's storage.
func (s *Session) persistDrafts() {
	id := s.selected
	drafts := s.drafts
	next := make(map[string]string, len(drafts)+1)
	for k, v := range drafts {
		next[k] = v
	}
	if id != "" {
		if text := s.draft; strings.TrimSpace(text) == "" {
			delete(next, id)
		} else {
			next[id] = text
		}
	}
	s.drafts = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("drafts", string(raw))
	}
}

// draftFor is a thread's unsent text when it is not the open one.
func (s *Session) draftFor(id string) string {
	if id == s.selected && (s.manager == nil || s.manager.app == s) {
		return ""
	}
	return s.drafts[id]
}

// threadPinned reports whether a thread is pinned to its project's top.
func (s *Session) threadPinned(id string) bool {
	return s.pinned[id]
}

// togglePin pins or unpins a thread.
func (s *Session) togglePin(t proto.Thread) {
	next := make(map[string]bool, len(s.pinned)+1)
	for k, v := range s.pinned {
		next[k] = v
	}
	if next[t.ID] {
		delete(next, t.ID)
	} else {
		next[t.ID] = true
	}
	s.pinned = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("pinnedThreads", string(raw))
	}
}

// addAttachmentDraft stages a file for a thread that is not open, as
// when an upload finishes after the reader switched away.
func (s *Session) addAttachmentDraft(threadID string, attachment proto.Attachment) {
	if threadID == "" {
		return
	}
	drafts := s.attachmentDrafts
	next := make(map[string][]proto.Attachment, len(drafts)+1)
	for key, value := range drafts {
		next[key] = value
	}
	next[threadID] = append(next[threadID], attachment)
	s.attachmentDrafts = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("attachmentDrafts", string(raw))
	}
}

// saveAttachmentDraft stores the open thread's staged files.
func (s *Session) saveAttachmentDraft() {
	id := s.selected
	if id == "" {
		return
	}
	drafts := s.attachmentDrafts
	next := make(map[string][]proto.Attachment, len(drafts)+1)
	for key, value := range drafts {
		next[key] = value
	}
	if files := s.attachments; len(files) > 0 {
		next[id] = append([]proto.Attachment(nil), files...)
	} else {
		delete(next, id)
	}
	s.attachmentDrafts = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("attachmentDrafts", string(raw))
	}
}

// pasteAttachments takes images and files pasted into the composer.
func (s *Session) pasteAttachments(event pasteEvent) {
	files := make([]pickedFile, 0, len(event.Files))
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
		files = append(files, pickedFile{Name: name, MIME: pasted.MIME, Size: int64(len(data)), Data: data})
	}
	s.attachFiles(files)
	if len(event.URIs) > 0 {
		s.attachPaths(event.URIs)
	}
}

// ensureAgentCommands loads workspace skills and custom commands once.
func (s *Session) ensureAgentCommands() {
	id := s.selected
	if id == "" || s.agentCommandsThread == id || s.agentCommandsBusy {
		return
	}
	c := s.client()
	if c == nil {
		return
	}
	s.agentCommandsBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 20*time.Second)
		defer cancel()
		commands, err := c.AgentCommands(ctx, id)
		s.win.Update(func() {
			s.agentCommandsBusy = false
			if err != nil || s.selected != id {
				return
			}
			s.agentCommands = commands
			s.agentCommandsThread = id
		})
	}()
}

// ensureFileIndex loads the checkout's files for @ mentions once.
func (s *Session) ensureFileIndex() {
	id := s.selected
	if id == "" || s.fileIndexThread == id || s.fileIndexBusy {
		return
	}
	c := s.client()
	if c == nil {
		return
	}
	s.fileIndexBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 30*time.Second)
		defer cancel()
		entries, err := c.FilesRecursive(ctx, id)
		s.win.Update(func() {
			s.fileIndexBusy = false
			if err != nil || s.selected != id {
				return
			}
			s.fileIndex = entries
			s.fileIndexThread = id
		})
	}()
}

// rememberThreads records the threads a snapshot first shows as seen, so
// only later completions mark one unread.
func (s *Session) rememberThreads(threads []proto.Thread) {
	seen := s.seen
	next := make(map[string]time.Time, len(seen)+len(threads))
	for id, at := range seen {
		next[id] = at
	}
	for _, t := range threads {
		if _, ok := next[t.ID]; !ok {
			next[t.ID] = t.UpdatedAt
		}
	}
	s.seen = next
}

// markSeen records that the reader has the thread's latest activity.
func (s *Session) markSeen(t proto.Thread) {
	seen := s.seen
	if at, ok := seen[t.ID]; ok && !t.UpdatedAt.After(at) {
		return
	}
	next := make(map[string]time.Time, len(seen)+1)
	for id, at := range seen {
		next[id] = at
	}
	next[t.ID] = t.UpdatedAt
	s.seen = next
}

// threadUnread reports whether a thread finished work after the reader
// last saw it.
func (s *Session) threadUnread(t proto.Thread) bool {
	if (t.ID == s.selected && (s.manager == nil || s.manager.app == s)) || t.Status == proto.ThreadRunning {
		return false
	}
	at, ok := s.seen[t.ID]
	return ok && t.UpdatedAt.After(at)
}

// projectCollapsed reports whether a project's threads are folded away.
func (s *Session) projectCollapsed(id string) bool {
	return s.sidebarCollapsed[id]
}

// setProjectCollapsed folds a project's threads and remembers the choice.
func (s *Session) setProjectCollapsed(id string, collapsed bool) {
	next := make(map[string]bool, len(s.sidebarCollapsed)+1)
	for k, v := range s.sidebarCollapsed {
		next[k] = v
	}
	next[id] = collapsed
	s.sidebarCollapsed = next
	if raw, err := json.Marshal(next); err == nil {
		_ = s.store.SetItem("sidebarCollapsed", string(raw))
	}
}

// statusSectionCollapsed keeps Working and Projects open, and Settled
// folded until the reader chooses otherwise.
func (s *Session) statusSectionCollapsed(id string) bool {
	collapsedState := s.sidebarCollapsed
	if s.manager != nil {
		collapsedState = s.manager.inboxCollapsed
	}
	if collapsed, ok := collapsedState[id]; ok {
		return collapsed
	}
	return id == "settled"
}

func (s *Session) setStatusSectionCollapsed(id string, collapsed bool) {
	current := s.sidebarCollapsed
	store := s.store
	if s.manager != nil {
		current = s.manager.inboxCollapsed
		store = s.manager.store
	}
	next := make(map[string]bool, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[id] = collapsed
	if s.manager != nil {
		s.manager.inboxCollapsed = next
	} else {
		s.sidebarCollapsed = next
	}
	if raw, err := json.Marshal(next); err == nil {
		_ = store.SetItem("inboxCollapsed", string(raw))
	}
}

// setRightOpen shows or hides the right panel and remembers it.
func (s *Session) setRightOpen(open bool) {
	s.rightOpen = open
	value := "false"
	if open {
		value = "true"
	}
	_ = s.store.SetItem("rightOpen", value)
	if open {
		s.openSurface(s.surface)
	}
}

// openTerminal starts or reattaches the thread's shell.
func (s *Session) openTerminal() {
	id := s.selected
	if id == "" {
		return
	}
	previous := s.terminalSession.ID
	s.terminalBusy = true
	s.terminalErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
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
		s.win.Update(func() {
			s.terminals = sessions
			s.terminalSession = active
			s.terminalBusy = false
		})
		return nil
	}, func(err error) {
		s.terminalBusy = false
		if err != nil {
			s.terminalErr = friendlyError(err)
		}
	})
}

// runProviderCommand runs a provider setup command in the thread's shell,
// or copies it when no thread is open.
func (s *Session) runProviderCommand(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	id := s.selected
	if id == "" {
		s.setClipboard(command)
		s.connErr = i18n.T("Copied “") + command + i18n.T("”. Open a thread to run it in its terminal.")
		return
	}
	s.setRightOpen(true)
	s.openSurface("terminal")
	s.call(func(ctx context.Context, c *client.Client) error {
		session, err := c.EnsureTerminal(ctx, id)
		if err != nil {
			return err
		}
		return c.TerminalInput(ctx, session.ID, command+"\n")
	}, func(err error) {
		if err != nil {
			s.setClipboard(command)
			s.connErr = friendlyError(err) + i18n.T(" — copied “") + command + i18n.T("” instead.")
		}
	})
}

// loadTerminalSessions refreshes the stored sessions without starting
// another shell, so an exited one can offer a restart.
func (s *Session) loadTerminalSessions() {
	id := s.selected
	if id == "" {
		return
	}
	active := s.terminalSession.ID
	s.call(func(ctx context.Context, c *client.Client) error {
		sessions, err := c.Terminals(ctx, id)
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			return nil
		}
		next := sessions[len(sessions)-1]
		for _, session := range sessions {
			if session.ID == active {
				next = session
				break
			}
		}
		s.win.Update(func() {
			if s.selected != id {
				return
			}
			s.terminals = sessions
			s.terminalSession = next
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.terminalErr = friendlyError(err)
		}
	})
}

// newTerminal opens another shell tab for the thread.
func (s *Session) newTerminal() {
	id := s.selected
	if id == "" {
		return
	}
	s.terminalBusy = true
	s.terminalErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		session, err := c.NewTerminal(ctx, id)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			s.terminals = append(s.terminals, session)
			s.terminalSession = session
			s.terminalBusy = false
		})
		return nil
	}, func(err error) {
		s.terminalBusy = false
		if err != nil {
			s.terminalErr = friendlyError(err)
		}
	})
}

// restartTerminal closes the active shell and starts a fresh one in its
// place.
func (s *Session) restartTerminal() {
	id := s.selected
	if id == "" {
		return
	}
	old := s.terminalSession
	if old.ID == "" {
		s.newTerminal()
		return
	}
	s.terminalBusy = true
	s.terminalErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		_ = c.CloseTerminal(ctx, old.ID)
		session, err := c.NewTerminal(ctx, id)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			list := s.terminals
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
			s.terminals = next
			s.terminalSession = session
			s.terminalBusy = false
		})
		return nil
	}, func(err error) {
		s.terminalBusy = false
		if err != nil {
			s.terminalErr = friendlyError(err)
		}
	})
}

// goBrowser opens a URL in the right panel's web view.
func (s *Session) goBrowser(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		s.browserNote = i18n.T("Enter an http or https address.")
		return
	}
	s.browserNote = ""
	s.browserInput = u.String()
	s.browserURL = u.String()
}

// openSurface switches the right panel to a surface, or to the picker
// when name is empty. The panel keeps a tab for every surface the reader
// opened.
func (s *Session) openSurface(name string) {
	if s.surface == "terminal" && name != "terminal" {
		s.terminalBridge.close()
	}
	if name == "" {
		// The picker: the surfaces this thread offers. The reader picks
		// one in the panel's own menu; the tab strip shows what is open.
		return
	}
	s.surface = name
	opened := false
	for _, tab := range s.tabs {
		if tab == name {
			opened = true
		}
	}
	if !opened {
		s.tabs = append(s.tabs, name)
	}
	s.rightErr = ""
	switch name {
	case "files":
		s.loadFiles(s.filesPath)
	case "diff":
		if id := s.selected; id != "" {
			s.loadWorkingDiff(id)
		}
	case "pr":
		s.loadPullRequest(s.selected)
	case "graph":
		s.loadGraph(s.selected)
	case "terminal":
		s.openTerminal()
	case "linked":
		s.loadPullRequests(s.selected)
	case "preview":
		s.loadPreviewServers()
	case "device":
		s.loadDevices()
	case "chat":
		s.openSideChat()
	}
}

// closeSurface closes a tab and moves to the one beside it.
func (s *Session) closeSurface(name string) {
	if s.surface != name {
		return
	}
	if name == "terminal" {
		s.terminalBridge.close()
	}
	next := make([]string, 0, len(s.tabs))
	for _, tab := range s.tabs {
		if tab != name {
			next = append(next, tab)
		}
	}
	s.tabs = next
	s.surface = ""
	for _, tab := range s.tabs {
		s.surface = tab
	}
	if s.surface != "" {
		s.openSurface(s.surface)
	}
}

func (s *Session) closeMobileSurface() {
	if s.surface == "terminal" {
		s.terminalBridge.close()
	}
	s.mobileSurface = ""
}

// setRightWidth resizes and remembers the surfaces panel.
func (s *Session) setRightWidth(width float32) {
	minWidth, maxWidth := float32(300), float32(680)
	// TODO(view): the view clamps maxWidth to the window's width.
	if width < minWidth {
		width = minWidth
	}
	if width > maxWidth {
		width = maxWidth
	}
	s.rightWidth = width
	_ = s.store.SetItem("rightWidth", strconv.FormatFloat(float64(width), 'f', 1, 64))
}

// loadFiles lists a directory of the selected thread's checkout.
func (s *Session) loadFiles(path string) {
	id := s.selected
	if id == "" {
		return
	}
	s.rightBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		entries, err := c.Files(ctx, id, path)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			s.rightBusy = false
			s.filesPath = path
			s.filesEntries = entries
			s.fileSelected = ""
			s.file = proto.WorkspaceFile{}
			s.rightErr = ""
		})
		return nil
	}, func(err error) {
		s.rightBusy = false
		if err != nil {
			s.rightErr = friendlyError(err)
		}
	})
}

// openFile reads one file into the right panel's viewer.
func (s *Session) openFile(path string) {
	id := s.selected
	if id == "" {
		return
	}
	s.rightBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		file, err := c.File(ctx, id, path)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			s.rightBusy = false
			s.fileSelected = path
			s.file = file
			s.rightErr = ""
		})
		return nil
	}, func(err error) {
		s.rightBusy = false
		if err != nil {
			s.rightErr = friendlyError(err)
		}
	})
}

// uploadURL is where an attachment's image can be read.
func (s *Session) uploadURL(id string) string {
	c := s.client()
	if c == nil {
		return ""
	}
	return c.UploadURL(id)
}

// rawFileURL is where the panel loads an image or opens one outside the
// app.
func (s *Session) rawFileURL(path string) string {
	c := s.client()
	if c == nil {
		return ""
	}
	return c.RawFileURL(s.selected, path)
}

// loadGraph reads the repository's commits for the graph surface.
func (s *Session) loadGraph(id string) {
	if id == "" {
		return
	}
	s.rightBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		commits, err := c.Graph(ctx, id)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			s.rightBusy = false
			s.graph = commits
			s.rightErr = ""
		})
		return nil
	}, func(err error) {
		s.rightBusy = false
		if err != nil {
			s.rightErr = friendlyError(err)
		}
	})
}

// loadPullRequest reads the branch's open pull request.
func (s *Session) loadPullRequest(id string) {
	if id == "" {
		return
	}
	s.rightBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		pr, detail, err := c.PullRequest(ctx, id)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			s.rightBusy = false
			s.pullRequest = pr
			s.prDetail = detail
		})
		return nil
	}, func(err error) {
		s.rightBusy = false
		if err != nil {
			s.rightErr = friendlyError(err)
		}
	})
}

// loadWorkingDiff reads the checkout's changes against HEAD.
func (s *Session) loadWorkingDiff(threadID string) {
	s.call(func(ctx context.Context, c *client.Client) error {
		diff, err := c.WorkingDiff(ctx, threadID)
		if err != nil {
			return err
		}
		s.win.Update(func() {
			if s.selected != threadID {
				return
			}
			s.workingDiff = diff.Patch
			s.workingFiles = diff.Files
			s.rightErr = ""
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.rightErr = friendlyError(err)
		}
	})
}

// stagePath stages or discards one changed path.
func (s *Session) stagePath(path string, stage bool) {
	id := s.selected
	if id == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		var err error
		if stage {
			err = c.StageFiles(ctx, id, []string{path})
		} else {
			err = c.DiscardFiles(ctx, id, []string{path})
		}
		return err
	}, func(err error) {
		if err != nil {
			s.rightErr = friendlyError(err)
			return
		}
		s.loadWorkingDiff(id)
		s.loadDetail(id)
	})
}

// discardWorkingFiles takes the thread's working changes back, file by
// file, the way the Changes surface discards them.
func (s *Session) discardWorkingFiles() {
	id := s.selected
	if id == "" {
		return
	}
	paths := make([]string, 0, len(s.workingFiles))
	for _, file := range s.workingFiles {
		paths = append(paths, file.Path)
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.DiscardFiles(ctx, id, paths)
	}, func(err error) {
		if err != nil {
			s.rightErr = friendlyError(err)
			return
		}
		s.loadWorkingDiff(id)
		s.loadDetail(id)
	})
}
