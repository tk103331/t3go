package provider

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// TestRunJSONUsesTheTurnDirectory pins the environment a provider CLI
// starts with: OpenCode picks its project from PWD rather than asking the
// kernel, so an inherited PWD would point every agent run at the directory
// the environment server itself was started in. env prints its environment
// untouched, where a shell would reset PWD from the working directory and
// hide that.
func TestRunJSONUsesTheTurnDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PWD is a Unix convention")
	}
	dir := t.TempDir()
	t.Setenv("PWD", "/the/server/was/started/here")
	var entries []string
	res := runJSON(context.Background(), procSpec{Path: "/usr/bin/env", CWD: dir}, "", func(line []byte) {
		if text := string(line); strings.HasPrefix(text, "PWD=") {
			entries = append(entries, text)
		}
	})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(entries) != 1 || entries[0] != "PWD="+dir {
		t.Fatalf("the provider saw %q, want one PWD=%s", entries, dir)
	}
}
