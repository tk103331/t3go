// Package orchestrator owns projects, threads and the agent runs inside
// them. It serializes one run per thread and records every change as an
// event.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/events"
	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/store"
)

// Errors callers can match.
var (
	ErrBusy          = errors.New("thread is already running")
	ErrEmpty         = errors.New("message is empty")
	ErrArchived      = errors.New("thread is archived")
	ErrBadPath       = errors.New("path is not a directory")
	ErrNoProvider    = errors.New("provider is not installed")
	ErrBadAttachment = errors.New("attachment not found")
)

// Service is the environment's orchestration layer.
type Service struct {
	Store     *store.Store
	Providers *provider.Registry
	Bus       *events.Bus
	Home      string
	Log       *log.Logger

	mu     sync.Mutex
	active map[string]*activeRun
}

type activeRun struct {
	cancel context.CancelFunc
	runID  string
	done   chan struct{}
}

// New returns a service.
func New(st *store.Store, providers *provider.Registry, bus *events.Bus, home string, logger *log.Logger) *Service {
	if logger == nil {
		logger = log.Default()
	}
	return &Service{Store: st, Providers: providers, Bus: bus, Home: home, Log: logger, active: map[string]*activeRun{}}
}

// interruptedReason is what a run a crash or shutdown cut short reports.
const interruptedReason = "the environment restarted before this run finished"

// RecoverInterrupted marks the runs and threads a crash left running, so
// the next start shows them as failed instead of running forever.
func (s *Service) RecoverInterrupted(ctx context.Context) (runs, threads int64, err error) {
	return s.Store.InterruptRuns(ctx, interruptedReason, time.Now())
}

// Shutdown cancels the live runs and waits for them to record their end,
// so the database closes after them.
func (s *Service) Shutdown(ctx context.Context) {
	s.mu.Lock()
	live := make([]*activeRun, 0, len(s.active))
	for _, run := range s.active {
		run.cancel()
		live = append(live, run)
	}
	s.mu.Unlock()
	for _, run := range live {
		select {
		case <-run.done:
		case <-ctx.Done():
			return
		}
	}
}

// Running reports whether a thread has a live run.
func (s *Service) Running(threadID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.active[threadID]
	return ok
}

// Settings reads the environment's settings.
func (s *Service) Settings(ctx context.Context) proto.Settings {
	settings := proto.DefaultSettings()
	if raw, ok, _ := s.Store.Setting(ctx, "settings"); ok {
		if err := jsonUnmarshal(raw, &settings); err != nil {
			s.Log.Printf("t3go: read settings: %v", err)
		}
	}
	return settings
}

// SaveSettings writes the environment's settings.
func (s *Service) SaveSettings(ctx context.Context, settings proto.Settings) error {
	raw, err := jsonMarshal(settings)
	if err != nil {
		return err
	}
	if err := s.Store.SetSetting(ctx, "settings", string(raw)); err != nil {
		return err
	}
	s.publish(ctx, proto.Event{Type: proto.EventSettings, Data: settings})
	return nil
}

// UploadPath is an uploaded file's place on disk.
func (s *Service) UploadPath(id string) string { return filepath.Join(s.Home, "uploads", id) }

// SaveUpload stores a prompt attachment and returns its metadata.
func (s *Service) SaveUpload(ctx context.Context, name, mime string, r io.Reader) (proto.Attachment, error) {
	name = cleanFilename(name)
	if name == "" {
		name = "file"
	}
	if err := os.MkdirAll(filepath.Join(s.Home, "uploads"), 0o700); err != nil {
		return proto.Attachment{}, fmt.Errorf("upload: directory: %w", err)
	}
	uploadID := id.New("upl")
	path := s.UploadPath(uploadID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return proto.Attachment{}, fmt.Errorf("upload: create: %w", err)
	}
	size, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if copyErr != nil {
			return proto.Attachment{}, fmt.Errorf("upload: write: %w", copyErr)
		}
		return proto.Attachment{}, fmt.Errorf("upload: close: %w", closeErr)
	}
	attachment := proto.Attachment{ID: uploadID, Name: name, MimeType: strings.TrimSpace(mime), Size: size}
	if err := s.Store.CreateUpload(ctx, attachment); err != nil {
		_ = os.Remove(path)
		return proto.Attachment{}, err
	}
	return attachment, nil
}

