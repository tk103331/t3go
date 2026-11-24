//go:build js && wasm

package webstore

import (
	"syscall/js"
	"testing"
)

// installLocalStorage gives the test a localStorage to write in, as the
// browser has.
func installLocalStorage(t *testing.T) {
	t.Helper()
	js.Global().Call("eval", `globalThis.localStorage = (() => {
		const entries = new Map();
		return {
			getItem: (k) => (entries.has(k) ? entries.get(k) : null),
			setItem: (k, v) => entries.set(k, String(v)),
			removeItem: (k) => entries.delete(k),
			key: (i) => Array.from(entries.keys())[i] ?? null,
			get length() { return entries.size; },
		};
	})();`)
}

// TestStorage keeps the client's entries in the browser's localStorage,
// under the prefix the app's entries share.
func TestStorage(t *testing.T) {
	installLocalStorage(t)
	store := New("nsgo:default:")
	store.SetItem("serverUrl", "http://127.0.0.1:4870")
	store.SetItem("token", "secret-token")
	value, ok := store.GetItem("token")
	if !ok || value != "secret-token" {
		t.Fatalf("the token reads %q, want %q", value, "secret-token")
	}
	// The entries sit under the prefix the old client used, so a browser
	// that already knows the environment carries it over.
	raw := js.Global().Get("localStorage").Call("getItem", "nsgo:default:token").String()
	if raw != "secret-token" {
		t.Fatalf("the browser holds %q, want %q", raw, "secret-token")
	}
	store.RemoveItem("token")
	if _, ok := store.GetItem("token"); ok {
		t.Fatal("the token was not removed")
	}
	if _, ok := store.GetItem("token"); ok {
		t.Fatal("the token came back")
	}
	store.Close()
}
