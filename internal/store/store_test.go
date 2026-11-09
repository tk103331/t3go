package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProjectsAndThreads(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Now()
	p := proto.Project{ID: "prj_1", Name: "demo", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := s.CreateProject(ctx, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.Project(ctx, p.ID); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if projects, err := s.Projects(ctx); err != nil || len(projects) != 1 {
		t.Fatalf("projects = %d, %v", len(projects), err)
	}

	th := proto.Thread{ID: "thr_1", ProjectID: p.ID, Title: "First", Provider: "claude", Model: "m",
		Permission: proto.PermissionFullAccess, Status: proto.ThreadIdle, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateThread(ctx, th); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threads, err := s.Threads(ctx, p.ID, false)
	if err != nil || len(threads) != 1 {
		t.Fatalf("threads = %d, %v", len(threads), err)
	}
	if threads[0].Title != "First" {
		t.Fatalf("title = %q", threads[0].Title)
	}
	th.Title = "Renamed"
	if err := s.UpdateThread(ctx, th); err != nil {
		t.Fatalf("update thread: %v", err)
	}
	if got, _ := s.Thread(ctx, th.ID); got.Title != "Renamed" {
		t.Fatalf("updated title = %q", got.Title)
	}
	if err := s.DeleteThread(ctx, th.ID); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	if _, err := s.Thread(ctx, th.ID); err != ErrNotFound {
		t.Fatalf("deleted thread err = %v", err)
	}
}

func TestMessagesKeepNewestInOrder(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Now()
	p := proto.Project{ID: "prj_1", Name: "demo", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := s.CreateProject(ctx, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := s.CreateThread(ctx, proto.Thread{ID: "thr_1", ProjectID: p.ID, Title: "t", Provider: "claude", Model: "m",
		Permission: proto.PermissionFullAccess, Status: proto.ThreadIdle, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	for i := 0; i < 5; i++ {
		m := proto.Message{ID: fmt.Sprintf("msg_%d", i), ThreadID: "thr_1", Role: proto.RoleAssistant,
			Kind: proto.KindText, Content: fmt.Sprint(i), CreatedAt: now.Add(time.Duration(i) * time.Millisecond)}
		if _, err := s.AppendMessage(ctx, m); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	all, err := s.Messages(ctx, "thr_1", 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("all = %d, %v", len(all), err)
	}
	last, err := s.Messages(ctx, "thr_1", 2)
	if err != nil {
		t.Fatalf("limited: %v", err)
	}
	if len(last) != 2 || last[0].Content != "3" || last[1].Content != "4" {
		t.Fatalf("limited messages = %#v", last)
	}
	last[0].Content = "three"
	if err := s.UpdateMessage(ctx, last[0]); err != nil {
		t.Fatalf("update message: %v", err)
	}
	if got, _ := s.Message(ctx, last[0].ID); got.Content != "three" {
		t.Fatalf("updated content = %q", got.Content)
	}
}

func TestSettingsAndEvents(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	if err := s.SetSetting(ctx, "settings", `{"title":"Mine"}`); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if raw, ok, err := s.Setting(ctx, "settings"); err != nil || !ok || raw != `{"title":"Mine"}` {
		t.Fatalf("setting = %q, %v, %v", raw, ok, err)
	}

	for i := 1; i <= 3; i++ {
		e := proto.Event{Type: proto.EventNotice, Data: proto.MessageDelta{MessageID: "m", Delta: fmt.Sprint(i)}}
		seq, err := s.AppendEvent(ctx, e, fmt.Sprintf(`{"delta":%d}`, i))
		if err != nil || seq != int64(i) {
			t.Fatalf("append event %d: seq %d, %v", i, seq, err)
		}
	}
	events, err := s.Events(ctx, 1, 10)
	if err != nil || len(events) != 2 || events[0].Seq != 2 {
		t.Fatalf("events after 1 = %#v, %v", events, err)
	}
	if seq, _ := s.LastEventSeq(ctx); seq != 3 {
		t.Fatalf("last seq = %d", seq)
	}
	if err := s.PruneEvents(ctx, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if seq, _ := s.LastEventSeq(ctx); seq != 3 {
		t.Fatalf("seq after prune = %d", seq)
	}
	events, _ = s.Events(ctx, 0, 10)
	if len(events) != 1 || events[0].Seq != 3 {
		t.Fatalf("kept events = %#v", events)
	}
}
