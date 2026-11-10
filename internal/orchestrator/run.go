package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
)

// Send starts a run for prompt in a thread.
func (s *Service) Send(ctx context.Context, threadID, prompt string) (proto.Run, error) {
	return s.SendWith(ctx, threadID, prompt, nil)
}

// SendWith starts a run for prompt and the attachments it names.
func (s *Service) SendWith(ctx context.Context, threadID, prompt string, attachmentIDs []string) (proto.Run, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return proto.Run{}, ErrEmpty
	}
	attachments, providerPrompt, err := s.resolveAttachments(ctx, prompt, attachmentIDs)
	if err != nil {
		return proto.Run{}, err
	}
	t, err := s.Store.Thread(ctx, threadID)
	if err != nil {
		return proto.Run{}, err
	}
	if t.Archived {
		return proto.Run{}, ErrArchived
	}
	runner, ok := s.Providers.Get(t.Provider)
	if !ok {
		return proto.Run{}, ErrNoProvider
	}
	if snapshot := s.Providers.Snapshot(ctx); !providerInstalled(snapshot, t.Provider) {
		return proto.Run{}, ErrNoProvider
	}
	project, err := s.Store.Project(ctx, t.ProjectID)
	if err != nil {
		return proto.Run{}, err
	}
	dir := t.WorktreePath
	if dir == "" {
		dir = project.Path
	}

	s.mu.Lock()
	if _, busy := s.active[threadID]; busy {
		s.mu.Unlock()
		return proto.Run{}, ErrBusy
	}
	now := time.Now()
	run := proto.Run{
		ID: id.New("run"), ThreadID: t.ID, Status: proto.RunRunning, Provider: t.Provider, Model: t.Model,
		Permission: t.Permission, Prompt: prompt, StartedAt: now,
	}
	userMsg := proto.Message{ID: id.New("msg"), ThreadID: t.ID, RunID: run.ID, Role: proto.RoleUser, Kind: proto.KindText,
		Content: prompt, Attachments: attachments, CreatedAt: now}
	assistant := proto.Message{ID: id.New("msg"), ThreadID: t.ID, RunID: run.ID, Role: proto.RoleAssistant, Kind: proto.KindText,
		Status: "running", CreatedAt: now.Add(time.Millisecond)}
	if err := s.Store.CreateRun(ctx, run); err != nil {
		s.mu.Unlock()
		return proto.Run{}, err
	}
	if _, err := s.Store.AppendMessage(ctx, userMsg); err != nil {
		s.mu.Unlock()
		return proto.Run{}, err
	}
	if _, err := s.Store.AppendMessage(ctx, assistant); err != nil {
		s.mu.Unlock()
		return proto.Run{}, err
	}
	if t.Title == "New thread" || t.Title == "" {
		t.Title = titleFromPrompt(prompt)
	}
	t.Status = proto.ThreadRunning
	t.LastError = ""
	t.Summary = prompt
	if len([]rune(t.Summary)) > 200 {
		t.Summary = string([]rune(t.Summary)[:199]) + "…"
	}
	t.UpdatedAt = time.Now()
	if err := s.Store.UpdateThread(ctx, t); err != nil {
		s.mu.Unlock()
		return proto.Run{}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.active[threadID] = &activeRun{cancel: cancel, runID: run.ID, done: done}
	s.mu.Unlock()

	s.publish(ctx, proto.Event{Type: proto.EventRunStarted, ThreadID: t.ID, Data: run})
	s.publish(ctx, proto.Event{Type: proto.EventThreadUpdated, ThreadID: t.ID, Data: t})
	s.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: t.ID, Data: userMsg})
	s.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: t.ID, Data: assistant})

	go s.execute(runCtx, t, dir, runner, run, providerPrompt, assistant, done)
	return run, nil
}

// resolveAttachments loads the uploads a prompt names and writes the list
// of paths the provider can read.
func (s *Service) resolveAttachments(ctx context.Context, prompt string, attachmentIDs []string) ([]proto.Attachment, string, error) {
	if len(attachmentIDs) == 0 {
		return nil, prompt, nil
	}
	if len(attachmentIDs) > maxAttachments {
		return nil, "", fmt.Errorf("at most %d attachments per message", maxAttachments)
	}
	seen := make(map[string]bool, len(attachmentIDs))
	attachments := make([]proto.Attachment, 0, len(attachmentIDs))
	var withFiles strings.Builder
	withFiles.WriteString(prompt)
	withFiles.WriteString("\n\nAttached files:\n")
	for _, uploadID := range attachmentIDs {
		if uploadID == "" || seen[uploadID] {
			continue
		}
		seen[uploadID] = true
		a, err := s.Store.Upload(ctx, uploadID)
		if err != nil {
			return nil, "", ErrBadAttachment
		}
		attachments = append(attachments, a)
		fmt.Fprintf(&withFiles, "- %s (%s", s.UploadPath(a.ID), a.Name)
		if a.MimeType != "" {
			fmt.Fprintf(&withFiles, ", %s", a.MimeType)
		}
		withFiles.WriteString(")\n")
	}
	if len(attachments) == 0 {
		return nil, prompt, nil
	}
	return attachments, withFiles.String(), nil
}

