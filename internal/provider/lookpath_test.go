package provider

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLookPathFindsCommonInstallDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix install layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")

	nvmDir := filepath.Join(home, ".nvm", "versions", "node", "v22.18.0", "bin")
	if err := os.MkdirAll(nvmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeFakeCLI(t, nvmDir, "claude", "echo 1.0.0")

	got, err := lookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("lookPath = %q, want %q", got, want)
	}
}

func TestProviderEnvironmentIncludesFallbackCLIDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix install layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")

	env := providerEnvironment()
	var path string
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	for _, want := range []string{
		filepath.Join(home, ".opencode", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		"/opt/homebrew/bin",
		"/usr/bin",
	} {
		if !strings.Contains(path, want) {
			t.Fatalf("provider PATH %q does not contain %q", path, want)
		}
	}
}
