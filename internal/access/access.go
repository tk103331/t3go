// Package access mints the per-device tokens a paired client uses.
//
// The environment token stays the owner's key: it always works and is what
// the machine's own client uses. Each paired device gets its own token
// instead, so losing a phone means revoking that one grant rather than
// rotating the token every device shares.
package access

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// Prefix marks a device token, so a server can tell one from the
// environment token before it looks anything up.
const Prefix = "t3d_"

// New returns a device token and the digest a store keeps for it.
func New() (token, digest string) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("access: crypto/rand: " + err.Error())
	}
	token = Prefix + base64.RawURLEncoding.EncodeToString(raw[:])
	return token, Digest(token)
}

// Digest is what a store keeps instead of the token itself.
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IsDevice reports whether token is shaped like a device token.
func IsDevice(token string) bool { return strings.HasPrefix(token, Prefix) }
