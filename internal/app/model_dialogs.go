package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// pickAgentCommand inserts a skill or custom command, asking for a
// prompt's arguments first.
func (s *Session) pickAgentCommand(command proto.AgentCommand) {
	if command.Kind != "prompt" || len(command.Arguments) == 0 {
		s.setDraft("/" + command.Name + " ")
		return
	}
	s.promptName = command.Name
	s.promptArgsDefs = command.Arguments
	count := len(command.Arguments)
	if count > len(promptArgsState{}.Values) {
		count = len(promptArgsState{}.Values)
	}
	s.promptArgValues = promptArgsState{Count: count}
	s.showPromptArgs = true
}

// submitPromptArgs inserts the prompt with the arguments that were typed.
func (s *Session) submitPromptArgs() {
	state := s.promptArgValues
	parts := []string{"/" + s.promptName}
	for index := 0; index < state.Count && index < len(state.Values); index++ {
		value := strings.TrimSpace(state.Values[index])
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	s.setDraft(strings.Join(parts, " "))
	s.showPromptArgs = false
}

// openNewBranch asks for a branch name.
func (s *Session) openNewBranch() {
	s.branchName = ""
	s.showBranch = true
}

// createBranch makes and checks out a branch in the thread's checkout.
func (s *Session) createBranch() {
	id := s.selected
	name := strings.TrimSpace(s.branchName)
	if id == "" || name == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		branches, err := c.CreateBranch(ctx, id, name)
		if err != nil {
			return err
		}
		s.post(func() {
			s.branches = branches
			s.showBranch = false
			s.loadDetail(id)
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
		}
	})
}

// pullBranch fast-forwards the thread's checkout.
func (s *Session) pullBranch() {
	id := s.selected
	if id == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		_, _, err := c.PullBranch(ctx, id)
		return err
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.loadDetail(id)
		s.loadBranches(id)
		s.loadWorkingDiff(id)
	})
}

// openPullRequest shows one pull request's detail.
func (s *Session) openPullRequest(number int) {
	id := s.selected
	if id == "" || number <= 0 {
		return
	}
	s.prViewNumber = number
	s.prViewBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		detail, err := c.PullRequestDetail(ctx, id, number)
		if err != nil {
			return err
		}
		s.post(func() {
			s.prViewBusy = false
			s.prView = detail
		})
		return nil
	}, func(err error) {
		s.prViewBusy = false
		if err != nil {
			s.connErr = friendlyError(err)
		}
	})
}

// closePullRequest returns to the pull request list.
func (s *Session) closePullRequest() {
	s.prViewNumber = 0
	s.prView = proto.PullRequestDetail{}
}

// submitPRReview posts the note as an approval, comment or change request.
func (s *Session) submitPRReview(verdict string) {
	id := s.selected
	number := s.prViewNumber
	note := strings.TrimSpace(s.prNote)
	if id == "" || number <= 0 {
		return
	}
	if note == "" && verdict != "approve" {
		s.connErr = i18n.T("Write a note before submitting this review.")
		return
	}
	s.prViewBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.ReviewPullRequest(ctx, id, number, verdict, note)
	}, func(err error) {
		s.prViewBusy = false
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.prNote = ""
		s.openPullRequest(number)
		s.loadPullRequests(id)
	})
}

// submitPRComment posts the note as a conversation comment.
func (s *Session) submitPRComment() {
	id := s.selected
	number := s.prViewNumber
	note := strings.TrimSpace(s.prNote)
	if id == "" || number <= 0 || note == "" {
		return
	}
	s.prViewBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.CommentPullRequest(ctx, id, number, note)
	}, func(err error) {
		s.prViewBusy = false
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.prNote = ""
		s.openPullRequest(number)
		s.loadPullRequests(id)
	})
}

// mergePullRequest merges the open pull request.
func (s *Session) mergePullRequest(method string) {
	id := s.selected
	number := s.prViewNumber
	if id == "" || number <= 0 {
		return
	}
	s.prViewBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.MergePullRequest(ctx, id, number, method, s.prDeleteBranch)
	}, func(err error) {
		s.prViewBusy = false
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.openPullRequest(number)
		s.loadPullRequests(id)
	})
}

// changePullRequestState readies, drafts, closes or reopens the PR.
func (s *Session) changePullRequestState(action string) {
	id := s.selected
	number := s.prViewNumber
	if id == "" || number <= 0 {
		return
	}
	s.prViewBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.PullRequestAction(ctx, id, number, action)
	}, func(err error) {
		s.prViewBusy = false
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.openPullRequest(number)
		s.loadPullRequests(id)
	})
}

