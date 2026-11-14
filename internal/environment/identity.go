// Package environment defines stable execution-environment identities.
package environment

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// Descriptor identifies one environment independently of its address or process.
type Descriptor struct {
	ID           string   `json:"environmentId"`
	PublicKey    string   `json:"publicKey"`
	InstanceID   string   `json:"instanceId"`
	Name         string   `json:"name"`
	Challenge    string   `json:"challenge"`
	Capabilities []string `json:"capabilities"`
	Signature    string   `json:"signature"`
}

// Identity is persisted by the environment, never by a relay.
type Identity struct {
	ID         string
	PrivateKey ed25519.PrivateKey
}

func Generate() (*Identity, error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Identity{ID: base64.RawURLEncoding.EncodeToString(pub), PrivateKey: key}, nil
}

func (i *Identity) PublicKey() string {
	return base64.RawURLEncoding.EncodeToString(i.PrivateKey.Public().(ed25519.PublicKey))
}

func (i *Identity) Describe(instance, name, challenge string) Descriptor {
	d := Descriptor{ID: i.ID, PublicKey: i.PublicKey(), InstanceID: instance, Name: name,
		Challenge: challenge, Capabilities: []string{"stableIdentity", "signedProbe", "relayV1"}}
	raw, _ := json.Marshal(d)
	d.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(i.PrivateKey, raw))
	return d
}

// Verify checks a fresh challenge and the paired identity before credentials are sent.
// It authenticates the descriptor; TLS is still required to protect the connection.
func Verify(d Descriptor, challenge, expectedID, expectedKey string) error {
	pub, err := base64.RawURLEncoding.DecodeString(d.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize || d.ID != d.PublicKey ||
		d.Challenge != challenge || (expectedID != "" && d.ID != expectedID) ||
		(expectedKey != "" && d.PublicKey != expectedKey) {
		return errors.New("environment identity mismatch")
	}
	signature, err := base64.RawURLEncoding.DecodeString(d.Signature)
	if err != nil {
		return errors.New("invalid environment signature")
	}
	d.Signature = ""
	raw, _ := json.Marshal(d)
	if !ed25519.Verify(pub, raw, signature) {
		return errors.New("invalid environment signature")
	}
	return nil
}
