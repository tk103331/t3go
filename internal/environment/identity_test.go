package environment

import "testing"

func TestSignedProbeRejectsReplayAndSubstitution(t *testing.T) {
	identity, _ := Generate()
	d := identity.Describe("instance", "desktop", "fresh-nonce")
	if err := Verify(d, "fresh-nonce", identity.ID, identity.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := Verify(d, "different-nonce", identity.ID, identity.PublicKey()); err == nil {
		t.Fatal("replayed probe accepted")
	}
	other, _ := Generate()
	if err := Verify(d, "fresh-nonce", other.ID, other.PublicKey()); err == nil {
		t.Fatal("substituted identity accepted")
	}
	d.Name = "tampered"
	if err := Verify(d, "fresh-nonce", identity.ID, identity.PublicKey()); err == nil {
		t.Fatal("tampered descriptor accepted")
	}
}
