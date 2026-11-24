package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func filesActionsEnv(t *testing.T) (*Tester, *Session, string) {
	t.Helper()
	return filesActionsEnvWithRepo(t, gitRepo(t))
}

func filesActionsEnvWithRepo(t *testing.T, repo string) (*Tester, *Session, string) {
	t.Helper()
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Files", repo)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Files thread", Provider: "side-fake", Model: "side-fake-1",
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
	session.showSurface("files")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !session.rightBusy && len(session.filesEntries) > 0
	}, "the files to load")
	return tester, session, repo
}

func filesEntryNamed(session *Session, name string) bool {
	for _, entry := range session.filesEntries {
		if entry.Name == name {
			return true
		}
	}
	return false
}

// The file tree's toolbar adds a file and a folder through its dialogs.
func TestUIFilesToolbarCreatesEntries(t *testing.T) {
	tester, session, repo := filesActionsEnv(t)

	if err := tester.tt.Click(i18n.T("New file or folder")); err != nil {
		t.Fatal(err)
	}
	if err := tester.tt.ChooseMenuItem(i18n.T("New file")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("New file")) {
		t.Fatal("the new file dialog did not open")
	}
	tester.tt.Type("created.txt")
	if err := tester.tt.Click(i18n.T("Create")); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return filesEntryNamed(session, "created.txt")
	}, "the new file to appear")
	if _, err := os.Stat(filepath.Join(repo, "created.txt")); err != nil {
		t.Fatalf("the server did not create the file: %v", err)
	}

	if err := tester.tt.Click(i18n.T("New file or folder")); err != nil {
		t.Fatal(err)
	}
	if err := tester.tt.ChooseMenuItem(i18n.T("New folder")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	tester.tt.Type("newdir")
	if err := tester.tt.Click(i18n.T("Create")); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return filesEntryNamed(session, "newdir")
	}, "the new folder to appear")
	info, err := os.Stat(filepath.Join(repo, "newdir"))
	if err != nil || !info.IsDir() {
		t.Fatalf("the server did not create the folder: %v", err)
	}
}

// Rename and delete run from the same dialogs the context menu opens.
func TestUIFilesRenameAndDelete(t *testing.T) {
	tester, session, repo := filesActionsEnv(t)
	session.openFileEntry("rename", "README.md", "README.md")
	tester.pump()
	tester.tt.Key(ui.Cmd, ui.KeyA)
	tester.tt.Type("renamed.md")
	clickDialogButton(t, tester, i18n.T("Rename"))
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return filesEntryNamed(session, "renamed.md")
	}, "the file to rename")
	if _, err := os.Stat(filepath.Join(repo, "renamed.md")); err != nil {
		t.Fatalf("the server did not rename the file: %v", err)
	}

	session.openFileEntry("delete", "renamed.md", "")
	tester.pump()
	clickDialogButton(t, tester, i18n.T("Delete"))
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !filesEntryNamed(session, "renamed.md")
	}, "the file to be deleted")
	if _, err := os.Stat(filepath.Join(repo, "renamed.md")); !os.IsNotExist(err) {
		t.Fatalf("the server did not delete the file: %v", err)
	}
}

