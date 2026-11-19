package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tk/t3go/internal/proto"
)

func registryPlatformKey() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	return runtime.GOOS + "-" + arch
}

func writeRegistryCache(t *testing.T, agents []proto.ACPRegistryAgent) string {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "cache", "acp-registry", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(acpRegistryDocument{Version: "1.0.0", Agents: agents})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestACPRegistryAcceptsSemanticDocumentVersion(t *testing.T) {
	var document acpRegistryDocument
	if err := json.Unmarshal([]byte(`{"version":"1.0.0","agents":[]}`), &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != "1.0.0" {
		t.Fatalf("version = %q", document.Version)
	}
}

func TestACPRegistrySearchFiltersPlatformAndPinnedPackages(t *testing.T) {
	home := writeRegistryCache(t, []proto.ACPRegistryAgent{
		{
			ID: "npx-pinned", Name: "NPX Pinned", Version: "1.2.3",
			Distribution: proto.ACPRegistryDistribution{
				NPX: &proto.ACPPackageDistribution{Package: "@scope/agent@1.2.3"},
			},
		},
		{
			ID: "uvx-pinned", Name: "UVX Pinned", Version: "2.0.0",
			Distribution: proto.ACPRegistryDistribution{
				UVX: &proto.ACPPackageDistribution{Package: "agent-acp==2.0.0"},
			},
		},
		{
			ID: "binary-compatible", Name: "Binary Compatible", Version: "3.0.0",
			Distribution: proto.ACPRegistryDistribution{Binary: map[string]proto.ACPBinaryDistribution{
				registryPlatformKey(): {Archive: "https://example.test/agent.tar.gz", CMD: "./agent"},
			}},
		},
		{
			ID: "binary-incompatible", Name: "Binary Incompatible", Version: "4.0.0",
			Distribution: proto.ACPRegistryDistribution{Binary: map[string]proto.ACPBinaryDistribution{
				"plan9-vax": {Archive: "https://example.test/agent.tar.gz", CMD: "./agent"},
			}},
		},
		{
			ID: "floating", Name: "Floating Package", Version: "5.0.0",
			Distribution: proto.ACPRegistryDistribution{
				NPX: &proto.ACPPackageDistribution{Package: "@scope/agent@latest"},
			},
		},
	})
	agents, err := ACPRegistrySearch(context.Background(), home, "", false)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{"npx-pinned": true, "uvx-pinned": true, "binary-compatible": true}
	if len(agents) != len(expected) {
		t.Fatalf("agents = %#v", agents)
	}
	for _, agent := range agents {
		if !expected[agent.ID] {
			t.Fatalf("unexpected incompatible or floating agent %#v", agent)
		}
	}
}

func TestPinnedPackageRequiresExactRegistryVersion(t *testing.T) {
	for _, test := range []struct {
		pkg, version string
		python, want bool
	}{
		{"agent@1.2.3", "1.2.3", false, true},
		{"@scope/agent@1.2.3", "1.2.3", false, true},
		{"agent@1.2.4", "1.2.3", false, false},
		{"agent@latest", "1.2.3", false, false},
		{"agent-acp==1.2.3", "1.2.3", true, true},
		{"agent-acp>=1.2.3", "1.2.3", true, false},
	} {
		if got := pinnedPackage(test.pkg, test.version, test.python); got != test.want {
			t.Errorf("pinnedPackage(%q, %q, %v) = %v, want %v", test.pkg, test.version, test.python, got, test.want)
		}
	}
}

func TestACPRegistryInstallPathsStayInsideAgentRoot(t *testing.T) {
	home := t.TempDir()
	agent := proto.ACPRegistryAgent{ID: "../outside", Version: "1.0.0"}
	if _, err := installedACPPath(home, agent, proto.ACPBinaryDistribution{CMD: "./agent"}); err == nil {
		t.Fatal("unsafe agent ID was accepted")
	}
	agent = proto.ACPRegistryAgent{ID: "safe", Version: "1.0.0"}
	if _, err := installedACPPath(home, agent, proto.ACPBinaryDistribution{CMD: "../../outside"}); err == nil {
		t.Fatal("escaping command path was accepted")
	}
}

// TestACPAgentIconNamesTheRegistryMark pins where an agent's mark is
// published: the catalog's own address when it is on disk, else the
// registry's path for that agent, which needs no fetch.
func TestACPAgentIconNamesTheRegistryMark(t *testing.T) {
	home := t.TempDir()
	if got, want := ACPAgentIcon(home, "goose"), acpRegistryIconBase+"goose.svg"; got != want {
		t.Fatalf("icon without a catalog = %q, want %q", got, want)
	}
	if got := ACPAgentIcon(home, "  "); got != "" {
		t.Fatalf("an unnamed agent's icon = %q", got)
	}

	cache := filepath.Join(home, "cache", "acp-registry")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	catalog := `{"version":"1.0.0","agents":[{"id":"goose","name":"Goose","version":"1.0.0","icon":"https://example.com/goose.svg"}]}`
	if err := os.WriteFile(filepath.Join(cache, "registry.json"), []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := ACPAgentIcon(home, "goose"), "https://example.com/goose.svg"; got != want {
		t.Fatalf("icon from the catalog = %q, want %q", got, want)
	}
	if got, want := ACPAgentIcon(home, "other"), acpRegistryIconBase+"other.svg"; got != want {
		t.Fatalf("an agent the catalog lacks = %q, want %q", got, want)
	}
}
