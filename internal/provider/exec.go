package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// procSpec is a provider process to start.
type procSpec struct {
	Path string
	Args []string
	CWD  string
}

// procResult is what a finished provider process left.
type procResult struct {
	Stderr string
	Err    error
	Lines  int
}

// runJSON starts spec and hands every stdout line to onLine. stderr is
// kept in a bounded tail for error messages. Cancelling ctx kills the
// process and its children.
func runJSON(ctx context.Context, spec procSpec, stdin string, onLine func([]byte)) procResult {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir = spec.CWD
	cmd.Stdin = strings.NewReader(stdin)
	setProcessGroup(cmd)
	hideWindow(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return procResult{Err: err}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return procResult{Err: err}
	}
	if err := cmd.Start(); err != nil {
		return procResult{Err: err}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessGroup(cmd)
		case <-done:
		}
	}()

	var tail ringBuffer
	tail.limit = 32 << 10
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := bufio.NewReaderSize(stderr, 32<<10)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				tail.write(line)
			}
			if err != nil {
				return
			}
		}
	}()

	lines := 0
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		lines++
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		onLine(append([]byte(nil), line...))
	}
	scanErr := sc.Err()
	err = cmd.Wait()
	close(done)
	wg.Wait()
	if err == nil && scanErr != nil && !errors.Is(scanErr, io.EOF) {
		err = scanErr
	}
	if ctx.Err() != nil {
		return procResult{Stderr: tail.String(), Err: ctx.Err(), Lines: lines}
	}
	if err != nil {
		msg := strings.TrimSpace(tail.String())
		if msg == "" {
			msg = err.Error()
		}
		return procResult{Stderr: tail.String(), Err: fmt.Errorf("%s", lastLines(msg, 12)), Lines: lines}
	}
	return procResult{Stderr: tail.String(), Lines: lines}
}

// lastLines keeps the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// ringBuffer is a bounded byte buffer.
type ringBuffer struct {
	buf   []byte
	limit int
}

func (r *ringBuffer) write(p []byte) {
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.limit {
		r.buf = r.buf[len(r.buf)-r.limit:]
	}
}

func (r *ringBuffer) String() string { return string(r.buf) }

// versionOf runs a CLI's version flag.
func versionOf(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// detect finds a CLI and probes its version.
func detect(ctx context.Context, id, name, binary string) proto.Provider {
	path, err := exec.LookPath(binary)
	if err != nil {
		return proto.Provider{ID: id, Name: name, Error: "not installed"}
	}
	p := proto.Provider{ID: id, Name: name, Installed: true, Path: path, Version: versionOf(ctx, path)}
	return p
}

// jsonString reads a string field from a decoded JSON map.
func jsonString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// jsonMap reads an object field.
func jsonMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

// jsonNumber reads a number field.
func jsonNumber(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

// marshalDetail renders a tool input compactly.
func marshalDetail(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	s := string(b)
	if len(s) > 4000 {
		s = s[:4000] + "…"
	}
	return s
}