// DeleteUpload removes an uploaded file and its metadata.
func (s *Service) DeleteUpload(ctx context.Context, uploadID string) error {
	if uploadID == "" {
		return nil
	}
	_ = os.Remove(s.UploadPath(uploadID))
	return s.Store.DeleteUpload(ctx, uploadID)
}

func (s *Service) deleteAttachments(ctx context.Context, messages []proto.Message) {
	for _, m := range messages {
		for _, a := range m.Attachments {
			_ = s.DeleteUpload(ctx, a.ID)
		}
	}
}

// cleanFilename keeps a name to one line and a sensible length.
func cleanFilename(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\r", " "))
	name = strings.ReplaceAll(name, "\x00", "")
	if i := strings.IndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	r := []rune(name)
	if len(r) > 120 {
		name = string(r[:119]) + "…"
	}
	return name
}

// CreateProject registers a directory.
func (s *Service) CreateProject(ctx context.Context, name, path string) (proto.Project, error) {
	path = store.NormalizePath(path)
	if path == "" {
		return proto.Project{}, errors.New("path is required")
	}
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return proto.Project{}, ErrBadPath
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(path)
	}
	now := time.Now()
	p := proto.Project{ID: id.New("prj"), Name: strings.TrimSpace(name), Path: path, CreatedAt: now, UpdatedAt: now}
	if err := s.Store.CreateProject(ctx, p); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return proto.Project{}, errors.New("this directory is already a project")
		}
		return proto.Project{}, err
	}
	s.publish(ctx, proto.Event{Type: proto.EventProject, ProjectID: p.ID, Data: p})
	return p, nil
}

// UpdateProject changes a project's name and path.
func (s *Service) UpdateProject(ctx context.Context, projectID, name, path string) (proto.Project, error) {
	p, err := s.Store.Project(ctx, projectID)
	if err != nil {
		return p, err
	}
	if name != "" {
		p.Name = strings.TrimSpace(name)
	}
	if path != "" {
		path = store.NormalizePath(path)
		st, err := os.Stat(path)
		if err != nil || !st.IsDir() {
			return p, ErrBadPath
		}
		p.Path = path
	}
	p.UpdatedAt = time.Now()
	if err := s.Store.UpdateProject(ctx, p); err != nil {
		return p, err
	}
	s.publish(ctx, proto.Event{Type: proto.EventProject, ProjectID: p.ID, Data: p})
	return p, nil
}

// DeleteProject removes a project and its threads.
func (s *Service) DeleteProject(ctx context.Context, projectID string) error {
	p, err := s.Store.Project(ctx, projectID)
	if err != nil {
		return err
	}
	// Stop live runs before their rows go away.
	threads, _ := s.Store.Threads(ctx, projectID, true)
	var messages []proto.Message
	for _, t := range threads {
		s.Stop(t.ID)
		if msgs, err := s.Store.Messages(ctx, t.ID, 0); err == nil {
			messages = append(messages, msgs...)
		}
	}
	if err := s.Store.DeleteProject(ctx, projectID); err != nil {
		return err
	}
	s.deleteAttachments(ctx, messages)
	s.publish(ctx, proto.Event{Type: proto.EventProject, ProjectID: p.ID, Data: map[string]string{"deleted": p.ID}})
	return nil
}

// CreateThreadInput creates a thread, optionally in its own worktree.
type CreateThreadInput struct {
	ProjectID  string
	Title      string
	Provider   string
	Model      string
	Permission proto.PermissionMode
	Branch     string
	Worktree   bool
}

