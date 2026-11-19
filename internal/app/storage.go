package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// appStorage is the client's key-value storage. The MyGo client keeps the
// same keys, and reads and writes the same storage.json file the NSGo
// client used, so profiles, drafts and preferences carry over.
type appStorage interface {
	GetItem(string) (string, bool)
	SetItem(string, string) error
	RemoveItem(string)
}

// fileStore keeps every entry in one JSON file, written through a single
// goroutine so bursts of changes cost one or two writes.
type fileStore struct {
	mu      sync.Mutex
	path    string
	entries map[string]string
	loading bool
	writes  chan struct{}
	done    chan struct{}
}

// storageDirs returns the app's data directory for id, as the NSGo client
// picked it: per-OS user data directories with a directory named by id.
func storageDirs(goos, id string) (data string) {
	// A dev script or test can point the client's storage at its own
	// directory.
	if dir := strings.TrimSpace(os.Getenv("T3GO_CLIENT_DATA")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch goos {
	case "darwin":
		data = filepath.Join(home, "Library", "Application Support")
	case "windows":
		data = os.Getenv("APPDATA")
	default:
		data = os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
	}
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00") {
		id = "default"
	}
	return filepath.Join(data, id)
}

// openStorage opens the storage file the NSGo client used for the app id,
// so its entries carry over to this client.
func openStorage(id string) *fileStore {
	dir := storageDirs(runtime.GOOS, id)
	return openFileStorage(filepath.Join(dir, "storage.json"))
}

func openFileStorage(path string) *fileStore {
	s := &fileStore{path: path, entries: map[string]string{}, writes: make(chan struct{}, 1), done: make(chan struct{})}
	s.load()
	go s.flusher()
	return s
}

// memoryStorage keeps entries in one map, for tests.
func memoryStorage(entries map[string]string) *fileStore {
	if entries == nil {
		entries = map[string]string{}
	}
	return &fileStore{entries: entries, writes: make(chan struct{}, 1), done: make(chan struct{})}
}

func (s *fileStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var entries map[string]string
	if json.Unmarshal(raw, &entries) == nil {
		s.entries = entries
	}
}

func (s *fileStore) GetItem(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.entries[key]
	return v, ok
}

func (s *fileStore) SetItem(key, value string) error {
	s.mu.Lock()
	s.entries[key] = value
	s.mu.Unlock()
	s.schedule()
	return nil
}

func (s *fileStore) RemoveItem(key string) {
	s.mu.Lock()
	delete(s.entries, key)
	s.mu.Unlock()
	s.schedule()
}

func (s *fileStore) schedule() {
	select {
	case s.writes <- struct{}{}:
	default:
	}
}

func (s *fileStore) flusher() {
	for {
		select {
		case <-s.writes:
			s.write()
		case <-s.done:
			return
		}
	}
}

func (s *fileStore) write() {
	s.mu.Lock()
	data, err := json.Marshal(s.entries)
	dir := filepath.Dir(s.path)
	s.mu.Unlock()
	if err != nil {
		log.Printf("app: the client's storage could not be encoded: %v", err)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("app: the client's storage could not be written: %v", err)
		return
	}
	tmp := s.path + ".tmp"
	file, err := os.Create(tmp)
	if err == nil {
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		if cerr := file.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp, s.path)
		}
	}
	if err != nil {
		os.Remove(tmp)
		log.Printf("app: the client's storage could not be written: %v", err)
	}
}

// Close stops the writer, writing what is left.
func (s *fileStore) Close() {
	s.write()
	close(s.done)
}

// environmentStorage isolates execution-specific UI data. A migrated first
// environment mirrors legacy keys so older app versions retain their profile.
type environmentStorage struct {
	base   appStorage
	id     string
	legacy bool
}

func scopedKey(key string) bool {
	switch key {
	case "serverUrl", "token", "customModels", "attachmentDrafts", "drafts", "pinnedThreads",
		"sidebarCollapsed", "selectedThread", "pendingMessages", "editorChoice", "lastThreadProject":
		return true
	}
	return false
}

func (s environmentStorage) key(key string) string {
	if s.id != "" && scopedKey(key) {
		return "env:" + s.id + ":" + key
	}
	return key
}

func (s environmentStorage) GetItem(key string) (string, bool) {
	if value, ok := s.base.GetItem(s.key(key)); ok {
		return value, true
	}
	if s.legacy && scopedKey(key) {
		return s.base.GetItem(key)
	}
	return "", false
}

func (s environmentStorage) SetItem(key, value string) error {
	if err := s.base.SetItem(s.key(key), value); err != nil {
		return err
	}
	if s.legacy && scopedKey(key) && s.id != "" {
		return s.base.SetItem(key, value)
	}
	return nil
}

func (s environmentStorage) RemoveItem(key string) {
	s.base.RemoveItem(s.key(key))
	if s.legacy && scopedKey(key) {
		s.base.RemoveItem(key)
	}
}

// sharedStore returns the storage every environment shares: the client's
// appearance preference and language live there, not in a session's own
// scoped keys.
func (s *Session) sharedStore() appStorage {
	if s.manager != nil {
		return s.manager.store
	}
	return s.store
}

// storedValue decodes the JSON entry key holds, or returns fallback.
func storedValue[T any](store appStorage, key string, fallback T) T {
	if raw, ok := store.GetItem(key); ok {
		var value T
		if json.Unmarshal([]byte(raw), &value) == nil && strings.TrimSpace(raw) != "null" {
			return value
		}
	}
	return fallback
}
