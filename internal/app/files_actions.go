package app

import (
	"context"
	"path"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// openFileEntry opens the file tree's dialog: a new file or folder in
// target, a rename of target, or its deletion.
func (s *Session) openFileEntry(mode, target, name string) {
	s.fileEntryMode, s.fileEntryTarget, s.fileEntryName = mode, target, name
	s.fileEntryOpen, s.fileEntryBusy, s.fileEntryErr = true, false, ""
}

// closeFileEntry dismisses the file tree's dialog.
func (s *Session) closeFileEntry() {
	s.fileEntryOpen, s.fileEntryMode, s.fileEntryErr = false, "", ""
}

// copyFileTreePath puts a checkout path on the clipboard.
func (s *Session) copyFileTreePath(entryPath string) {
	mygo.Clipboard.WriteText(entryPath)
}

// openFileExternally opens one file in the system browser or editor.
func (s *Session) openFileExternally(c *ui.Context, entryPath string) {
	if url := s.rawFileURL(entryPath); url != "" {
		c.OpenURL(url)
	}
}

// confirmFileEntry applies the file tree's open dialog.
func (s *Session) confirmFileEntry() {
	id := s.selected
	mode, target, name := s.fileEntryMode, s.fileEntryTarget, strings.TrimSpace(s.fileEntryName)
	if id == "" || mode == "" {
		return
	}
	if mode != "delete" && name == "" {
		s.fileEntryErr = i18n.T("Enter a name.")
		return
	}
	s.fileEntryBusy, s.fileEntryErr = true, ""
	s.call(func(ctx context.Context, c *client.Client) error {
		switch mode {
		case "file", "folder":
			return c.CreateWorkspaceEntry(ctx, id, target, name, mode == "folder")
		case "rename":
			to := name
			if dir := path.Dir(target); dir != "." && dir != "/" {
				to = dir + "/" + name
			}
			return c.RenameWorkspaceEntry(ctx, id, target, to)
		case "delete":
			return c.DeleteWorkspaceEntry(ctx, id, target)
		}
		return nil
	}, func(err error) {
		s.fileEntryBusy = false
		if err != nil {
			s.fileEntryErr = friendlyError(err)
			return
		}
		if mode == "rename" || mode == "delete" {
			if s.fileSelected == target {
				s.fileSelected, s.file = "", proto.WorkspaceFile{}
			}
		}
		s.closeFileEntry()
		s.loadFiles(s.filesPath)
	})
}

// fileEntryDialog draws the file tree's new, rename and delete dialogs.
func fileEntryDialog(c *ui.Context, s *Session) {
	if s.fileEntryMode == "" {
		return
	}
	t := c.Theme()
	modal(c, &s.fileEntryOpen, func() {
		dialogPanel(c, 380).Children(func() {
			title := i18n.T("New file")
			action := i18n.T("Create")
			switch s.fileEntryMode {
			case "folder":
				title = i18n.T("New folder")
			case "rename":
				title, action = i18n.T("Rename"), i18n.T("Rename")
			case "delete":
				title, action = i18n.T("Delete"), i18n.T("Delete")
			}
			text(c, title, 16, t.Text, 600)
			dir := s.fileEntryTarget
			if s.fileEntryMode == "file" || s.fileEntryMode == "folder" {
				if dir == "" {
					dir = "/"
				} else {
					dir = "/" + dir
				}
				faint(c, dir, 11.5)
				ui.Field(c, i18n.T("Name"), func() {
					ui.TextInput(c, &s.fileEntryName).Placeholder(i18n.T("Name")).AutoFocus().
						OnSubmit(func() { s.confirmFileEntry() })
				})
			} else if s.fileEntryMode == "rename" {
				ui.Field(c, i18n.T("Name"), func() {
					ui.TextInput(c, &s.fileEntryName).Placeholder(i18n.T("Name")).AutoFocus().
						OnSubmit(func() { s.confirmFileEntry() })
				})
			} else {
				ui.Text(c, i18n.T("Delete %s? This cannot be undone.", s.fileEntryTarget)).
					FontSize(12.5).TextColor(t.TextMuted)
			}
			if s.fileEntryErr != "" {
				errorNote(c, s.fileEntryErr)
			}
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).MinHeight(controlMinHeight(c)).Clicked() {
					s.closeFileEntry()
				}
				var b ui.Element
				if s.fileEntryMode == "delete" {
					b = ui.Button(c, "").Label(action).MinHeight(controlMinHeight(c)).Children(func() {
						ui.Icon(c, lucide.Trash()).FontSize(15).TextColor(t.Danger)
						ui.Text(c, action).TextColor(t.Danger)
					})
				} else {
					b = ui.PrimaryButton(c, "").Label(action).MinHeight(controlMinHeight(c)).Children(func() {
						ui.Text(c, action)
					})
				}
				if s.fileEntryBusy {
					b.Disabled(true)
				}
				b.OnClick(s.confirmFileEntry)
			})
		})
	})
}

// fileTreeMenu is one entry's context menu: open, rename, delete and the
// file-only actions.
func (s *Session) fileTreeMenu(c *ui.Context, entry proto.WorkspaceEntry) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if entry.Directory {
			if m.Item(i18n.T("Open")).Chosen() {
				s.loadFiles(entry.Path)
			}
			if m.Item(i18n.T("New file")).Chosen() {
				s.openFileEntry("file", entry.Path, "")
			}
			if m.Item(i18n.T("New folder")).Chosen() {
				s.openFileEntry("folder", entry.Path, "")
			}
		} else {
			if m.Item(i18n.T("Open")).Chosen() {
				s.openFile(entry.Path)
			}
			if m.Item(i18n.T("Open externally")).Chosen() {
				s.openFileExternally(c, entry.Path)
			}
		}
		m.Separator()
		if m.Item(i18n.T("Copy path")).Chosen() {
			s.copyFileTreePath(entry.Path)
		}
		if m.Item(i18n.T("Rename")).Chosen() {
			s.openFileEntry("rename", entry.Path, entry.Name)
		}
		if m.Item(i18n.T("Delete")).Chosen() {
			s.openFileEntry("delete", entry.Path, "")
		}
	}
}
