package client

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/server"
)

func newTestClient(t *testing.T) (*Client, *server.Server) {
	t.Helper()
	cfg, err := config.Load(t.TempDir(), "127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	srv, err := server.New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		srv.Close()
	})
	return New(ts.URL, cfg.Token), srv
}

func TestParsePairing(t *testing.T) {
	url, token, ok := ParsePairing("t3go://pair?url=http://127.0.0.1:4870&token=abc")
	if !ok || url != "http://127.0.0.1:4870" || token != "abc" {
		t.Fatalf("pair = %q %q %v", url, token, ok)
	}
	c := New("t3go://pair?url=example.test:4870&token=abc", "")
	if c.BaseURL != "http://example.test:4870" || c.Token != "abc" {
		t.Fatalf("client = %q %q", c.BaseURL, c.Token)
	}
	if _, _, ok := ParsePairing("http://example.test"); ok {
		t.Fatal("plain URL parsed as a pair link")
	}
}

func TestClientActions(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestClient(t)
	p, err := c.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projects, err := c.Projects(ctx)
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects = %d, %v", len(projects), err)
	}
	th, err := c.CreateThread(ctx, CreateThreadInput{ProjectID: p.ID, Title: "Client thread", Provider: "claude", Model: "test-model"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	detail, err := c.ThreadDetail(ctx, th.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Thread.ID != th.ID {
		t.Fatalf("detail thread = %q", detail.Thread.ID)
	}
	threads, err := c.Threads(ctx, p.ID)
	if err != nil || len(threads) != 1 {
		t.Fatalf("threads = %d, %v", len(threads), err)
	}
	path, _, dirs, err := c.ListDir(ctx, t.TempDir())
	if err != nil || path == "" || dirs == nil {
		t.Fatalf("list dir = %q %#v %v", path, dirs, err)
	}
}

func TestClientUnauthorized(t *testing.T) {
	good, _ := newTestClient(t)
	c := New(good.BaseURL, "wrong-token")
	if _, err := c.Projects(context.Background()); err != ErrUnauthorized {
		t.Fatalf("error = %v", err)
	}
}

func TestStreamDeliversEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _ := newTestClient(t)
	p, err := c.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := c.CreateThread(ctx, CreateThreadInput{ProjectID: p.ID, Provider: "claude", Model: "m"}); err != nil {
		t.Fatalf("thread: %v", err)
	}

	got := make(chan proto.Event, 8)
	go func() {
		_ = c.Stream(ctx, 0, func(e proto.Event) {
			select {
			case got <- e:
			default:
			}
		}, nil)
	}()
	for {
		select {
		case e := <-got:
			if e.Type == proto.EventThreadCreated && e.Seq > 0 {
				return
			}
		case <-ctx.Done():
			t.Fatal("no thread.created event")
		}
	}
}

func TestStreamRejectsBadToken(t *testing.T) {
	good, _ := newTestClient(t)
	good.Token = "wrong"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := good.Stream(ctx, 0, func(proto.Event) {}, func(error) {})
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("stream error = %v", err)
	}
}

// TestClientDirs lists folders on the environment's machine.
func TestClientDirs(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestClient(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	listing, err := c.Dirs(ctx, root)
	if err != nil {
		t.Fatalf("dirs: %v", err)
	}
	if listing.Path != root || listing.Parent != filepath.Dir(root) || len(listing.Entries) != 1 {
		t.Fatalf("listing = %#v", listing)
	}
	if listing.Entries[0].Name != "child" || listing.Entries[0].Path != filepath.Join(root, "child") {
		t.Fatalf("entry = %#v", listing.Entries[0])
	}
	listing, err = c.CreateDir(ctx, root, "created")
	if err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "created")); err != nil {
		t.Fatalf("created dir: %v", err)
	}
	home, err := c.Dirs(ctx, "")
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	if home.Home == "" || home.Path != home.Home {
		t.Fatalf("home = %#v", home)
	}
}