// loadUsage reads the token and cost usage of the last days.
func (s *Session) loadUsage(days int) {
	if days <= 0 {
		days = 30
	}
	s.usageDays = days
	s.usageSeq++
	seq := s.usageSeq
	if !s.settingsConnectionReady() {
		s.usageBusy = false
		s.usageErr = i18n.T("Connect this environment before loading its usage.")
		return
	}
	s.usageBusy = true
	s.usageErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		report, err := c.Usage(ctx, days)
		if err != nil {
			return err
		}
		s.post(func() {
			if seq != s.usageSeq {
				return
			}
			s.usageReport = report
			s.usageBusy = false
		})
		return nil
	}, func(err error) {
		if seq != s.usageSeq {
			return
		}
		s.usageBusy = false
		if err != nil {
			s.usageErr = friendlyError(err)
		}
	})
}

// loadPreviewServers lists local development servers to open.
func (s *Session) loadPreviewServers() {
	s.previewBusy = true
	s.previewErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		servers, detail, err := c.PreviewServers(ctx)
		if err != nil {
			return err
		}
		s.post(func() {
			s.previewServers = servers
			s.previewDetail = detail
			s.previewBusy = false
		})
		return nil
	}, func(err error) {
		s.previewBusy = false
		if err != nil {
			s.previewErr = friendlyError(err)
		}
	})
}

// openPreviewServer opens a local server in the browser surface.
func (s *Session) openPreviewServer(url string) {
	if url == "" {
		return
	}
	s.browserURL = url
	s.browserInput = url
	s.browserNote = ""
	s.openSurface("browser")
}

// loadDevices lists the environment's simulators.
func (s *Session) loadDevices() {
	s.deviceErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		devices, detail, err := c.Devices(ctx)
		if err != nil {
			return err
		}
		s.post(func() {
			s.devices = devices
			s.deviceDetail = detail
			selected := s.deviceSelected
			found := false
			for _, device := range devices {
				if device.ID == selected {
					found = true
				}
			}
			if !found && len(devices) > 0 {
				next := devices[0]
				for _, device := range devices {
					if device.State == "booted" {
						next = device
						break
					}
				}
				s.deviceSelected = next.ID
			}
			if len(devices) > 0 {
				s.loadDeviceShot()
				s.scheduleDeviceShot()
			}
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.deviceErr = friendlyError(err)
		}
	})
}

// selectDevice shows one simulator's screen.
func (s *Session) selectDevice(device proto.Device) {
	s.deviceSelected = device.ID
	s.deviceErr = ""
	s.deviceImage = nil
	if strings.ToLower(device.State) == "booted" {
		s.loadDeviceShot()
	}
	s.scheduleDeviceShot()
}

// loadDeviceShot reads the selected simulator's screen.
func (s *Session) loadDeviceShot() {
	id := s.deviceSelected
	if id == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		shot, err := c.DeviceScreenshot(ctx, id)
		if err != nil {
			return err
		}
		s.post(func() {
			if s.deviceSelected == id {
				s.deviceImage = shot
				s.deviceErr = ""
			}
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.deviceErr = friendlyError(err)
		}
	})
}

// bootDevice starts a simulator and refreshes its screen.
func (s *Session) bootDevice(id string) {
	if id == "" {
		return
	}
	s.deviceBusy = true
	s.deviceErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.BootDevice(ctx, id)
	}, func(err error) {
		s.deviceBusy = false
		if err != nil {
			s.deviceErr = friendlyError(err)
			return
		}
		s.deviceSelected = id
		s.loadDevices()
	})
}

// shutdownDevice stops a simulator.
func (s *Session) shutdownDevice(id string) {
	if id == "" {
		return
	}
	s.deviceBusy = true
	s.deviceErr = ""
	s.call(func(ctx context.Context, c *client.Client) error {
		return c.ShutdownDevice(ctx, id)
	}, func(err error) {
		s.deviceBusy = false
		if err != nil {
			s.deviceErr = friendlyError(err)
			return
		}
		s.loadDevices()
	})
}

// refreshDeviceShot reloads a booted simulator's screen.
func (s *Session) refreshDeviceShot() {
	s.loadDeviceShot()
	s.scheduleDeviceShot()
}

// setDeviceLive turns the automatic screen refresh on or off.
func (s *Session) setDeviceLive(live bool) {
	s.deviceLive = live
	if s.deviceTimer != nil {
		s.deviceTimer()
		s.deviceTimer = nil
	}
	if live {
		s.loadDeviceShot()
		s.scheduleDeviceShot()
	}
}

