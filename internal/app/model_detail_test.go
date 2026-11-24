package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

func TestThreadDetailPreservesNewerEvents(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	now := time.Now()
	history := proto.Message{ID: "history", ThreadID: "thread", Content: "Historical message", CreatedAt: now}
	old := proto.Message{ID: "answer", ThreadID: "thread", Content: "Partial", CreatedAt: now.Add(time.Second)}
	snapshot := proto.ThreadDetail{Thread: proto.Thread{ID: "thread", Title: "Old title"}, Messages: []proto.Message{history, old}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
			_ = json.NewEncoder(w).Encode(snapshot)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { unblock(); ts.Close() })
	post := &testPost{}
	mgr := newManagerForTest(post, memoryStorage(nil))
	s := mgr.newSession(client.EnvironmentProfile{})
	t.Cleanup(s.disposeEnvironmentSession)
	s.cli, s.selected = client.New(ts.URL, ""), "thread"
	s.loadDetail("thread")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("detail request did not start")
	}
	newer := old
	newer.Content = "Complete streamed answer"
	s.applyEvent(proto.Event{Type: proto.EventThreadUpdated, ThreadID: "thread", Data: proto.Thread{ID: "thread", Title: "Latest title"}})
	s.applyEvent(proto.Event{Type: proto.EventMessage, ThreadID: "thread", Data: newer})
	s.applyEvent(proto.Event{Type: proto.EventRunStarted, ThreadID: "thread"})
	unblock()
	tester := &Tester{post: post, tt: ui.NewTester(func(c *ui.Context) {
		for _, message := range s.messages {
			ui.Text(c, message.Content)
		}
	}, 800, 600)}
	tester.wait(t, 5*time.Second, func() bool {
		return len(s.messages) == 2
	}, "historical messages to merge with newer events")
	if s.messages[0].ID != history.ID || s.messages[1].Content != newer.Content || s.detail.Thread.Title != "Latest title" || !s.running {
		t.Fatal("a stale detail response overwrote newer messages, title or running state")
	}
}

func TestThreadDetailIgnoresOlderRequests(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var requests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title := "Newest response"
		if requests.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				title = "Older response"
			case <-r.Context().Done():
				return
			}
		}
		_ = json.NewEncoder(w).Encode(proto.ThreadDetail{Thread: proto.Thread{ID: "thread", Title: title}})
	}))
	t.Cleanup(func() { unblock(); ts.Close() })
	post := &testPost{}
	mgr := newManagerForTest(post, memoryStorage(nil))
	applied := 0
	mgr.post = func(fn func()) { post.Update(func() { fn(); applied++ }) }
	s := mgr.newSession(client.EnvironmentProfile{})
	t.Cleanup(s.disposeEnvironmentSession)
	s.cli, s.selected = client.New(ts.URL, ""), "thread"
	s.loadDetail("thread")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first detail request did not start")
	}
	s.loadDetail("thread")
	tester := &Tester{post: post, tt: ui.NewTester(func(c *ui.Context) { ui.Text(c, s.detail.Thread.Title) }, 800, 600)}
	tester.wait(t, 5*time.Second, func() bool { return applied == 1 }, "newer detail request")
	unblock()
	tester.wait(t, 5*time.Second, func() bool { return applied == 2 }, "older detail request")
	if s.detail.Thread.Title != "Newest response" {
		t.Fatal("an older detail request replaced the latest response")
	}
}
