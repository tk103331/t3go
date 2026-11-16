package relay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
)

const (
	HeaderRelayEnvironment = "X-T3Go-Relay-Environment"
	HeaderRelayTimestamp   = "X-T3Go-Relay-Timestamp"
	HeaderRelaySignature   = "X-T3Go-Relay-Signature"
)

// SignRelayRequest authenticates a hub-originated request to its environment.
func SignRelayRequest(key, method, environmentID string, timestamp int64, requestURI string) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(method + "\n" + environmentID + "\n" +
		strconv.FormatInt(timestamp, 10) + "\n" + requestURI))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyRelayRequest checks a hub-originated request without exposing the
// environment token to the Relay or account client.
func VerifyRelayRequest(key, method, environmentID string, timestamp int64, requestURI, signature string) bool {
	expected := SignRelayRequest(key, method, environmentID, timestamp, requestURI)
	return hmac.Equal([]byte(expected), []byte(signature))
}