// CreateThread makes a thread and its worktree when asked.
func (s *Service) CreateThread(ctx context.Context, in CreateThreadInput) (proto.Thread, error) {
	project, err := s.Store.Project(ctx, in.ProjectID)
	if err != nil {
		return proto.Thread{}, err
	}
	settings := s.Settings(ctx)
	if in.Provider == "" {
		in.Provider = settings.DefaultProvider
	}
	if in.Model == "" {
		in.Model = defaultModelFor(s.Providers.Snapshot(ctx), in.Provider)
	}
	if !in.Permission.Valid() {
		in.Permission = proto.PermissionMode(settings.DefaultPermission)
	}
	if !in.Permission.Valid() {
		in.Permission = proto.PermissionFullAccess
	}
	if _, ok := s.Providers.Get(in.Provider); !ok {
		return proto.Thread{}, fmt.Errorf("unknown provider %q", in.Provider)
	}
	now := time.Now()
	thread := proto.Thread{
		ID: id.New("thr"), ProjectID: project.ID, Title: cleanTitle(in.Title), Provider: in.Provider, Model: in.Model,
		Permission: in.Permission, Branch: strings.TrimSpace(in.Branch), Status: proto.ThreadIdle, CreatedAt: now, UpdatedAt: now,
	}
	if thread.Title == "" {
		thread.Title = "New thread"
	}
	if in.Worktree {
		if !git.IsRepo(ctx, project.Path) {
			return proto.Thread{}, errors.New("worktrees need a Git project")
		}
		slug := slugify(thread.Title)
		if slug == "" {
			slug = strings.TrimPrefix(thread.ID, "thr_")[:8]
		}
		if thread.Branch == "" {
			thread.Branch = "t3go/" + slug
		}
		thread.WorktreePath = filepath.Join(s.Home, "worktrees", slug+"-"+shortID(thread.ID))
		if err := git.CreateWorktree(ctx, project.Path, thread.WorktreePath, thread.Branch, ""); err != nil {
			return proto.Thread{}, fmt.Errorf("create worktree: %w", err)
		}
	} else if thread.Branch == "" {
		thread.Branch = git.Status(ctx, project.Path).Branch
	}
	if err := s.Store.CreateThread(ctx, thread); err != nil {
		if thread.WorktreePath != "" {
			_ = git.RemoveWorktree(ctx, project.Path, thread.WorktreePath)
		}
		return proto.Thread{}, err
	}
	s.publish(ctx, proto.Event{Type: proto.EventThreadCreated, ThreadID: thread.ID, ProjectID: thread.ProjectID, Data: thread})
	return thread, nil
}

// UpdateThread changes a thread's title, model, permission or archive
// state.
func (s *Service) UpdateThread(ctx context.Context, threadID string, patch ThreadPatch) (proto.Thread, error) {
	t, err := s.Store.Thread(ctx, threadID)
	if err != nil {
		return t, err
	}
	if patch.Title != nil {
		t.Title = cleanTitle(*patch.Title)
		if t.Title == "" {
			t.Title = "Untitled thread"
		}
	}
	if patch.Provider != nil && *patch.Provider != "" {
		t.Provider = *patch.Provider
	}
	if patch.Model != nil && *patch.Model != "" {
		t.Model = *patch.Model
	}
	if patch.Permission != nil && patch.Permission.Valid() {
		t.Permission = *patch.Permission
	}
	if patch.Archived != nil {
		t.Archived = *patch.Archived
	}
	t.UpdatedAt = time.Now()
	if err := s.Store.UpdateThread(ctx, t); err != nil {
		return t, err
	}
	s.publish(ctx, proto.Event{Type: proto.EventThreadUpdated, ThreadID: t.ID, Data: t})
	return t, nil
}

// ThreadPatch is a partial thread update.
type ThreadPatch struct {
	Title      *string
	Provider   *string
	Model      *string
	Permission *proto.PermissionMode
	Archived   *bool
}

