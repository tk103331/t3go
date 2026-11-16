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
)

// jsonlPeer is a small JSON-RPC-like line protocol peer used by ACP.
// The protocol delimits messages with LF and uses JSON id fields.
type jsonlPeer struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	nextID  int64
	pending map[string]chan map[string]any
	err     error
	stderr  ringBuffer

	// handle receives notifications and peer-to-peer requests.
	handle func(map[string]any)
}

func startJSONLPeer(ctx context.Context, spec procSpec, handle func(map[string]any)) (*jsonlPeer, error) {
	childCtx, cancel := context.WithCancel(ctx)
	cmd := providerCommandContext(childCtx, spec.Path, spec.Args...)
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	cmd.Dir = spec.CWD
	setProcessGroup(cmd)
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	p := &jsonlPeer{cmd: cmd, stdin: stdin, cancel: cancel, done: make(chan struct{}),
		pending: map[string]chan map[string]any{}, handle: handle}
	p.stderr.limit = 32 << 10
	go p.readStderr(stderr)
	go p.readStdout(stdout)
	go func() {
		select {
		case <-childCtx.Done():
			killProcessGroup(cmd)
		case <-p.done:
		}
	}()
	return p, nil
}

func (p *jsonlPeer) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 32<<10), 1<<20)
	for sc.Scan() {
		p.stderr.write(append(sc.Bytes(), '\n'))
	}
}

func (p *jsonlPeer) readStdout(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 32<<20)
	for sc.Scan() {
		var msg map[string]any
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		key := jsonID(msg["id"])
		if key != "" {
			p.mu.Lock()
			ch := p.pending[key]
			delete(p.pending, key)
			p.mu.Unlock()
			if ch != nil {
				ch <- msg
				continue
			}
		}
		if p.handle != nil {
			p.handle(msg)
		}
	}
	p.finish()
}

func (p *jsonlPeer) finish() {
	err := p.cmd.Wait()
	if err == nil {
		err = io.EOF
	}
	detail := strings.TrimSpace(p.stderr.String())
	if detail != "" {
		err = fmt.Errorf("%w: %s", err, lastLines(detail, 12))
	}
	p.mu.Lock()
	if p.err == nil {
		p.err = err
	}
	for key, ch := range p.pending {
		delete(p.pending, key)
		close(ch)
	}
	p.mu.Unlock()
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}

func (p *jsonlPeer) request(ctx context.Context, payload map[string]any, timeout time.Duration) (map[string]any, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	p.mu.Lock()
	if p.err != nil {
		err := p.err
		p.mu.Unlock()
		return nil, err
	}
	p.nextID++
	id := fmt.Sprint(p.nextID)
	payload["id"] = id
	ch := make(chan map[string]any, 1)
	p.pending[id] = ch
	p.mu.Unlock()

	if err := p.write(payload); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, err
	}
	select {
	case response, ok := <-ch:
		if !ok {
			p.mu.Lock()
			err := p.err
			p.mu.Unlock()
			if err == nil {
				err = errors.New("provider process exited")
			}
			return nil, err
		}
		return response, nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (p *jsonlPeer) write(payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	_, err = p.stdin.Write(append(raw, '\n'))
	return err
}

func (p *jsonlPeer) respond(id any, result any, rpcErr any) error {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	return p.write(msg)
}

func (p *jsonlPeer) notify(method string, params any) error {
	return p.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (p *jsonlPeer) close() {
	_ = p.stdin.Close()
	p.cancel()
	killProcessGroup(p.cmd)
	select {
	case <-p.done:
	case <-time.After(1500 * time.Millisecond):
	}
}

func jsonID(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return fmt.Sprint(value)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}