// maxAttachments is the most files one prompt carries.
const maxAttachments = 10

func providerInstalled(list []proto.Provider, id string) bool {
	for _, p := range list {
		if p.ID == id {
			return p.Installed
		}
	}
	return false
}

func (s *Service) execute(ctx context.Context, thread proto.Thread, dir string, runner provider.Runner, run proto.Run, prompt string, assistant proto.Message, done chan struct{}) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		delete(s.active, thread.ID)
		s.mu.Unlock()
	}()

	if ref, err := git.Checkpoint(ctx, dir, thread.ID, run.ID); err == nil {
		run.Checkpoint = ref
		if err := s.Store.SetRunCheckpoint(context.Background(), run.ID, ref); err != nil {
			s.Log.Printf("t3go: record checkpoint: %v", err)
		}
	}

	sink := &runSink{svc: s, thread: thread, run: &run, text: assistant, ctx: ctx, tools: map[string]*proto.Message{}}
	err := runner.Run(ctx, provider.Turn{
		ThreadID: thread.ID, RunID: run.ID, CWD: dir, Prompt: prompt, Model: thread.Model,
		Permission: thread.Permission, SessionID: thread.ProviderSession,
		ApprovalURL: s.ApprovalURL, ApprovalToken: s.ApprovalToken,
	}, sink)
	sink.finish(err)
}

type runSink struct {
	svc    *Service
	ctx    context.Context
	thread proto.Thread
	run    *proto.Run

	mu             sync.Mutex
	text           proto.Message
	reasoning      *proto.Message
	reasonText     strings.Builder
	tools          map[string]*proto.Message
	usage          *proto.Usage
	session        string
	lastTextFlush  time.Time
	lastThinkFlush time.Time
}

func (s *runSink) dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (s *runSink) sessionChanged(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	if s.session == id {
		s.mu.Unlock()
		return
	}
	s.session = id
	s.thread.ProviderSession = id
	s.thread.UpdatedAt = time.Now()
	thread := s.thread
	s.mu.Unlock()
	ctx, cancel := s.dbCtx()
	defer cancel()
	if err := s.svc.Store.UpdateThread(ctx, thread); err != nil {
		s.svc.Log.Printf("t3go: save provider session: %v", err)
		return
	}
	s.svc.publish(ctx, proto.Event{Type: proto.EventThreadUpdated, ThreadID: thread.ID, Data: thread})
}

func (s *runSink) Text(delta string) {
	if delta == "" {
		return
	}
	s.mu.Lock()
	s.text.Content += delta
	msgID := s.text.ID
	flush := time.Since(s.lastTextFlush) > 200*time.Millisecond
	if flush {
		s.lastTextFlush = time.Now()
	}
	s.mu.Unlock()
	s.svc.publish(context.Background(), proto.Event{Type: proto.EventMessageDelta, ThreadID: s.thread.ID,
		Data: proto.MessageDelta{MessageID: msgID, Delta: delta}})
	if flush {
		s.flushMessage(true)
	}
}

func (s *runSink) Reasoning(delta string) {
	if delta == "" {
		return
	}
	s.mu.Lock()
	if s.reasoning == nil {
		msg := proto.Message{ID: id.New("msg"), ThreadID: s.thread.ID, RunID: s.run.ID, Role: proto.RoleAssistant,
			Kind: proto.KindReasoning, Status: "running", CreatedAt: time.Now()}
		s.reasoning = &msg
		ctx, cancel := s.dbCtx()
		if _, err := s.svc.Store.AppendMessage(ctx, msg); err != nil {
			s.svc.Log.Printf("t3go: append reasoning: %v", err)
		}
		cancel()
		s.svc.publish(context.Background(), proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: msg})
	}
	s.reasonText.WriteString(delta)
	s.reasoning.Content = s.reasonText.String()
	flush := time.Since(s.lastThinkFlush) > 500*time.Millisecond
	if flush {
		s.lastThinkFlush = time.Now()
	}
	msg := *s.reasoning
	s.mu.Unlock()
	if flush {
		ctx, cancel := s.dbCtx()
		if err := s.svc.Store.UpdateMessage(ctx, msg); err == nil {
			s.svc.publish(context.Background(), proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: msg})
		}
		cancel()
	}
}

func (s *runSink) ToolStart(toolID, name, detail string) {
	if name == "" {
		name = "Tool"
	}
	msg := proto.Message{ID: id.New("msg"), ThreadID: s.thread.ID, RunID: s.run.ID, Role: proto.RoleAssistant,
		Kind: proto.KindTool, Title: name, Detail: detail, Status: "running", CreatedAt: time.Now()}
	s.mu.Lock()
	s.tools[toolID] = &msg
	s.mu.Unlock()
	ctx, cancel := s.dbCtx()
	defer cancel()
	if _, err := s.svc.Store.AppendMessage(ctx, msg); err != nil {
		s.svc.Log.Printf("t3go: append tool: %v", err)
		return
	}
	s.svc.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: msg})
}