// scheduleDeviceShot reloads a booted simulator's screen every few seconds
// while the surface is open.
func (s *Session) scheduleDeviceShot() {
	if s.deviceTimer != nil {
		s.deviceTimer()
		s.deviceTimer = nil
	}
	if !s.deviceLive {
		return
	}
	s.deviceTimer = s.after(2*time.Second, func() {
		s.deviceTimer = nil
		if s.surface != "device" || !s.rightOpen {
			return
		}
		id := s.deviceSelected
		booted := false
		for _, device := range s.devices {
			if device.ID == id && device.State == "booted" {
				booted = true
			}
		}
		if id == "" || !booted {
			return
		}
		s.loadDeviceShot()
		s.scheduleDeviceShot()
	})
}

// loadPullRequests lists the branch's pull requests.
func (s *Session) loadPullRequests(threadID string) {
	if threadID == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		pulls, detail, err := c.PullRequests(ctx, threadID)
		if err != nil {
			return err
		}
		s.post(func() {
			if s.selected != threadID {
				return
			}
			s.pullRequests = pulls
			s.prListDetail = detail
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
		}
	})
}

// loadBranches fills the Changes dialog's branch picker.
func (s *Session) loadBranches(threadID string) {
	s.call(func(ctx context.Context, c *client.Client) error {
		branches, err := c.Branches(ctx, threadID)
		if err != nil {
			return err
		}
		s.post(func() {
			if s.selected == threadID {
				s.branches = branches
			}
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.gitNote = friendlyError(err)
		}
	})
}

// switchBranch checks out another branch in the thread's checkout.
func (s *Session) switchBranch(name string) {
	id := s.selected
	if id == "" || s.gitBusy {
		return
	}
	s.gitBusy = true
	s.gitNote = i18n.T("Switching branch…")
	s.call(func(ctx context.Context, c *client.Client) error {
		branches, err := c.SwitchBranch(ctx, id, name)
		if err != nil {
			return err
		}
		s.post(func() {
			s.gitBusy = false
			s.branches = branches
			s.gitNote = i18n.T("On ") + name
			s.loadDetail(id)
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.gitBusy = false
			s.gitNote = friendlyError(err)
		}
	})
}

// setThreadModel changes the open thread's model, and its provider when
// the model belongs to another one.
func (s *Session) setThreadModel(model proto.Model) {
	s.updateThreadSettings(map[string]any{"model": model.ID, "provider": model.Provider})
}

// planReady reports a finished turn while plan mode is on.
func (s *Session) planReady() bool {
	detail := s.detail
	if !detail.Thread.Plan || s.running {
		return false
	}
	for i := len(detail.Runs) - 1; i >= 0; i-- {
		if detail.Runs[i].Status == proto.RunCompleted {
			return true
		}
	}
	return false
}

// approvePlan leaves plan mode and stages the implementation prompt.
func (s *Session) approvePlan() {
	s.setThreadPlan(false)
	s.setDraft(i18n.T("Implement the plan."))
}

// setThreadPlan turns the open thread's plan-first mode on or off.
func (s *Session) setThreadPlan(plan bool) {
	s.updateThreadSettings(map[string]any{"plan": plan})
}

// setThreadPermission changes the open thread's permission mode.
func (s *Session) setThreadPermission(permission proto.PermissionMode) {
	s.updateThreadSettings(map[string]any{"permission": string(permission)})
}

func (s *Session) updateThreadSettings(patch map[string]any) {
	id := s.selected
	if id == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		thread, err := c.UpdateThread(ctx, id, patch)
		if err != nil {
			return err
		}
		s.post(func() {
			s.upsertThread(thread)
			detail := s.detail
			if detail.Thread.ID == id {
				detail.Thread = thread
				s.detail = detail
			}
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
		}
	})
}

// retryRun reruns one turn in place.
func (s *Session) retryRun(runID string) {
	id := s.selected
	if id == "" || s.running {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.Retry(ctx, id, runID)
		return err
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.running = true
	})
}

// branchFromMessage copies the transcript up to a message into a new
// thread.
func (s *Session) branchFromMessage(m proto.Message) {
	s.forkThread(m.ID, "")
}

// openEditMessage edits a user message by branching before it.
func (s *Session) openEditMessage(m proto.Message) {
	s.editingMessage = m
	s.editDraft = m.Content
}

func (s *Session) cancelEditMessage() {
	s.editingMessage = proto.Message{}
	s.editDraft = ""
}

func (s *Session) confirmEditMessage() {
	message := s.editingMessage
	content := strings.TrimSpace(s.editDraft)
	if message.ID == "" || content == "" {
		return
	}
	s.cancelEditMessage()
	s.forkThread(message.ID, content)
}

