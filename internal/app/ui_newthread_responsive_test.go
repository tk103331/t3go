package app

import (
	"context"
	"testing"
	"time"

	"github.com/tk/t3go/internal/i18n"
)

// The new thread's choices share the Create thread row on a wide page and
// take their own row above it when the page is narrow, instead of mixing
// a wrapped permission pill with the actions.
func TestUINewThreadResponsiveControls(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	project, err := srv.Service.CreateProject(context.Background(), "Desktop workspace", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		w, h   int
		oneRow bool
	}{
		{"wide", 1440, 900, true},
		{"narrow", 780, 760, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tester, session := startAppAt(t, ts, cfg, tc.w, tc.h, nil)
			tester.wait(t, 15*time.Second, func() bool {
				return session != nil && session.connected && len(session.projects) == 1
			}, "connect")
			session.openNewThreadIn(project.ID)
			tester.wait(t, 15*time.Second, func() bool {
				tester.post.drain()
				tester.tt.Frame()
				return !session.newThreadBranchesBusy
			}, "branches")
			tester.pump()

			create, ok := tester.tt.Access(i18n.T("Create thread"))
			if !ok || create.Bounds.W < 26 || create.Bounds.H < 28 ||
				create.Bounds.X+create.Bounds.W > float32(tc.w) {
				t.Fatalf("the create action is clipped: %+v", create.Bounds)
			}
			controls := []string{
				i18n.T("Agent") + " " + session.newThreadAgentLabel(),
				i18n.T("Model"),
				session.newThreadPermissionOption().pillLabel(),
			}
			for _, label := range controls {
				control, ok := tester.tt.Access(label)
				if !ok || control.Bounds.W < 40 || control.Bounds.H < 28 {
					t.Fatalf("%s is missing or clipped: %+v", label, control.Bounds)
				}
				if tc.oneRow {
					if !verticalOverlap(control.Bounds, create.Bounds) {
						t.Fatalf("%s is not on the Create thread row: %+v vs %+v",
							label, control.Bounds, create.Bounds)
					}
					continue
				}
				if control.Bounds.Y+control.Bounds.H > create.Bounds.Y {
					t.Fatalf("%s did not take its own row above the actions: %+v vs %+v",
						label, control.Bounds, create.Bounds)
				}
			}
		})
	}
}
