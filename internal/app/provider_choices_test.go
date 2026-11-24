package app

import (
	"testing"

	"github.com/tk/t3go/internal/proto"
)

func TestNewThreadProviderDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, configuredProvider, configuredModel, wantProvider, wantModel string
	}{
		{"provider default model", "codex", "", "codex", "preferred"},
		{"explicit custom model", "codex", "custom-model", "codex", "custom-model"},
		{"missing provider", "missing", "old-model", "codex", "preferred"},
		{"disabled provider", "disabled", "old-model", "codex", "preferred"},
		{"uninstalled provider", "claude", "old-model", "codex", "preferred"},
		{"no configured default", "", "", "codex", "preferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{
				settings: proto.Settings{DefaultProvider: tc.configuredProvider, DefaultModel: tc.configuredModel},
				providers: []proto.Provider{
					{ID: "claude", Source: "builtin", Enabled: true},
					{ID: "disabled", Source: "acp", Installed: true},
					{ID: "codex", Source: "builtin", Installed: true, Enabled: true},
				},
				models: []proto.Model{
					{ID: "first", Provider: "codex"},
					{ID: "preferred", Provider: "codex", Default: true},
				},
			}
			p, m := s.newThreadDefaults()
			if p != tc.wantProvider || m != tc.wantModel {
				t.Fatalf("selection = %s/%s, want %s/%s", p, m, tc.wantProvider, tc.wantModel)
			}
		})
	}
}