// forkThread runs a fork on the server and selects the new thread.
func (s *Session) forkThread(messageID, content string) {
	id := s.selected
	if id == "" {
		return
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		thread, err := c.Fork(ctx, id, messageID, content)
		if err != nil {
			return err
		}
		s.post(func() {
			s.upsertThread(thread)
			s.selectThread(thread.ID)
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
		}
	})
}

func (s *Session) openDiff() {
	id := s.selected
	if id == "" {
		return
	}
	s.showDiff = true
	s.loadDiff(id, "")
}

func (s *Session) loadDiff(threadID, runID string) {
	s.diffText = i18n.T("Loading diff…")
	s.loadBranches(threadID)
	s.call(func(ctx context.Context, c *client.Client) error {
		diff, err := c.Diff(ctx, threadID, runID)
		if err != nil {
			return err
		}
		s.post(func() {
			if diff == "" {
				diff = i18n.T("This run made no changes.")
			}
			s.diffText = diff
		})
		return nil
	}, func(err error) {
		if err != nil {
			s.diffText = friendlyError(err)
		}
	})
}

func (s *Session) createProject() {
	if s.projectBusy {
		return
	}
	path := strings.TrimSpace(s.newProjectPath)
	name := strings.TrimSpace(s.newProjectName)
	if path == "" {
		s.connErr = i18n.T("Choose the project's directory.")
		return
	}
	// On phones and touch clients, the project path belongs to the
	// connected environment, not the device running this UI. The host
	// browser can select those paths, so let the server validate them.
	if s.manager == nil && !s.newProjectRemote {
		if info, err := os.Stat(path); err != nil {
			s.connErr = i18n.T("That directory does not exist.")
			return
		} else if !info.IsDir() {
			s.connErr = i18n.T("That path is not a directory.")
			return
		}
	}
	if name == "" {
		name = filepath.Base(filepath.Clean(path))
		if name == "." || name == string(filepath.Separator) {
			name = "Project"
		}
	}
	target := s.projectTarget()
	if target == nil {
		s.connErr = i18n.T("Choose an environment.")
		return
	}
	for _, project := range target.projects {
		if filepath.Clean(project.Path) == filepath.Clean(path) {
			s.connErr = i18n.T("That directory is already a project.")
			return
		}
	}
	s.connErr = ""
	s.projectBusy = true
	var created proto.Project
	s.callProjectTarget(func(ctx context.Context, c *client.Client) error {
		var err error
		created, err = c.CreateProject(ctx, name, path)
		return err
	}, func(err error) {
		s.projectBusy = false
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.showNewProject = false
		s.newProjectName = ""
		s.newProjectPath = ""
		s.newProjectRemote = false
		target.reload()
		if created.ID != "" {
			if s.showNewThread {
				s.selectThreadEnvironment(target.profile.ID)
				if session := s.newThreadTarget(); session != nil {
					session.selectNewThreadProject(created.ID)
				}
			} else {
				target.selectNewThreadProject(created.ID)
			}
		}
	})
}

func (s *Session) browseProject() {
	if s.manager != nil || s.narrow {
		s.openDirs()
		return
	}
	s.pickDirectories(func(paths []string) {
		if len(paths) == 0 {
			return
		}
		if path := strings.TrimSpace(paths[0]); path != "" {
			s.newProjectPath = path
			s.newProjectRemote = false
			if s.newProjectName == "" {
				s.newProjectName = folderName(path)
			}
		}
	})
}

// openDirs browses the connected environment's folders, which a phone
// cannot see for itself.
func (s *Session) openDirs() {
	path := strings.TrimSpace(s.newProjectPath)
	s.showDirs = true
	s.loadDirs(path)
}

// closeDirs hides the environment's folder browser.
func (s *Session) closeDirs() {
	s.dirsSeq++
	s.dirsBusy = false
	s.showDirs = false
}

func (s *Session) openNewDir() {
	s.newDirName = ""
	s.newDirErr = ""
	s.showNewDir = true
}

func (s *Session) closeNewDir() {
	s.newDirSeq++
	s.newDirBusy = false
	s.showNewDir = false
	s.newDirName = ""
	s.newDirErr = ""
}

func (s *Session) createDir() {
	if s.newDirBusy {
		return
	}
	name := strings.TrimSpace(s.newDirName)
	if name == "" {
		s.newDirErr = i18n.T("Enter a folder name.")
		return
	}
	s.newDirErr = ""
	s.newDirBusy = true
	parent := s.dirs.Path
	s.newDirSeq++
	seq := s.newDirSeq
	var listing proto.HostDirs
	s.callProjectTarget(func(ctx context.Context, c *client.Client) error {
		var err error
		listing, err = c.CreateDir(ctx, parent, name)
		return err
	}, func(err error) {
		if seq != s.newDirSeq {
			return
		}
		s.newDirBusy = false
		if err != nil {
			s.newDirErr = friendlyError(err)
			return
		}
		s.dirs = listing
		s.closeNewDir()
	})
}

