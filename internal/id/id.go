// Package id makes the short, sortable identifiers T3Go stores.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// New returns a prefix and a time-ordered identifier, such as
// "thr_01k9x3h2v8a1b2c3d4e5f6g7".
func New(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("id: crypto/rand: %v", err))
	}
	// A millisecond timestamp first keeps identifiers in creation order
	// when they are sorted as text; the random tail keeps them unique.
	ms := uint64(time.Now().UnixMilli())
	var head [6]byte
	for i := 5; i >= 0; i-- {
		head[i] = byte(ms)
		ms >>= 8
	}
	return prefix + "_" + hex.EncodeToString(head[:]) + hex.EncodeToString(b[:])
}

// Valid reports whether s is a nonempty identifier.
func Valid(s string) bool {
	return s != "" && len(s) <= 128 && !strings.ContainsAny(s, " \t\r\n/\\")
}