func (s *runSink) ToolFinish(toolID, output string, failed bool) {
	s.mu.Lock()
	msg, ok := s.tools[toolID]
	if !ok {
		s.mu.Unlock()
		return
	}
	msg.Content = output
	msg.Status = "completed"
	if failed {
		msg.Status = "failed"
	}
	copied := *msg
	s.mu.Unlock()
	ctx, cancel := s.dbCtx()
	defer cancel()
	if err := s.svc.Store.UpdateMessage(ctx, copied); err == nil {
		s.svc.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: copied})
	}
}

func (s *runSink) Session(id string) { s.sessionChanged(id) }

func (s *runSink) Usage(usage proto.Usage) {
	s.mu.Lock()
	s.usage = &usage
	s.mu.Unlock()
}

func (s *runSink) Notice(message string) {
	if message == "" {
		return
	}
	msg := proto.Message{ID: id.New("msg"), ThreadID: s.thread.ID, RunID: s.run.ID, Role: proto.RoleSystem,
		Kind: proto.KindNotice, Content: message, CreatedAt: time.Now()}
	ctx, cancel := s.dbCtx()
	defer cancel()
	if _, err := s.svc.Store.AppendMessage(ctx, msg); err != nil {
		return
	}
	s.svc.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: msg})
}

func (s *runSink) flushMessage(publish bool) {
	s.mu.Lock()
	msg := s.text
	s.mu.Unlock()
	ctx, cancel := s.dbCtx()
	defer cancel()
	if err := s.svc.Store.UpdateMessage(ctx, msg); err != nil {
		s.svc.Log.Printf("t3go: save message: %v", err)
		return
	}
	if publish {
		s.svc.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: msg})
	}
}

func (s *runSink) finish(runErr error) {
	s.mu.Lock()
	if s.reasoning != nil {
		s.reasoning.Content = s.reasonText.String()
		s.reasoning.Status = "completed"
		if ctx, cancel := s.dbCtx(); true {
			_ = s.svc.Store.UpdateMessage(ctx, *s.reasoning)
			cancel()
		}
	}
	text := s.text
	usage := s.usage
	s.mu.Unlock()

	cancelled := errors.Is(runErr, context.Canceled) || errors.Is(s.ctx.Err(), context.Canceled)
	now := time.Now()
	run := *s.run
	run.EndedAt = &now
	run.Usage = usage
	switch {
	case cancelled:
		run.Status = proto.RunCancelled
		run.Error = "Stopped"
	case runErr != nil:
		run.Status = proto.RunFailed
		run.Error = runErr.Error()
	default:
		run.Status = proto.RunCompleted
	}

	ctx, cancel := s.dbCtx()
	defer cancel()

	text.Status = "completed"
	if run.Status == proto.RunFailed {
		text.Status = "failed"
	} else if run.Status == proto.RunCancelled {
		text.Status = "cancelled"
	}
	if text.Content == "" {
		text.Content = ""
	}
	if err := s.svc.Store.UpdateMessage(ctx, text); err != nil {
		s.svc.Log.Printf("t3go: save final message: %v", err)
	}
	s.svc.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: text})

	if runErr != nil && !cancelled {
		errMsg := proto.Message{ID: id.New("msg"), ThreadID: s.thread.ID, RunID: s.run.ID, Role: proto.RoleSystem,
			Kind: proto.KindError, Content: runErr.Error(), CreatedAt: now}
		if _, err := s.svc.Store.AppendMessage(ctx, errMsg); err == nil {
			s.svc.publish(ctx, proto.Event{Type: proto.EventMessage, ThreadID: s.thread.ID, Data: errMsg})
		}
	}

	if err := s.svc.Store.UpdateRun(ctx, run); err != nil {
		s.svc.Log.Printf("t3go: save run: %v", err)
	}
	s.svc.publish(ctx, proto.Event{Type: proto.EventRunFinished, ThreadID: s.thread.ID, Data: run})

	thread := s.thread
	thread.UpdatedAt = now
	switch {
	case run.Status == proto.RunFailed:
		thread.Status = proto.ThreadFailed
		thread.LastError = run.Error
	default:
		thread.Status = proto.ThreadIdle
		thread.LastError = ""
	}
	if err := s.svc.Store.UpdateThread(ctx, thread); err != nil {
		s.svc.Log.Printf("t3go: save thread: %v", err)
	}
	s.svc.publish(ctx, proto.Event{Type: proto.EventThreadUpdated, ThreadID: thread.ID, Data: thread})
}

// Diff returns a run's checkpoint diff.
func (s *Service) Diff(ctx context.Context, threadID, runID string) (string, error) {
	t, err := s.Store.Thread(ctx, threadID)
	if err != nil {
		return "", err
	}
	runs, err := s.Store.Runs(ctx, threadID)
	if err != nil {
		return "", err
	}
	var ref string
	for i := len(runs) - 1; i >= 0; i-- {
		if (runID == "" || runs[i].ID == runID) && runs[i].Checkpoint != "" {
			ref = runs[i].Checkpoint
			break
		}
	}
	if ref == "" {
		return "", fmt.Errorf("no checkpoint for this run")
	}
	return git.Diff(ctx, s.ThreadDir(ctx, t), ref)
}