// loadDirs reads one folder on the environment's machine.
func (s *Session) loadDirs(path string) {
	s.dirsSeq++
	seq := s.dirsSeq
	s.dirsErr = ""
	s.dirsBusy = true
	var listing proto.HostDirs
	s.callProjectTarget(func(ctx context.Context, c *client.Client) error {
		var err error
		listing, err = c.Dirs(ctx, path)
		return err
	}, func(err error) {
		if seq != s.dirsSeq {
			return
		}
		s.dirsBusy = false
		if err != nil {
			s.dirs = proto.HostDirs{}
			s.dirsErr = friendlyError(err)
			return
		}
		s.dirs = listing
	})
}

// useDirs makes the folder on screen the new project's path.
func (s *Session) useDirs() {
	path := strings.TrimSpace(s.dirs.Path)
	if path == "" {
		return
	}
	s.newProjectPath = path
	s.newProjectRemote = true
	if s.newProjectName == "" {
		s.newProjectName = folderName(path)
	}
	s.showDirs = false
}

func (s *Session) createThread() {
	s.createThreadThen(nil)
}

func (s *Session) createThreadThen(after func(proto.Thread)) {
	if s.threadBusy {
		return
	}
	projectID := s.newThreadProj
	if projectID == "" {
		projectID = s.currentProject()
	}
	if projectID == "" {
		s.connErr = i18n.T("Add a project first.")
		return
	}
	provider := s.newThreadProv
	model := s.newThreadModel
	if provider == "" || model == "" {
		s.connErr = i18n.T("Choose a provider and a model.")
		return
	}
	installed := false
	for _, p := range s.providers {
		if p.ID == provider {
			installed = p.Installed
		}
	}
	if !installed {
		s.connErr = i18n.T("Install ") + provider + i18n.T(" before creating a thread with it.")
		return
	}
	worktree := s.newThreadWT
	existingWorktree := strings.TrimSpace(s.newThreadExistingWT)
	branch := ""
	if !worktree {
		branch = strings.TrimSpace(s.newThreadBase)
	}
	in := client.CreateThreadInput{
		ProjectID:        projectID,
		Title:            strings.TrimSpace(s.newThreadTitle),
		Provider:         s.newThreadProv,
		Model:            s.newThreadModel,
		Permission:       s.newThreadPerm,
		Branch:           branch,
		Worktree:         worktree,
		BaseRef:          strings.TrimSpace(s.newThreadBase),
		ExistingWorktree: existingWorktree,
	}
	var created proto.Thread
	s.threadBusy = true
	s.call(func(ctx context.Context, c *client.Client) error {
		t, err := c.CreateThread(ctx, in)
		created = t
		return err
	}, func(err error) {
		s.threadBusy = false
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		s.showNewThread = false
		s.newThreadTitle = ""
		s.selectNewThreadProject(projectID)
		s.reload()
		if created.ID != "" {
			if after != nil {
				after(created)
			} else {
				s.selectThread(created.ID)
			}
		}
	})
}

