package proc

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDirGivesTheChildThePWDThatMatchesIt(t *testing.T) {
	t.Setenv("PWD", "/somewhere/else")
	dir := t.TempDir()
	cmd := exec.Command("sh", "-c", "printf '%s\\n' \"$PWD\"")
	Dir(cmd, dir)
	if cmd.Dir != dir {
		t.Fatalf("Dir = %q, want %q", cmd.Dir, dir)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Fatalf("child PWD = %q, want %q", got, dir)
	}
	if pwd := countPrefix(cmd.Env, "PWD="); pwd != 1 {
		t.Fatalf("the child environment has %d PWD entries", pwd)
	}
}

func TestDirKeepsAnEmptyDirectory(t *testing.T) {
	t.Setenv("PWD", "/kept")
	cmd := exec.Command("true")
	Dir(cmd, "")
	if cmd.Dir != "" {
		t.Fatalf("Dir = %q, want the inherited one", cmd.Dir)
	}
	cmd.Env = []string{"PWD=/kept"}
	Dir(cmd, "")
	if got := countPrefix(cmd.Env, "PWD=/kept"); got != 1 {
		t.Fatal("an empty directory touched the environment")
	}
}

func countPrefix(env []string, prefix string) int {
	n := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			n++
		}
	}
	return n
}

func TestDirKeepsTheRestOfTheEnvironment(t *testing.T) {
	cmd := exec.Command("true")
	cmd.Env = []string{"PATH=/bin", "T3GO_TEST_MARKER=1"}
	Dir(cmd, "/tmp")
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "PATH=/bin") || !strings.Contains(env, "T3GO_TEST_MARKER=1") {
		t.Fatalf("environment lost entries: %v", cmd.Env)
	}
}
