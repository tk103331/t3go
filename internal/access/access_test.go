package access

import (
	"strings"
	"testing"
)

func TestNewMintsDistinctTokensThatKeepTheirDigest(t *testing.T) {
	first, firstDigest := New()
	second, _ := New()
	if !strings.HasPrefix(first, Prefix) || len(first) <= len(Prefix)+20 {
		t.Fatalf("token = %q, want a prefixed secret", first)
	}
	if first == second {
		t.Fatal("two paired devices share a token")
	}
	if firstDigest != Digest(first) {
		t.Fatal("a token does not match the digest kept for it")
	}
	if Digest(second) == firstDigest {
		t.Fatal("two devices share a digest")
	}
	if IsDevice("") || IsDevice(firstDigest) || !IsDevice(first) {
		t.Fatal("device tokens are not told apart from other secrets")
	}
}