// currentProject is the project of the open thread, or the first one.
func (s *Session) currentProject() string {
	selected := s.selected
	for _, t := range s.threads {
		if t.ID == selected {
			return t.ProjectID
		}
	}
	if projects := s.projects; len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

// preferredNewThreadProject returns the last project explicitly selected for
// a new thread, provided it still belongs to this environment.
func (s *Session) preferredNewThreadProject() string {
	if id, ok := s.store.GetItem("lastThreadProject"); ok {
		id = strings.TrimSpace(id)
		for _, project := range s.projects {
			if project.ID == id {
				return id
			}
		}
	}
	return s.currentProject()
}

func (s *Session) selectNewThreadProject(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	changed := s.newThreadProj != id
	if changed {
		s.newThreadBase = ""
		s.newThreadWT = false
		s.newThreadExistingWT = ""
	}
	s.newThreadProj = id
	_ = s.store.SetItem("lastThreadProject", id)
	s.loadNewThreadBranches(id)
}

func (s *Session) loadNewThreadBranches(projectID string) {
	s.newThreadBranchesSeq++
	seq := s.newThreadBranchesSeq
	s.newThreadBranches = nil
	s.newThreadBranchesErr = ""
	var project proto.Project
	found := false
	for _, candidate := range s.projects {
		if candidate.ID == projectID {
			project = candidate
			found = true
			break
		}
	}
	if !found || !project.IsGit {
		s.newThreadBranchesBusy = false
		return
	}
	s.newThreadBranchesBusy = true
	var branches []proto.Branch
	s.call(func(ctx context.Context, c *client.Client) error {
		var err error
		branches, err = c.ProjectBranches(ctx, project.ID)
		return err
	}, func(err error) {
		if seq != s.newThreadBranchesSeq || s.newThreadProj != projectID {
			return
		}
		s.newThreadBranchesBusy = false
		if err != nil {
			s.newThreadBranchesErr = friendlyError(err)
			return
		}
		s.newThreadBranches = branches
		selected := s.newThreadBase
		available := false
		for _, branch := range branches {
			if branch.Name == selected {
				available = true
				break
			}
		}
		if available {
			return
		}
		for _, branch := range branches {
			if branch.Current {
				s.newThreadBase = branch.Name
				return
			}
		}
		for _, branch := range branches {
			if branch.Default {
				s.newThreadBase = branch.Name
				return
			}
		}
		if len(branches) > 0 {
			s.newThreadBase = branches[0].Name
			return
		}
		s.newThreadBase = ""
	})
}

func (s *Session) openNewProject() {
	s.connErr = ""
	id := s.profile.ID
	if s.manager != nil && s.selected == "" && s.manager.defaultID != "" {
		id = s.manager.defaultID
	}
	s.selectProjectEnvironment(id)
	s.newProjectRemote = s.manager != nil
	s.showNewProject = true
}

func (s *Session) openNewProjectForThread() {
	target := s.newThreadTarget()
	if target == nil {
		s.openNewProject()
		return
	}
	s.connErr = ""
	s.selectProjectEnvironment(target.profile.ID)
	s.newProjectRemote = s.manager != nil
	s.showNewProject = true
}

// openNewThreadIn starts a thread in a named project, for a project's
// own action.
func (s *Session) openNewThreadIn(projectID string) {
	s.openNewThread()
	if s.narrow {
		s.newThreadEnv = s.profile.ID
		s.newThreadVisited = map[*Session]bool{s: true}
	}
	if projectID != "" {
		s.selectNewThreadProject(projectID)
	}
}

func (s *Session) openNewThread() {
	if s.mobileThreadSearch {
		s.closeMobileThreadSearch()
	}
	s.prepareNewThread()
	if s.narrow {
		target := s
		if s.manager != nil && s.selected == "" {
			if session := s.manager.sessions[s.manager.defaultID]; session != nil {
				target = session
			}
		}
		if target != s {
			target.prepareNewThread()
		}
		s.newThreadEnv = target.profile.ID
		s.newThreadVisited = map[*Session]bool{target: true}
	}
	s.showNewThread = true
}

func (s *Session) prepareNewThread() {
	s.connErr = ""
	s.newThreadTitle = ""
	s.newThreadBase = ""
	s.newThreadWT = false
	s.newThreadExistingWT = ""
	s.selectNewThreadProject(s.preferredNewThreadProject())
	settings := s.settings
	provider := settings.DefaultProvider
	model := settings.DefaultModel
	if provider == "" {
		for _, p := range s.providers {
			if p.Installed {
				provider = p.ID
				break
			}
		}
	}
	if model == "" {
		for _, m := range s.allModels() {
			if m.Provider == provider {
				model = m.ID
				break
			}
		}
	}
	s.newThreadProv = provider
	s.newThreadModel = model
	perm := proto.PermissionMode(settings.DefaultPermission)
	if !perm.Valid() {
		perm = proto.PermissionFullAccess
	}
	s.newThreadPerm = perm
}

// setSidebarOpen shows or hides the sidebar, and remembers it.
func (s *Session) setSidebarOpen(open bool) {
	s.sidebarOpen = open
	_ = s.store.SetItem("sidebarOpen", strconv.FormatBool(open))
}

// setSidebarWidth resizes the sidebar, and remembers the size.
func (s *Session) setSidebarWidth(w float32) {
	s.sidebarWidth = w
	_ = s.store.SetItem("sidebarWidth", strconv.FormatFloat(float64(w), 'f', 1, 64))
}

// openPalette searches the environment's threads and commands.
func (s *Session) openPalette() {
	s.connErr = ""
	s.searchHits = nil
	s.searchErr = ""
	s.showPalette = true
}

func (s *Session) closePalette() {
	s.searchSeq++
	if s.searchTimer != nil {
		s.searchTimer()
		s.searchTimer = nil
	}
	s.showPalette = false
}

// searchMessages debounces a message search for the palette.
func (s *Session) searchMessages(query string) {
	if s.searchTimer != nil {
		s.searchTimer()
		s.searchTimer = nil
	}
	query = strings.TrimSpace(query)
	s.searchSeq++
	s.searchErr = ""
	if len(query) < 2 {
		s.searchHits = nil
		s.searchBusy = false
		return
	}
	seq := s.searchSeq
	s.searchTimer = s.after(250*time.Millisecond, func() {
		s.searchTimer = nil
		s.searchBusy = true
		s.call(func(ctx context.Context, c *client.Client) error {
			hits, err := c.SearchMessages(ctx, query, 20)
			if err != nil {
				return err
			}
			s.post(func() {
				if s.searchSeq != seq {
					return
				}
				s.searchHits = hits
				s.searchBusy = false
			})
			return nil
		}, func(err error) {
			if s.searchSeq != seq {
				return
			}
			s.searchBusy = false
			if err != nil {
				s.searchErr = friendlyError(err)
			}
		})
	})
}

// runCommand runs the palette row with id, closing the palette first so
// the dialog or thread it opens has the screen.
func (s *Session) runCommand(id string) {
	s.closePalette()
	switch {
	case id == "new-thread":
		s.openNewThread()
	case id == "new-project":
		s.openNewProject()
	case id == "settings":
		s.openSettings()
	case id == "toggle-archived":
		s.toggleShowArchived()
	case id == "toggle-sidebar":
		s.setSidebarOpen(!s.sidebarOpen)
	case strings.HasPrefix(id, "thread:"):
		if threadID := strings.TrimPrefix(id, "thread:"); threadID != "" {
			if strings.Contains(threadID, "/") {
				s.selectSidebarThread(threadID)
			} else {
				s.selectThread(threadID)
			}
		}
	case strings.HasPrefix(id, "message:"):
		parts := strings.SplitN(strings.TrimPrefix(id, "message:"), ":", 2)
		if len(parts) == 2 && parts[0] != "" {
			s.selectThread(parts[0])
		}
	}
}

// setSettingsSection switches the settings page and remembers it.
func (s *Session) setSettingsSection(name string) {
	s.settingsSection = name
	s.prepareLocalSettingsTarget(name)
	if s.mobilePage == "settings" && s.mobileSettingsDetail != "" {
		s.mobileSettingsDetail = name
	}
	_ = s.store.SetItem("settingsSection", name)
	s.loadSettingsSection(name)
}

// openSettings starts a draft from the environment's settings, so closing
// the dialog keeps what was typed out of the saved values.
func (s *Session) openSettings() {
	if s.settingsSaveBusy {
		return
	}
	if s.mobileThreadSearch {
		s.closeMobileThreadSearch()
	}
	s.connErr = ""
	s.settingsSaveErr = ""
	s.settingsEnv = s.profile.ID
	s.settingsVisited = map[*Session]bool{s: true}
	s.beginSettingsDraft()
	s.prepareLocalSettingsTarget(s.settingsSection)
	if s.narrow && s.settingsSection == "usage" {
		s.settingsSection = "general"
	}
	if s.narrow && s.selected == "" {
		s.mobileSettingsDetail = ""
		s.mobilePage = "settings"
		return
	}
	s.showSettings = true
	s.loadSettingsSection(s.settingsSection)
}

func (s *Session) closeSettings() {
	if s.settingsSaveBusy {
		return
	}
	s.showSettings = false
	if s.mobilePage == "settings" {
		s.mobileSettingsDetail = ""
		s.mobilePage = "threads"
	}
}

// saveSettings writes the draft and its values into the environment.
func (s *Session) saveSettings(st proto.Settings) {
	s.settings = st
	s.call(func(ctx context.Context, c *client.Client) error {
		_, err := c.SaveSettings(ctx, st)
		return err
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
		}
	})
}

