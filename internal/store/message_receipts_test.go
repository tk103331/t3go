package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
)

func TestStartTurnRollsBackAndRetainsReceiptsAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "turn.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	now := time.Now()
	project := proto.Project{ID: "project", Name: "atomic", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := s.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	thread := proto.Thread{ID: "thread", ProjectID: project.ID, Title: "Before", Status: proto.ThreadIdle, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateThread(ctx, thread); err != nil {
		t.Fatal(err)
	}
	run := proto.Run{ID: "run", ThreadID: thread.ID, Status: proto.RunRunning, Prompt: "one", StartedAt: now}
	messages := []proto.Message{
		{ID: "user", ThreadID: thread.ID, RunID: run.ID, Role: proto.RoleUser, Kind: proto.KindText, Content: "one", CreatedAt: now},
		{ID: "assistant", ThreadID: thread.ID, RunID: run.ID, Role: proto.RoleAssistant, Kind: proto.KindText, CreatedAt: now},
	}
	thread.Title, thread.Status = "After", proto.ThreadRunning
	// Fail at the final write: all earlier writes must be rolled back.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_receipt BEFORE INSERT ON message_receipts BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.StartTurn(ctx, thread, run, messages, "send_once", "hash"); err == nil {
		t.Fatal("receipt failure ignored")
	}
	gotThread, err := s.Thread(ctx, thread.ID)
	if err != nil || gotThread.Title != "Before" || gotThread.Status != proto.ThreadIdle {
		t.Fatalf("partial thread: %#v, %v", gotThread, err)
	}
	runs, err := s.Runs(ctx, thread.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("partial run: %#v, %v", runs, err)
	}
	gotMessages, err := s.Messages(ctx, thread.ID, 0)
	if err != nil || len(gotMessages) != 0 {
		t.Fatalf("partial transcript: %#v, %v", gotMessages, err)
	}
	if _, _, err := s.MessageReceipt(ctx, thread.ID, "send_once"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial receipt: %v", err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_receipt`); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.StartTurn(canceled, thread, run, messages, "send_once", "hash"); err == nil {
		t.Fatal("canceled transaction committed")
	}
	if err := s.StartTurn(ctx, thread, run, messages, "send_once", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	receipt, hash, err := s.MessageReceipt(ctx, thread.ID, "send_once")
	if err != nil || receipt.RunID != run.ID || hash != "hash" || receipt.AcceptedAt.IsZero() {
		t.Fatalf("lost durable receipt: %#v %q %v", receipt, hash, err)
	}
	if _, err := s.db.Exec(`DELETE FROM runs WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MessageReceipt(ctx, thread.ID, "send_once"); err != nil {
		t.Fatalf("history deletion erased acceptance: %v", err)
	}
	if err := s.DeleteThread(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MessageReceipt(ctx, thread.ID, "send_once"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted thread kept receipt: %v", err)
	}
}
