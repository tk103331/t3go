package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/proto"
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

// TestProviderMarksFetchKeepAndReuse pins the registry marks: an agent the
// app does not ship is fetched once, kept in local storage, drawn from
// memory afterwards, and not fetched again by a restart.
func TestProviderMarksFetchKeepAndReuse(t *testing.T) {
	const mark = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path fill="currentColor" d="M2 2h12v12H2z"/></svg>`
	var fetched atomic.Int32
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched.Add(1)
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, mark)
	}))
	defer registry.Close()
	address := registry.URL + "/goose.svg"

	var instance *app
	surface := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.Untrack(func() { instance = newApp(cx) })
		return core.Dynamic(func(cx *core.Scope) core.Node {
			return core.View{Children: []core.Node{
				ui.Icon(instance.providerMarks.icon(address), 16, ui.ColorsOf(cx).Muted)}}
		})
	}, nsgotest.Size(200, 200), nsgotest.Storage(map[string]string{}))
	surface.WaitUntil(5*time.Second, func() bool { return instance.providerMarks.parsed(address) != nil })
	if got := fetched.Load(); got != 1 {
		t.Fatalf("the mark was fetched %d times", got)
	}
	raw, ok := surface.Storage().GetItem(providerMarkStorageKey)
	if !ok || !strings.Contains(raw, address) {
		t.Fatalf("the mark was not kept: %q", raw)
	}

	// The same storage serves the mark to a fresh cache without a fetch.
	var restarted *app
	restored := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.Untrack(func() { restarted = newApp(cx) })
		return core.Text{Content: "restored"}
	}, nsgotest.Size(200, 200), nsgotest.Storage(map[string]string{providerMarkStorageKey: raw}))
	restored.WaitUntil(5*time.Second, func() bool { return restarted != nil })
	if restarted.providerMarks.parsed(address) == nil {
		t.Fatal("a kept mark was fetched again after a restart")
	}
	if got := fetched.Load(); got != 1 {
		t.Fatalf("the mark was fetched %d times after a restart", got)
	}
}

// TestProviderIconURLNamesRegistryMarks pins where an unsent mark comes
// from: the environment's own report, its ACP configuration, or nothing.
func TestProviderIconURLNamesRegistryMarks(t *testing.T) {
	var instance *app
	probe := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.Untrack(func() {
			instance = newApp(cx)
			instance.providers.Set([]proto.Provider{{ID: "amp", Icon: "https://example.com/amp.svg"}})
			instance.settings.Set(proto.Settings{ACPProviders: []proto.ACPProviderSettings{
				{ID: "acp-goose", AgentID: "goose"},
			}})
		})
		return core.Text{Content: "probe"}
	}, nsgotest.Size(200, 200))
	probe.WaitUntil(5*time.Second, func() bool { return instance != nil })
	if got := instance.providerIconURL("amp"); got != "https://example.com/amp.svg" {
		t.Fatalf("amp mark = %q", got)
	}
	if got, want := instance.providerIconURL("acp-goose"), registryIconURL("goose"); got != want {
		t.Fatalf("goose mark = %q, want %q", got, want)
	}
	if got := instance.providerIconURL("unknown-agent"); got != "" {
		t.Fatalf("an unknown agent's mark = %q", got)
	}
}

// TestProviderMarkFetchable pins the addresses a mark is fetched from.
func TestProviderMarkFetchable(t *testing.T) {
	for address, want := range map[string]bool{
		"https://cdn.agentclientprotocol.com/registry/v1/latest/goose.svg": true,
		"http://127.0.0.1:9000/goose.svg":                                  true,
		"http://localhost:9000/goose.svg":                                  true,
		"http://relay.example.com/goose.svg":                               false,
		"ftp://cdn.agentclientprotocol.com/goose.svg":                      false,
		"file:///etc/passwd":                                               false,
		"":                                                                 false,
	} {
		if got := providerMarkFetchable(address); got != want {
			t.Errorf("providerMarkFetchable(%q) = %t, want %t", address, got, want)
		}
	}
}
