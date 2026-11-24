package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

func friendlyError(err error) string {
	if errors.Is(err, client.ErrUnauthorized) {
		return i18n.T("That token was rejected. Check the token and try again.")
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return i18n.T("No server is listening at that address.")
	case strings.Contains(msg, "no such host"):
		return i18n.T("That host could not be found.")
	case strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timeout"):
		return i18n.T("The server did not answer in time.")
	}
	return msg
}

func decodeData[T any](v any) (T, bool) {
	var out T
	if v == nil {
		return out, false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return out, false
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, false
	}
	return out, true
}

// upsertProto replaces a message with the same id, or appends it.
func upsertProto(list []proto.Message, message proto.Message) []proto.Message {
	next := make([]proto.Message, len(list), len(list)+1)
	copy(next, list)
	for i := range next {
		if next[i].ID == message.ID {
			next[i] = message
			return next
		}
	}
	return append(next, message)
}

// shortID is the human-sized prefix of an id or hash.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// folderName is the last part of a path from any platform.
func folderName(path string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(path), `/\`)
	if i := strings.LastIndexAny(trimmed, `/\`); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	return trimmed
}

// threadMatches reports whether a thread's title, prompt or branch matches
// the sidebar's search.
func threadMatches(t proto.Thread, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{t.Title, t.Summary, t.Branch, t.Provider, t.Model}, " "))
	return strings.Contains(haystack, query)
}

// relativeTime writes the compact age the sidebar shows.
func relativeTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	age := time.Since(at)
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	case at.Year() == time.Now().Year():
		return at.Local().Format("Jan 2")
	}
	return at.Local().Format("Jan 2006")
}

func plural(n int, singular, multiple string) string {
	if n == 1 {
		return singular
	}
	return multiple
}

// oneLine collapses whitespace and clips text to limit runes.
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > limit {
		return strings.TrimSpace(string(r[:limit-1])) + "…"
	}
	return s
}

// setClipboard puts text on the system clipboard.
func (s *Session) setClipboard(text string) {
	_ = mygo.Clipboard.Write(transfer.TextData(text))
}

// pickFiles opens the system's file picker and delivers the chosen paths,
// or nil when the picker was canceled, on the main thread.
func (s *Session) pickFiles(cb func([]string)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: s.win, Multiple: true})
		s.post(func() {
			if err != nil {
				cb(nil)
				return
			}
			cb(paths)
		})
	}()
}

// pickDirectories opens the system's folder picker and delivers the chosen
// paths, or nil when the picker was canceled, on the main thread.
func (s *Session) pickDirectories(cb func([]string)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: s.win, Directory: true})
		s.post(func() {
			if err != nil {
				cb(nil)
				return
			}
			cb(paths)
		})
	}()
}

// after schedules fn on the main thread after d and returns its cancel
// function, which reports whether the timer was stopped in time.
func (s *Session) after(d time.Duration, fn func()) func() bool {
	timer := time.AfterFunc(d, func() { s.post(fn) })
	return timer.Stop
}

// droppedPaths turns file:// URLs or paths from a drop into local paths.
func droppedPaths(uris []string) []string {
	var paths []string
	for _, uri := range uris {
		path := uri
		if u, err := url.Parse(uri); err == nil {
			if u.Scheme == "file" {
				path = u.Path
			} else if u.Scheme != "" {
				continue
			}
		}
		if unescaped, err := url.PathUnescape(path); err == nil {
			path = unescaped
		}
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			paths = append(paths, path)
		}
	}
	return paths
}

// mentionPaths reads the @ paths in the draft without duplicates.
func mentionPaths(draft string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, field := range strings.Fields(draft) {
		path := strings.TrimPrefix(field, "@")
		if path == field || path == "" {
			continue
		}
		path = strings.TrimRight(path, ".,;:!?)]")
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

// slashQuery reports the command being typed after a leading slash.
func slashQuery(draft string) (string, bool) {
	trimmed := strings.TrimSpace(draft)
	if !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\n") {
		return "", false
	}
	query := strings.TrimPrefix(trimmed, "/")
	if strings.ContainsAny(query, " \t") {
		return "", false
	}
	return query, true
}

// activeMention returns where a mention starts and what follows its @,
// while that is one word.
func activeMention(draft string) (int, string, bool) {
	at := strings.LastIndex(draft, "@")
	if at < 0 {
		return 0, "", false
	}
	if at > 0 {
		before := rune(draft[at-1])
		if !unicode.IsSpace(before) && before != '(' && before != '[' {
			return 0, "", false
		}
	}
	query := draft[at+1:]
	if strings.ContainsAny(query, " \t\n,)]") {
		return 0, "", false
	}
	return at, query, true
}

// humanSize writes a byte count in the largest fitting unit.
func humanSize(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	case size > 0:
		return fmt.Sprintf("%d B", size)
	}
	return "0 B"
}

// pickedFile is one file the app uploads to the environment: a file the
// user picked or dropped (Path set), or bytes pasted into the composer
// (Data set).
type pickedFile struct {
	Name      string
	MIME      string
	Size      int64
	Path      string
	Data      []byte
	Temporary bool
}

// open reads the file's contents.
func (f pickedFile) open() (io.ReadCloser, error) {
	if f.Data != nil {
		return io.NopCloser(strings.NewReader(string(f.Data))), nil
	}
	return os.Open(f.Path)
}

func filesFromPaths(paths []string) []pickedFile {
	files := make([]pickedFile, 0, len(paths))
	for _, path := range paths {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		files = append(files, pickedFile{Name: filepath.Base(path), MIME: mime.TypeByExtension(filepath.Ext(path)),
			Size: st.Size(), Path: path})
	}
	return files
}

// pastedFile is one file the composer received from the system clipboard.
type pastedFile struct {
	Name string
	MIME string
	Data []byte
}

// pasteEvent carries what the view received from a paste into the composer.
type pasteEvent struct {
	Files []pastedFile
	URIs  []string
}

// persistSplitWidths writes the sidebar's and the surfaces panel's widths
// back to the client's storage.
func (s *Session) persistSplitWidths() {
	_ = s.store.SetItem("sidebarWidth", strconv.FormatFloat(float64(s.sidebarWidth), 'f', -1, 32))
	_ = s.store.SetItem("rightWidth", strconv.FormatFloat(float64(s.rightWidth), 'f', -1, 32))
}
