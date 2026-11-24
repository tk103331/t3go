package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// The @ list floats above the composer with a fixed height: a long file
// list scrolls inside it instead of stretching the page.
func TestUIComposerMentionPopoverScrolls(t *testing.T) {
	repo := gitRepo(t)
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("file-%02d.txt", i)
		if err := os.WriteFile(filepath.Join(repo, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Suggest", repo)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Suggest thread", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 1000, 760, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "connect")
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool { return session.detail.Thread.ID == thread.ID }, "thread")
	session.setDraft("@")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.fileIndex) >= 40
	}, "the file index to arrive")
	tester.pump()

	if !tester.tt.HasText(i18n.T("Files")) {
		t.Fatalf("the @ list is not a popover with its title: %v", tester.tt.Texts())
	}
	first, ok := tester.tt.Access("file-00.txt")
	if !ok || first.Bounds.H == 0 {
		t.Fatalf("the first file row is missing: %+v", first.Bounds)
	}
	late, ok := tester.tt.Access("file-39.txt")
	if !ok || late.Bounds.H != 0 {
		t.Fatalf("a long list shows every row instead of scrolling: %+v", late.Bounds)
	}
	// Scroll inside the panel until the last file shows, then pick it.
	point, ok := tester.tt.Access(i18n.T("Files"))
	if !ok {
		t.Fatal("the popover title has no place to scroll from")
	}
	for range 12 {
		tester.tt.Scroll(point.Bounds.X+point.Bounds.W/2, point.Bounds.Y+point.Bounds.H+60, 0, 160)
		tester.tt.Frame()
		if row, ok := tester.tt.Access("file-39.txt"); ok && row.Bounds.H > 0 {
			break
		}
	}
	row, ok := tester.tt.Access("file-39.txt")
	if !ok || row.Bounds.H == 0 {
		t.Fatalf("the last file did not scroll into the popover: %+v", row.Bounds)
	}
	if err := tester.tt.Click("file-39.txt"); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return strings.Contains(session.draft, "@file-39.txt")
	}, "the mention to be inserted")
}

// The / list is the same floating panel: a bounded height and a scroll
// for a long list of commands.
func TestUIComposerSlashPopoverScrolls(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Slash", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Slash thread", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 900, 600, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "connect")
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool { return session.detail.Thread.ID == thread.ID }, "thread")
	session.setDraft("/")
	tester.pump()
	if !tester.tt.HasText(i18n.T("Skills & commands")) {
		t.Fatalf("the / list is not a popover with its title: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText("/settings") {
		t.Fatalf("the slash commands are missing: %v", tester.tt.Texts())
	}
}