// Right-clicking a file offers the tree's actions, including the
// file-only ones.
func TestUIFilesContextMenu(t *testing.T) {
	tester, session, _ := filesActionsEnv(t)
	if err := tester.tt.RightClick("README.md"); err != nil {
		t.Fatal(err)
	}
	menu := tester.tt.Menu()
	for _, want := range []string{i18n.T("Open"), i18n.T("Open externally"), i18n.T("Copy path"),
		i18n.T("Rename"), i18n.T("Delete")} {
		found := false
		for _, label := range menu {
			if label == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("the file menu is missing %q: %v", want, menu)
		}
	}
	if err := tester.tt.ChooseMenuItem(i18n.T("Rename")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	if session.fileEntryMode != "rename" || session.fileEntryTarget != "README.md" {
		t.Fatalf("the Rename item opened %q for %q", session.fileEntryMode, session.fileEntryTarget)
	}
	session.closeFileEntry()
}

// Double-clicking a file opens it in its own tab, beside the tree.
func TestUIFilesDoubleClickOpensFileTab(t *testing.T) {
	tester, session, _ := filesActionsEnv(t)
	if err := tester.tt.Click("README.md"); err != nil {
		t.Fatal(err)
	}
	if err := tester.tt.Click("README.md"); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.surface == "file" && session.fileTabPath == "README.md" &&
			!session.fileTabBusy && session.fileTab.Content != ""
	}, "the file tab to load")
	tester.pump()
	if title := session.surfaceTitle("file"); title != "README.md" {
		t.Fatalf("the file tab is titled %q", title)
	}
	if !tester.tt.HasText("README.md") || !tester.tt.HasText("demo") {
		t.Fatalf("the file tab does not show the file: %v", tester.tt.Texts())
	}
}

// A code file's tab shows the source in a laid-out editor, not a blank
// pane with only its size known.
func TestUIFilesCodeTabShowsSource(t *testing.T) {
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "main.go"),
		[]byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tester, session, _ := filesActionsEnvWithRepo(t, repo)
	if err := tester.tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	if err := tester.tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.surface == "file" && session.fileTabPath == "main.go" &&
			!session.fileTabBusy && strings.Contains(session.fileTab.Content, "package main")
	}, "the code file tab to load")
	tester.pump()
	found := false
	for _, a := range tester.tt.AccessAll() {
		if strings.Contains(a.Value, "package main") && a.Bounds.W > 100 && a.Bounds.H > 100 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the code editor has no visible area: %v", tester.tt.Texts())
	}
}

// The file tab keeps its editor across frames, so a selection survives,
// and offers find and line wrapping.
func TestUIFileTabSelectionWrapAndFind(t *testing.T) {
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "main.go"),
		[]byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tester, session, _ := filesActionsEnvWithRepo(t, repo)
	session.openFileTab("main.go")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.surface == "file" && session.fileView != nil && !session.fileTabBusy
	}, "the file editor")
	tester.pump()

	// A selection made in the editor is still there a frame later, as the
	// state is not rebuilt under it.
	session.fileView.Select(0, len("package"))
	tester.pump()
	if got := session.fileView.SelectedText(); got != "package" {
		t.Fatalf("the selection did not survive: %q", got)
	}

	// Find opens the editor's find bar, and searches the file.
	if err := tester.tt.Click(i18n.T("Find in file")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	if !tester.tt.HasText("Find") {
		t.Fatalf("the find bar did not open: %v", tester.tt.Texts())
	}
	session.fileView.Find("main")
	tester.pump()
	if count, _ := session.fileView.FindMatches(); count == 0 {
		t.Fatal("finding a word in the file found no matches")
	}

	// Wrap toggles the editor's soft wrap and rebuilds it once.
	view := session.fileView
	if err := tester.tt.Click(i18n.T("Wrap")); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 5*time.Second, func() bool {
		tester.tt.Frame()
		return !session.fileWrap
	}, "line wrapping to turn off")
	if session.fileView == view {
		t.Fatal("turning wrap off did not rebuild the editor")
	}
	if err := tester.tt.Click(i18n.T("Wrap")); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 5*time.Second, func() bool {
		tester.tt.Frame()
		return session.fileWrap
	}, "line wrapping to turn back on")
}

// clickDialogButton clicks the button a dialog labels, where its title
// carries the same words as a text element.
func clickDialogButton(t *testing.T, tester *Tester, name string) {
	t.Helper()
	for _, a := range tester.tt.AccessAll() {
		if a.Name == name && a.Role == ui.RoleButton && a.Bounds.W > 0 && a.Bounds.H > 0 {
			tester.tt.ClickAt(a.Bounds.X+a.Bounds.W/2, a.Bounds.Y+a.Bounds.H/2)
			return
		}
	}
	t.Fatalf("dialog button %q is missing", name)
}
