package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/tk/t3go/internal/i18n"
)

// TestUIDirsBreadcrumbAndNewFolder walks the folder picker: the path it
// shows and the folder it creates.
func TestUIDirsBreadcrumbAndNewFolder(t *testing.T) {
	_, ts, cfg := startEnv(t)
	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")

	// A directory of the test's own, so the folder it creates is
	// removed with it.
	root := t.TempDir()
	session.showDirs = true
	session.loadDirs(root)
	tester.wait(t, 15*time.Second, func() bool {
		return session.dirs.Path != "" && !session.dirsBusy
	}, "the folder listing to arrive")
	tester.pump()
	if session.dirs.Path != root {
		t.Fatalf("the picker shows %q, want %q", session.dirs.Path, root)
	}
	// The path of the folder on screen has a way back to its parents.
	if !tester.tt.HasText(i18n.T("Parent folder")) {
		t.Fatalf("the folder picker has no parent folder: %v", tester.tt.Texts())
	}
	segments := strings.Split(strings.Trim(root, "/"), "/")
	if !tester.tt.HasText(segments[len(segments)-1]) {
		t.Fatalf("the folder picker does not show %q: %v", segments[len(segments)-1], tester.tt.Texts())
	}

	// A folder is created in the directory on screen.
	if err := tester.tt.Click(i18n.T("New folder")); err != nil {
		t.Fatalf("open the new folder form: %v", err)
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("Create folder")) {
		t.Fatalf("the new folder form did not open: %v", tester.tt.Texts())
	}
	if err := tester.tt.Click(i18n.T("Folder name")); err != nil {
		t.Fatalf("focus the folder name: %v", err)
	}
	tester.tt.Type("t3go-picker-folder")
	tester.pump()
	if err := tester.tt.Click(i18n.T("Create folder")); err != nil {
		t.Fatalf("create the folder: %v", err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.dirsErr == "" && len(session.dirs.Entries) > 0
	}, "the new folder to appear")
	if _, err := os.Stat(filepath.Join(root, "t3go-picker-folder")); err != nil {
		t.Fatalf("the folder was not created: %v", err)
	}
}

// TestPairingLinkFromQRImage reads a pairing code out of a picture, the
// way the client reads the picture a phone photographed.
func TestPairingLinkFromQRImage(t *testing.T) {
	link := "t3go://pair?url=http://127.0.0.1:4870&token=secret-token"
	picture, err := qrcode.Encode(link, qrcode.Medium, 320)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pairingLinkFromQRImage(picture)
	if err != nil {
		t.Fatalf("read the pairing code: %v", err)
	}
	if got != link {
		t.Fatalf("the code reads %q, want %q", got, link)
	}
	// A picture without a pairing code is refused.
	other, err := qrcode.Encode("not a pairing code", qrcode.Medium, 320)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pairingLinkFromQRImage(other); err == nil {
		t.Fatal("a picture without a pairing code was accepted")
	}
	if _, err := pairingLinkFromQRImage(nil); err == nil {
		t.Fatal("an empty picture was accepted")
	}
}
