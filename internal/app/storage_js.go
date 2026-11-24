//go:build js && wasm

package app

import (
	"github.com/tk/t3go/internal/app/webstore"
)

// openStorage opens the browser's storage for the client. The prefix is
// the one the NSGo client used on the web, so the environments, drafts
// and preferences a browser already holds carry over to this client.
func openStorage(id string) appStorage {
	return webstore.New("nsgo:default:")
}
