package environment

import "testing"

func TestUsableName(t *testing.T) {
	for name, want := range map[string]bool{
		"":                   false,
		"bogon":              false,
		"host.bogon":         false,
		"localhost":          false,
		"LOCALHOST.":         false,
		"api.localhost":      false,
		"127.0.0.1":          false,
		"127.0.1.1:4870":     false,
		"http://[::1]/":      false,
		"::1":                false,
		"0.0.0.0":            false,
		"tiankaiMacBook-Pro": true,
		"VM-0-10-ubuntu":     true,
		"192.168.1.20":       true,
	} {
		if got := UsableName(name); got != want {
			t.Errorf("UsableName(%q) = %v, want %v", name, got, want)
		}
	}
}
