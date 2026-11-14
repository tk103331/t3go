package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
)

func TestStreamResyncsOnConflictOrLiveGap(t *testing.T) {
	for _, mode := range []string{"conflict", "gap"} {
		t.Run(mode, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "conflict" {
					w.WriteHeader(http.StatusConflict)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"seq\":11,\"type\":\"first\"}\n\n")
				fmt.Fprint(w, "data: {\"seq\":11,\"type\":\"duplicate\"}\n\n")
				fmt.Fprint(w, "data: {\"seq\":13,\"type\":\"gap\"}\n\n")
			}))
			defer ts.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var events []proto.Event
			var reported error
			err := New(ts.URL, "").Stream(ctx, 10, func(e proto.Event) {
				events = append(events, e)
			}, func(err error) { reported = err })
			if !errors.Is(err, ErrResyncRequired) || !errors.Is(reported, ErrResyncRequired) {
				t.Fatalf("resync not reported: %v, %v", err, reported)
			}
			if mode == "gap" && (len(events) != 1 || events[0].Seq != 11) {
				t.Fatalf("duplicate or out-of-sequence event applied: %#v", events)
			}
		})
	}
}

func TestStreamDeadlinesAndQuietReadiness(t *testing.T) {
	for _, mode := range []string{"headers", "idle", "heartbeats"} {
		t.Run(mode, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "headers" {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				if mode == "idle" {
					<-r.Context().Done()
					return
				}
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-r.Context().Done():
						return
					case <-ticker.C:
						fmt.Fprint(w, ": heartbeat\n\n")
						w.(http.Flusher).Flush()
					}
				}
			}))
			defer ts.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 220*time.Millisecond)
			defer cancel()
			c := New(ts.URL, "")
			c.HTTP.Timeout = 20 * time.Millisecond // Must not bound a healthy stream's lifetime.
			c.StreamHeaderTimeout = 60 * time.Millisecond
			c.StreamIdleTimeout = 60 * time.Millisecond
			ready := 0
			err := c.streamOnce(ctx, 0, func() { ready++ }, func(proto.Event) { t.Error("heartbeat dispatched as event") })
			switch mode {
			case "headers":
				if !errors.Is(err, ErrStreamHeadersTimeout) || ready != 0 {
					t.Fatalf("header deadline: %v, ready=%d", err, ready)
				}
			case "idle":
				if !errors.Is(err, ErrStreamIdleTimeout) || ready != 1 {
					t.Fatalf("idle deadline: %v, ready=%d", err, ready)
				}
			case "heartbeats":
				if !errors.Is(err, context.DeadlineExceeded) || ready != 1 {
					t.Fatalf("healthy quiet stream: %v, ready=%d", err, ready)
				}
			}
		})
	}
}

func TestStreamAuthorizationStopsReconnecting(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var requests atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(code)
			}))
			defer ts.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := New(ts.URL, "").Stream(ctx, 0, nil, nil)
			if !IsAuthorizationError(err) || requests.Load() != 1 {
				t.Fatalf("auth retried: %v, %d requests", err, requests.Load())
			}
		})
	}
}
