package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
)

type settlementRunner struct {
	fakeRunner
	started chan struct{}
	release chan struct{}
}

func (r *settlementRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	close(r.started)
	select {
	case <-r.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	sink.Session("late-session")
	sink.Text("Completed")
	return nil
}

func TestExplicitSettlementAndNewTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runner := &settlementRunner{started: make(chan struct{}), release: make(chan struct{})}
	svc := newTestService(t, runner)
	project, err := svc.CreateProject(ctx, "Project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Provider: "fake", Model: "fake-1"})
	if err != nil {
		t.Fatal(err)
	}
	settled, nextModel := true, "next-model"
	if _, err := svc.UpdateThread(ctx, thread.ID, ThreadPatch{Settled: &settled}); err != nil {
		t.Fatal(err)
	}
	run, err := svc.SendWithRequest(ctx, thread.ID, "Start new work", nil, "send_settled")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop(thread.ID)
	select {
	case <-runner.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	active, err := svc.Store.Thread(ctx, thread.ID)
	if err != nil || active.Settled || active.Status != proto.ThreadRunning {
		t.Fatalf("new work did not reopen the thread: %+v, %v", active, err)
	}
	if _, err := svc.UpdateThread(ctx, thread.ID, ThreadPatch{Settled: &settled, Model: &nextModel}); err != nil {
		t.Fatal(err)
	}
	close(runner.release)
	if err := svc.Wait(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	finished, err := svc.Store.Thread(ctx, thread.ID)
	if err != nil || !finished.Settled || finished.Model != nextModel || finished.Status != proto.ThreadIdle {
		t.Fatalf("run completion lost explicit user choices: %+v, %v", finished, err)
	}
	again, err := svc.SendWithRequest(ctx, thread.ID, "Start new work", nil, "send_settled")
	if err != nil || again.ID != run.ID {
		t.Fatalf("idempotent retry changed the turn: %+v, %v", again, err)
	}
	finished, err = svc.Store.Thread(ctx, thread.ID)
	if err != nil || !finished.Settled {
		t.Fatal("replaying an accepted request unsettled the thread")
	}
}
