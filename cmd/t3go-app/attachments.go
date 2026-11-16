package main

import (
	"context"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/asuka/sdk/device"
	"github.com/egoist/asuka/sdk/transcribe"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/proto"
)

// pickAttachments opens the platform document picker, uploads the chosen
// files to the environment and keeps them with the current thread's draft.
func (a *app) pickAttachments() {
	if a.selected.Peek() == "" {
		a.notice.Set("Open a thread before attaching files.")
		return
	}
	device.PickFiles(device.FileOptions{Multiple: true}, func(files []device.File, err error) {
		if err != nil {
			a.attachmentErr.Set(friendlyError(err))
			return
		}
		if len(files) > 0 {
			a.uploadAttachments(files)
		}
	})
}

func (a *app) pickPhotos() {
	if a.selected.Peek() == "" {
		a.notice.Set("Open a thread before attaching photos.")
		return
	}
	device.PickPhotos(4, func(paths []string, err error) {
		if err != nil {
			a.attachmentErr.Set(friendlyError(err))
			return
		}
		files := make([]device.File, 0, len(paths))
		for _, path := range paths {
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue
			}
			files = append(files, device.File{
				Path: path,
				Name: filepath.Base(path),
				Type: mimeTypeOf(path),
				Size: info.Size(),
			})
		}
		if len(files) > 0 {
			a.uploadAttachments(files)
		}
	})
}

func mimeTypeOf(path string) string {
	if value := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); value != "" {
		return value
	}
	return "application/octet-stream"
}

func (a *app) uploadAttachments(files []device.File) {
	c := a.client()
	threadID := a.selected.Peek()
	if c == nil || threadID == "" {
		return
	}
	a.attachmentBusy.Set(true)
	a.attachmentErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 2*time.Minute)
		defer cancel()
		var uploaded []proto.Attachment
		for _, picked := range files {
			file, err := os.Open(picked.Path)
			if err != nil {
				ui.Post(func() {
					if a.client() == c {
						a.attachmentErr.Set(friendlyError(err))
					}
				})
				continue
			}
			attachment, err := c.Upload(ctx, picked.Name, picked.Type, file)
			_ = file.Close()
			if err != nil {
				ui.Post(func() {
					if a.client() == c {
						a.attachmentErr.Set(friendlyError(err))
					}
				})
				continue
			}
			uploaded = append(uploaded, attachment)
		}
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.attachmentBusy.Set(false)
			if a.selected.Peek() == threadID && len(uploaded) > 0 {
				current := append(a.attachments.Peek(), uploaded...)
				a.attachments.Set(current)
				a.attachmentDrafts[threadID] = current
			}
		})
	}()
}

func (a *app) removeAttachment(id string) {
	threadID := a.selected.Peek()
	current := a.attachments.Peek()
	next := make([]proto.Attachment, 0, len(current))
	for _, attachment := range current {
		if attachment.ID != id {
			next = append(next, attachment)
		}
	}
	a.attachments.Set(next)
	if threadID != "" {
		if len(next) == 0 {
			delete(a.attachmentDrafts, threadID)
		} else {
			a.attachmentDrafts[threadID] = next
		}
	}
	c := a.client()
	if c == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		_ = c.DeleteUpload(ctx, id)
	}()
}

func (a *app) transcribeAudio() {
	device.PickFiles(device.FileOptions{Types: []string{"audio/*"}}, func(files []device.File, err error) {
		if err != nil {
			a.notice.Set(friendlyError(err))
			return
		}
		if len(files) == 0 {
			return
		}
		file := files[0]
		a.notice.Set("Transcribing audio on this device…")
		go func() {
			result, err := transcribe.File(file.Path, transcribe.Options{})
			ui.Post(func() {
				if err != nil {
					a.notice.Set(friendlyError(err))
					return
				}
				draft := strings.TrimSpace(a.draft.Peek())
				if draft != "" {
					draft += "\n"
				}
				a.draft.Set(draft + strings.TrimSpace(result.Text))
				a.notice.Set("")
			})
		}()
	})
}