func (s *Session) askDeleteThread(t proto.Thread) {
	if t.ID == "" {
		return
	}
	s.connErr = ""
	s.pendingDelete = itemTarget{Kind: "thread", ID: t.ID, Name: t.Title}
}

func (s *Session) askDeleteProject(p proto.Project) {
	if p.ID == "" {
		return
	}
	s.connErr = ""
	s.pendingDelete = itemTarget{Kind: "project", ID: p.ID, Name: p.Name}
}

func (s *Session) cancelDelete() {
	s.pendingDelete = itemTarget{}
}

// confirmDelete removes the pending thread or project, after the dialog
// asked. A project takes its threads with it.
func (s *Session) confirmDelete() {
	target := s.pendingDelete
	if target.Kind == "" {
		return
	}
	s.pendingDelete = itemTarget{}
	var removed []string
	if target.Kind == "project" {
		for _, t := range s.threads {
			if t.ProjectID == target.ID {
				removed = append(removed, t.ID)
			}
		}
	}
	s.call(func(ctx context.Context, c *client.Client) error {
		if target.Kind == "project" {
			return c.DeleteProject(ctx, target.ID)
		}
		return c.DeleteThread(ctx, target.ID)
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		if target.Kind == "project" {
			if s.selected != "" {
				for _, id := range removed {
					if id == s.selected {
						s.selected = ""
						s.messages = nil
						s.detail = proto.ThreadDetail{}
						break
					}
				}
			}
			s.reload()
			return
		}
		s.removeThread(target.ID)
		if s.selected == target.ID {
			s.selected = ""
			s.messages = nil
			s.detail = proto.ThreadDetail{}
			s.running = false
		}
	})
}

