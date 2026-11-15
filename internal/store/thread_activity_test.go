package store

import (
	"context"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
)

func TestSettlementMigrationAndRunUpdates(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Now()
	project := proto.Project{ID: "project", Path: t.TempDir(), Name: "Project", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	thread := proto.Thread{ID: "thread", ProjectID: project.ID, Title: "Original", Provider: "claude", Model: "old",
		Status: proto.ThreadIdle, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateThread(ctx, thread); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema, retaining real existing project/thread rows.
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE threads DROP COLUMN settled"); err != nil {
		t.Fatal(err)
	}
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	thread, err = upgraded.Thread(ctx, thread.ID)
	if err != nil || thread.Settled || thread.Title != "Original" {
		t.Fatalf("migration changed the existing inbox: %+v, %v", thread, err)
	}
	thread.Settled, thread.Model, thread.Title = true, "next", "Reviewed"
	if err := upgraded.UpdateThread(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.SetProviderSession(ctx, thread.ID, "claude", "late-session"); err != nil {
		t.Fatal(err)
	}
	finished, err := upgraded.FinishThread(ctx, thread.ID, proto.ThreadIdle, "")
	if err != nil || !finished.Settled || finished.Model != "next" || finished.Title != "Reviewed" {
		t.Fatalf("late run update overwrote user choices: %+v, %v", finished, err)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	thread, err = reopened.Thread(ctx, thread.ID)
	if err != nil || !thread.Settled {
		t.Fatalf("explicit settlement was lost after restart: %+v, %v", thread, err)
	}
}