// DeleteThread stops and removes a thread.
func (s *Service) DeleteThread(ctx context.Context, threadID string) error {
	t, err := s.Store.Thread(ctx, threadID)
	if err != nil {
		return err
	}
	s.Stop(threadID)
	if t.WorktreePath != "" && strings.HasPrefix(t.WorktreePath, filepath.Join(s.Home, "worktrees")+string(os.PathSeparator)) {
		if p, err := s.Store.Project(ctx, t.ProjectID); err == nil {
			_ = git.RemoveWorktree(ctx, p.Path, t.WorktreePath)
		}
	}
	messages, _ := s.Store.Messages(ctx, threadID, 0)
	if err := s.Store.DeleteThread(ctx, threadID); err != nil {
		return err
	}
	s.deleteAttachments(ctx, messages)
	s.publish(ctx, proto.Event{Type: proto.EventThreadDeleted, ThreadID: threadID})
	return nil
}

// Detail returns a thread, its transcript, runs and checkout state.
func (s *Service) Detail(ctx context.Context, threadID string) (proto.ThreadDetail, error) {
	t, err := s.Store.Thread(ctx, threadID)
	if err != nil {
		return proto.ThreadDetail{}, err
	}
	messages, err := s.Store.Messages(ctx, threadID, 4000)
	if err != nil {
		return proto.ThreadDetail{}, err
	}
	runs, err := s.Store.Runs(ctx, threadID)
	if err != nil {
		return proto.ThreadDetail{}, err
	}
	detail := proto.ThreadDetail{Thread: t, Messages: messages, Runs: runs}
	if detail.Messages == nil {
		detail.Messages = []proto.Message{}
	}
	if detail.Runs == nil {
		detail.Runs = []proto.Run{}
	}
	dir := s.ThreadDir(ctx, t)
	if st := git.Status(ctx, dir); st.Repo {
		var latest string
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].Checkpoint != "" {
				latest = runs[i].Checkpoint
				break
			}
		}
		if latest != "" {
			st.Checkpoint = latest
			st.DiffStat = git.DiffStat(ctx, dir, latest)
		}
		detail.Git = &st
	}
	return detail, nil
}

// ThreadDir is the checkout a thread works in.
func (s *Service) ThreadDir(ctx context.Context, t proto.Thread) string {
	if t.WorktreePath != "" {
		return t.WorktreePath
	}
	if p, err := s.Store.Project(ctx, t.ProjectID); err == nil {
		return p.Path
	}
	return ""
}

// Stop cancels a thread's live run.
func (s *Service) Stop(threadID string) bool {
	s.mu.Lock()
	run := s.active[threadID]
	s.mu.Unlock()
	if run == nil {
		return false
	}
	run.cancel()
	return true
}

// Wait blocks until a thread's live run ends.
func (s *Service) Wait(ctx context.Context, threadID string) error {
	s.mu.Lock()
	run := s.active[threadID]
	s.mu.Unlock()
	if run == nil {
		return nil
	}
	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) publish(ctx context.Context, e proto.Event) {
	_ = s.Bus.Publish(ctx, e)
}

// cleanTitle keeps a title to one line and a sensible length.
func cleanTitle(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", " "))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	r := []rune(s)
	if len(r) > 80 {
		s = string(r[:79]) + "…"
	}
	return s
}

func titleFromPrompt(prompt string) string {
	line := strings.TrimSpace(prompt)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	line = strings.TrimPrefix(line, "#")
	line = strings.TrimSpace(line)
	if line == "" {
		return "New thread"
	}
	return cleanTitle(line)
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.' || r == '/':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 32 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

func shortID(s string) string {
	s = strings.TrimPrefix(s, "thr_")
	if len(s) > 8 {
		s = s[:8]
	}
	return s
}

func defaultModelFor(providers []proto.Provider, provider string) string {
	for _, p := range providers {
		if p.ID != provider {
			continue
		}
		for _, m := range p.Models {
			if m.Default {
				return m.ID
			}
		}
		if len(p.Models) > 0 {
			return p.Models[0].ID
		}
	}
	return ""
}
