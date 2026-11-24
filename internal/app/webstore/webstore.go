//go:build js && wasm

// Package webstore keeps the client's entries in the browser's
// localStorage, so the web client remembers its environments, drafts and
// preferences across reloads.
package webstore

import (
	"syscall/js"
)

// Storage is the browser's localStorage, with each entry under the
// prefix the app's entries share.
type Storage struct {
	prefix string
}

// New returns the storage the entries of the app with this prefix live
// in.
func New(prefix string) *Storage {
	return &Storage{prefix: prefix}
}

// GetItem returns the value of a key.
func (s *Storage) GetItem(key string) (string, bool) {
	store := js.Global().Get("localStorage")
	if store.IsUndefined() || store.IsNull() {
		return "", false
	}
	value := store.Call("getItem", s.prefix+key)
	if value.IsNull() || value.IsUndefined() {
		return "", false
	}
	return value.String(), true
}

// SetItem keeps a value under a key.
func (s *Storage) SetItem(key, value string) error {
	store := js.Global().Get("localStorage")
	if store.IsUndefined() || store.IsNull() {
		return nil
	}
	store.Call("setItem", s.prefix+key, value)
	return nil
}

// RemoveItem drops a key.
func (s *Storage) RemoveItem(key string) {
	store := js.Global().Get("localStorage")
	if store.IsUndefined() || store.IsNull() {
		return
	}
	store.Call("removeItem", s.prefix+key)
}

// Close ends the storage. The browser's storage needs no closing.
func (s *Storage) Close() {}
