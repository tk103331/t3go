package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func composerControlsEnv(t *testing.T) (*Tester, *Session) {
	t.Helper()
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Composer", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Composer controls", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 1440, 900, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "connect")
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool { return session.detail.Thread.ID == thread.ID }, "thread")
	session.detail.Runs = []proto.Run{{Usage: &proto.Usage{TotalTokens: 1000, ContextWindow: 200000}}}
	return tester, session
}

func verticalOverlap(a, b ui.Rect) bool {
	return a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

// A narrow middle pane drops the token reading before it lets the model,
// config and send controls wrap onto another line.
func TestUIComposerDropsTokensBeforeWrapping(t *testing.T) {
	tester, session := composerControlsEnv(t)
	session.rightOpen, session.rightWidth, session.surface, session.tabs = true, 700, "", nil
	tester.pump()
	for _, label := range tester.tt.Texts() {
		if strings.HasSuffix(label, i18n.T(" left")) {
			t.Fatalf("the token reading still competes for room in a narrow pane: %q", label)
		}
	}
	thread := session.detail.Thread
	model, ok := tester.tt.Access(i18n.T("Choose model: ") + session.modelDisplayName(thread.Provider, thread.Model))
	if !ok {
		t.Fatalf("the model control is missing: %v", tester.tt.Texts())
	}
	config, ok := tester.tt.Access(i18n.T("Config"))
	if !ok {
		t.Fatalf("the config control is missing: %v", tester.tt.Texts())
	}
	send, ok := tester.tt.Access(i18n.T("Send"))
	if !ok {
		t.Fatalf("the send control is missing: %v", tester.tt.Texts())
	}
	if !verticalOverlap(model.Bounds, send.Bounds) || !verticalOverlap(config.Bounds, send.Bounds) {
		t.Fatalf("controls wrapped though the token reading is gone: model %+v, config %+v, send %+v",
			model.Bounds, config.Bounds, send.Bounds)
	}
	if send.Bounds.W > 30 || send.Bounds.H > 30 {
		t.Fatalf("the desktop send button did not shrink: %+v", send.Bounds)
	}
}

// A wide middle pane keeps the token reading and still shows it on the
// same line as the controls.
func TestUIComposerKeepsTokensWhenWide(t *testing.T) {
	tester, _ := composerControlsEnv(t)
	tester.pump()
	found := false
	for _, label := range tester.tt.Texts() {
		if strings.HasSuffix(label, i18n.T(" left")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the wide composer hides the token reading: %v", tester.tt.Texts())
	}
}
