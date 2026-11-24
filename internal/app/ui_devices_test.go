package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUIDevices lists a simulator, boots it and shows its screen, with a
// fake xcrun standing in for the environment's tools.
func TestUIDevices(t *testing.T) {
	dir := t.TempDir()
	for _, tool := range []string{"adb", "emulator"} {
		if err := os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(dir, "booted")
	logPath := filepath.Join(dir, "device.log")
	script := `#!/bin/sh
case "$2" in
list)
  if [ -f "$DEVICE_STATE" ]; then state=Booted; else state=Shutdown; fi
  printf '{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-17-0":[{"udid":"7E99F39F-299A-4E23-A2D0-7948BC3EE5F4","name":"iPhone 15 Pro","state":"%s","isAvailable":true}]}}\n' "$state"
  ;;
boot) touch "$DEVICE_STATE"; echo "boot $3" >> "$DEVICE_LOG" ;;
shutdown) rm -f "$DEVICE_STATE"; echo "shutdown $3" >> "$DEVICE_LOG" ;;
io) echo io >> "$DEVICE_LOG"; printf 'PNGDATA' > "$5" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "xcrun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVICE_STATE", state)
	t.Setenv("DEVICE_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Device", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	tester.wait(t, 15*time.Second, func() bool {
		return len(session.threads) == 1
	}, "the thread list to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID
	}, "the detail to load")

	// The device surface lists the simulator.
	session.showSurface("device")
	session.loadDevices()
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.devices) == 1
	}, "the simulator to arrive")
	tester.pump()
	if !tester.tt.HasText("iPhone 15 Pro") {
		t.Fatalf("the device surface does not list the simulator: %v", tester.tt.Texts())
	}

	// Selecting and booting it runs the environment's boot command.
	device := session.devices[0]
	session.selectDevice(device)
	session.bootDevice(device.ID)
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		raw, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(raw), "boot 7E99F39F")
	}, "the boot command to run")

	// A booted device refreshes its screen (the fake writes a shot).
	session.refreshDeviceShot()
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.deviceImage) > 0
	}, "the screenshot to arrive")
	tester.pump()

	// Shutting it down runs the environment's shutdown command.
	session.shutdownDevice(device.ID)
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		raw, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(raw), "shutdown 7E99F39F")
	}, "the shutdown command to run")
}

// TestUIProviderSignIn runs the provider's sign-in command in the open
// thread's terminal.
func TestUIProviderSignIn(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo 1.0.0 ;;\nauth) echo \"Not logged in\" >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Sign in", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	tester.wait(t, 15*time.Second, func() bool {
		return len(session.threads) == 1
	}, "the thread list to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID
	}, "the detail to load")

	// The providers page shows the signed-out CLI with a sign-in action.
	session.setSettingsSection("providers")
	session.refreshProviders()
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !session.providersBusy && len(session.providers) > 0
	}, "the providers to arrive")
	tester.pump()
	var claude proto.Provider
	for _, p := range session.providers {
		if p.ID == "claude" {
			claude = p
		}
	}
	if claude.ID == "" || claude.AuthStatus != "unauthenticated" || claude.LoginCommand == "" {
		t.Fatalf("provider = %#v, want a signed-out claude with a login command", claude)
	}
	if !tester.tt.HasText("Sign in") {
		t.Fatalf("the providers page does not show the sign-in action: %v", tester.tt.Texts())
	}

	// Running it opens the thread's terminal and runs the command there.
	session.runSettingsProviderCommand(session, claude.LoginCommand)
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(srv.Terminals.List(thread.ID)) > 0 && session.surface == "terminal"
	}, "the sign-in command to run in the terminal")
}
