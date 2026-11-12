// Package preview finds the local development servers a browser can open.
package preview

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// ErrUnavailable says the platform has no lsof to list sockets.
var ErrUnavailable = errors.New("cannot list local servers on this machine")

// List returns the listening TCP servers on this machine.
func List(ctx context.Context) ([]proto.PreviewServer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-u", strconv.Itoa(os.Getuid()),
		"-iTCP", "-sTCP:LISTEN", "-Fpcn").Output()
	if err != nil {
		return nil, ErrUnavailable
	}
	return Parse(out), nil
}

// Parse reads lsof's p/c/n field output.
func Parse(raw []byte) []proto.PreviewServer {
	var servers []proto.PreviewServer
	var current proto.PreviewServer
	seen := map[int]bool{}
	flush := func() {
		if current.Port > 0 && !seen[current.Port] {
			current.URL = "http://localhost:" + strconv.Itoa(current.Port)
			servers = append(servers, current)
			seen[current.Port] = true
		}
		current = proto.PreviewServer{}
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if len(line) < 2 {
			continue
		}
		value := line[1:]
		switch line[0] {
		case 'p':
			flush()
			current.PID, _ = strconv.Atoi(value)
		case 'c':
			current.Process = value
		case 'n':
			if port := addressPort(value); port > 0 {
				current.Address = value
				current.Port = port
				flush()
			}
		}
	}
	flush()
	sort.Slice(servers, func(i, j int) bool { return servers[i].Port < servers[j].Port })
	return servers
}

// addressPort reads the port from lsof's n field, as in 127.0.0.1:3000
// or [::1]:8080.
func addressPort(address string) int {
	if i := strings.LastIndexByte(address, ':'); i >= 0 {
		if port, err := strconv.Atoi(strings.TrimSpace(address[i+1:])); err == nil {
			return port
		}
	}
	return 0
}
