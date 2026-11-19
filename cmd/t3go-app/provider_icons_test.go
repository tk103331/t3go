package main

import (
	"testing"

	"github.com/oneo-me/nsgo/core"
)

// TestProviderMarksParse pins that every agent mark the app ships is an SVG
// the renderer can draw; a broken one would quietly fall back to a robot.
func TestProviderMarksParse(t *testing.T) {
	marks := map[string]string{
		"claude":   providerClaudeSVG,
		"codex":    providerCodexSVG,
		"opencode": providerOpenCodeSVG,
		"gemini":   providerGeminiSVG,
		"qwen":     providerQwenSVG,
		"copilot":  providerCopilotSVG,
		"amp":      providerAmpSVG,
	}
	for name, raw := range marks {
		if _, err := core.ParseSVG([]byte(raw)); err != nil {
			t.Errorf("%s mark does not parse: %v", name, err)
		}
	}
}

// TestProviderFamilyGroupsIds pins which ids draw which mark: the built-in
// providers, the ACP agents they install, and ids the app has no mark for.
func TestProviderFamilyGroupsIds(t *testing.T) {
	for id, want := range map[string]string{
		"claude":             "claude",
		"claude-code-acp":    "claude",
		"anthropic-cli":      "claude",
		"codex":              "codex",
		"codex-acp":          "codex",
		"openai":             "codex",
		"opencode":           "opencode",
		"opencode-v2":        "opencode",
		"gemini":             "gemini",
		"gemini-cli":         "gemini",
		"qwen-code":          "qwen",
		"github-copilot-cli": "copilot",
		"amp-acp":            "amp",
		"goose":              "",
		"brand-new-agent":    "",
	} {
		if got := providerFamily(id); got != want {
			t.Errorf("providerFamily(%q) = %q, want %q", id, got, want)
		}
	}
}
