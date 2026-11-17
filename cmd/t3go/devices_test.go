package main

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/tk/t3go/internal/access"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/store"
)

// captureStdout runs fn and returns its exit code and what it printed.
func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = write
	code := fn()
	write.Close()
	os.Stdout = saved
	out, err := io.ReadAll(read)
	read.Close()
	if err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

// TestDevicesCLIMintsListsAndRevokesGrant pins the command-line pairing
// story: a device gets its own token, shows up in the list, and can be
// taken away again.
func TestDevicesCLIMintsListsAndRevokesGrant(t *testing.T) {
	t.Setenv("T3GO_TOKEN", "")
	home := t.TempDir()
	code, out := captureStdout(t, func() int {
		return cmdPair([]string{"--home", home, "--listen", "127.0.0.1:4870", "--device", "Pixel 8", "--json"})
	})
	if code != 0 {
		t.Fatalf("pair --device exited %d", code)
	}
	var paired struct {
		Token   string `json:"token"`
		Pairing string `json:"pairing"`
	}
	if err := json.Unmarshal([]byte(out), &paired); err != nil {
		t.Fatalf("pair output %q: %v", out, err)
	}
	if !access.IsDevice(paired.Token) {
		t.Fatalf("pair --device minted %q, want a device token", paired.Token)
	}
	link, err := url.Parse(paired.Pairing)
	if err != nil || link.Query().Get("token") != paired.Token || link.Query().Get("url") != "http://127.0.0.1:4870" {
		t.Fatalf("pairing link = %q: %v", paired.Pairing, err)
	}

	cfg, err := config.Load(home, "", false)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(config.DBPath(cfg.Home))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	devices, err := st.Devices(t.Context())
	if err != nil || len(devices) != 1 || devices[0].Label != "Pixel 8" || devices[0].Revoked() {
		t.Fatalf("devices = %+v, %v", devices, err)
	}
	deviceID := devices[0].ID

	code, out = captureStdout(t, func() int { return cmdDevices([]string{"list", "--home", home, "--json"}) })
	if code != 0 || !strings.Contains(out, deviceID) || !strings.Contains(out, "Pixel 8") {
		t.Fatalf("devices list = %d, %q", code, out)
	}
	if _, err := st.DeviceByDigest(t.Context(), access.Digest(paired.Token)); err != nil {
		t.Fatalf("the minted token does not authenticate: %v", err)
	}

	if code, _ := captureStdout(t, func() int { return cmdDevices([]string{"revoke", "--home", home, deviceID}) }); code != 0 {
		t.Fatalf("devices revoke exited %d", code)
	}
	if _, err := st.DeviceByDigest(t.Context(), access.Digest(paired.Token)); err == nil {
		t.Fatal("a revoked device still authenticates")
	}
	if code, _ := captureStdout(t, func() int { return cmdDevices([]string{"revoke", "--home", home, "dev_missing"}) }); code != 1 {
		t.Fatalf("revoking an unknown device exited %d", code)
	}
	if code, _ := captureStdout(t, func() int { return cmdDevices([]string{"--home", home, "nonsense"}) }); code != 2 {
		t.Fatalf("an unknown action exited %d", code)
	}
}
