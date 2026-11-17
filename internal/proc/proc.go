// Package proc starts the child processes T3Go runs on a project's behalf.
package proc

import (
	"os"
	"os/exec"
	"strings"
)

// Dir makes cmd run in dir, and gives it the PWD that matches.
//
// Go only changes the child's working directory. A child that trusts the
// environment's PWD instead of asking the kernel, as OpenCode does to pick
// its project, would otherwise keep the directory the environment server
// itself was started in, so agents would work on the wrong project.
func Dir(cmd *exec.Cmd, dir string) {
	cmd.Dir = dir
	if dir == "" {
		return
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	kept := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, "PWD=") {
			continue
		}
		kept = append(kept, entry)
	}
	cmd.Env = append(kept, "PWD="+dir)
}
