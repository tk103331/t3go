//go:build !(js && wasm)

package app

import (
	"path/filepath"
	"runtime"
)

// openStorage opens the client's storage in the data directory the NSGo
// client used, so its profiles, drafts and preferences carry over.
func openStorage(id string) appStorage {
	dir := storageDirs(runtime.GOOS, id)
	return openFileStorage(filepath.Join(dir, "storage.json"))
}
