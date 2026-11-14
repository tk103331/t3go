package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/store"
)

type receiptRunner struct {
	fakeRunner
	calls   atomic.Int32
	release chan struct{}
}

func (r *receiptRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	r.calls.Add(1)
	select {
	case <-r.release:
		return r.fakeRunner.Run(ctx, turn, sink)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMessageRequestDeduplicatesConcurrentAndCompletedTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runner := &receiptRunner{release: make(chan struct{})}
	svc := newTestService(t, runner)
	project, err := svc.CreateProject(ctx, "Receipts", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Provider: "fake", Model: "fake-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Stop(thread.ID); _ = svc.Wait(context.Background(), thread.ID) })
	const count = 12
	runs := make([]proto.Run, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runs[i], errs[i] = svc.SendWithRequest(ctx, thread.ID, "  Only once  ", nil, "send_same")
		}()
	}
	wg.Wait()
	for i := range count {
		if errs[i] != nil || runs[i].ID == "" || runs[i].ID != runs[0].ID {
			t.Fatalf("duplicate %d: %#v, %v", i, runs[i], errs[i])
		}
	}
	if _, err := svc.SendWithRequest(ctx, thread.ID, "different", nil, "send_same"); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("changed content: %v", err)
	}
	if _, err := svc.SendWithRequest(ctx, thread.ID, "Only once", []string{"new-upload"}, "send_same"); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("changed attachments: %v", err)
	}
	if _, err := svc.SendWithRequest(ctx, thread.ID, "later", nil, "send_busy"); !errors.Is(err, ErrBusy) {
		t.Fatalf("new request while busy: %v", err)
	}
	if _, _, err := svc.Store.MessageReceipt(ctx, thread.ID, "send_busy"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rejected request received a receipt: %v", err)
	}
	close(runner.release)
	if err := svc.Wait(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	// Even a new service instance must consult durable acceptance records.
	restarted := New(svc.Store, svc.Providers, svc.Bus, t.TempDir(), svc.Log)
	again, err := restarted.SendWithRequest(ctx, thread.ID, "Only once", []string{}, "send_same")
	if err != nil || again.ID != runs[0].ID || again.Status != proto.RunCompleted {
		t.Fatalf("completed retry: %#v, %v", again, err)
	}
	detail, err := svc.Detail(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, message := range detail.Messages {
		if message.Role == proto.RoleUser {
			users++
		}
	}
	if len(detail.Runs) != 1 || users != 1 || runner.calls.Load() != 1 {
		t.Fatalf("reexecuted: %d runs, %d user messages, %d executions", len(detail.Runs), users, runner.calls.Load())
	}
	other, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Provider: "fake", Model: "fake-1"})
	if err != nil {
		t.Fatal(err)
	}
	separate, err := svc.SendWithRequest(ctx, other.ID, "Only once", nil, "send_same")
	if err != nil || separate.ID == again.ID {
		t.Fatalf("thread scope: %#v, %v", separate, err)
	}
	if err := svc.Wait(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", svc.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM runs WHERE id=?`, runs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.SendWithRequest(ctx, thread.ID, "Only once", nil, "send_same"); !errors.Is(err, ErrAcceptedRunGone) {
		t.Fatalf("deleted accepted run was executable again: %v", err)
	}
	if runner.calls.Load() != 2 {
		t.Fatalf("removed history caused reexecution: %d", runner.calls.Load())
	}
}

func TestMessageReceiptSurvivesInterruptedAcceptance(t *testing.T) {
	ctx := context.Background()
	runner := &receiptRunner{release: make(chan struct{})}
	svc := newTestService(t, runner)
	project, err := svc.CreateProject(ctx, "Interrupted", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Provider: "fake", Model: "fake-1"})
	if err != nil {
		t.Fatal(err)
	}
	thread.Status = proto.ThreadRunning
	run := proto.Run{ID: "run_interrupted", ThreadID: thread.ID, Provider: thread.Provider, Status: proto.RunRunning, Prompt: "accepted", StartedAt: time.Now()}
	// Simulate a crash after the acceptance transaction and before execution.
	if err := svc.Store.StartTurn(ctx, thread, run, nil, "send_interrupted", messageHash("accepted", nil)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := svc.SendWithRequest(ctx, thread.ID, "accepted", nil, "send_interrupted")
	if err != nil || again.ID != run.ID || again.Status != proto.RunFailed || runner.calls.Load() != 0 {
		t.Fatalf("interrupted acceptance replayed: %#v, %v, calls=%d", again, err, runner.calls.Load())
	}
}

func TestRequestIDValidation(t *testing.T) {
	for _, key := range []string{"", ".", "..", "with space", "slash/path", "中文", string(make([]byte, 129))} {
		if ValidRequestID(key) {
			t.Errorf("accepted invalid key %q", key)
		}
	}
	for _, key := range []string{"send_1", "a.b-C", "1"} {
		if !ValidRequestID(key) {
			t.Errorf("rejected key %q", key)
		}
	}
}