func (s *Session) askRenameThread(t proto.Thread) {
	if t.ID == "" {
		return
	}
	s.connErr = ""
	s.renameName = t.Title
	s.pendingRename = itemTarget{Kind: "thread", ID: t.ID, Name: t.Title}
}

func (s *Session) askRenameProject(p proto.Project) {
	if p.ID == "" {
		return
	}
	s.connErr = ""
	s.renameName = p.Name
	s.pendingRename = itemTarget{Kind: "project", ID: p.ID, Name: p.Name}
}

func (s *Session) cancelRename() {
	s.pendingRename = itemTarget{}
}

// setArchived files a thread away or brings it back. Archiving the open
// thread closes it when the list hides archived ones.
func (s *Session) setArchived(t proto.Thread, archived bool) {
	if t.ID == "" {
		return
	}
	s.connErr = ""
	var updated proto.Thread
	s.call(func(ctx context.Context, c *client.Client) error {
		u, err := c.UpdateThread(ctx, t.ID, map[string]any{"archived": archived})
		updated = u
		return err
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		if updated.ID != "" {
			s.upsertThread(updated)
		}
		if archived && !s.showArchived && s.selected == t.ID {
			s.selected = ""
			s.messages = nil
			s.detail = proto.ThreadDetail{}
			s.running = false
		}
	})
}

func (s *Session) toggleShowArchived() {
	s.showArchived = !s.showArchived
}

// confirmRename saves the pending thread's or project's new name.
func (s *Session) confirmRename() {
	target := s.pendingRename
	name := strings.TrimSpace(s.renameName)
	if target.Kind == "" || name == "" || name == target.Name {
		s.cancelRename()
		return
	}
	s.pendingRename = itemTarget{}
	var updated proto.Thread
	s.call(func(ctx context.Context, c *client.Client) error {
		if target.Kind == "project" {
			_, err := c.UpdateProject(ctx, target.ID, name, "")
			return err
		}
		t, err := c.UpdateThread(ctx, target.ID, map[string]any{"title": name})
		updated = t
		return err
	}, func(err error) {
		if err != nil {
			s.connErr = friendlyError(err)
			return
		}
		if target.Kind == "project" {
			s.reload()
			return
		}
		if updated.ID != "" {
			s.upsertThread(updated)
		}
		if s.selected == target.ID {
			d := s.detail
			d.Thread = updated
			s.detail = d
		}
	})
}

// createPreparedThread makes the thread the new-thread page prepared: it
// names the thread from the prompt, creates it in the chosen project,
// and sends the first message when one was typed.
func (s *Session) createPreparedThread(send bool) {
	target := s
	if compactView && s.manager != nil {
		target = s.newThreadTarget()
	}
	if target == nil || target.threadBusy {
		return
	}
	if !target.settingsConnectionReady() {
		target.connErr = i18n.T("Connect the selected environment in Settings first.")
		return
	}
	draft := target.newThreadPrompt
	prompt := strings.TrimSpace(draft)
	attachments := append([]proto.Attachment(nil), target.newThreadFiles...)
	if send && prompt == "" {
		return
	}
	if target.newThreadProj == "" {
		target.connErr = i18n.T("Choose a project first.")
		return
	}
	if target.newThreadTitle == "" && prompt != "" {
		target.newThreadTitle = oneLine(prompt, 72)
	}
	target.createThreadThen(func(created proto.Thread) {
		s.showNewThread = false
		target.newThreadFiles = nil
		if s.manager != nil {
			s.manager.activate(target.profile.ID)
		}
		if !send {
			target.saveAttachmentDraftFor(created.ID, attachments)
		}
		target.selectThread(created.ID)
		if send {
			// Keep any edits made while the create request was in flight.
			if target.newThreadPrompt == draft {
				target.newThreadPrompt = ""
			}
			target.sendPrompt(created.ID, prompt, attachments)
		} else if target.newThreadPrompt == draft {
			target.newThreadPrompt = ""
		}
	})
}

// compactView says whether the window is phone-wide; the view keeps it
// current, since the data layer has no window to ask.
var compactView bool
