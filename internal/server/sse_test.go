package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/tk/t3go/internal/proto"
)

func TestSSERequiresResyncOutsideReplayWindow(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	for range 6 {
		e.server.Bus.Publish(ctx, proto.Event{Type: proto.EventSettings})
	}
	e.server.Bus.Prune(ctx, 2)
	for _, cursor := range []int64{-1, 1, 7} {
		t.Run(fmt.Sprint(cursor), func(t *testing.T) {
			var out struct {
				Error struct{ Code string } `json:"error"`
			}
			req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/events?after=%d", e.http.URL, cursor), nil)
			req.Header.Set("Authorization", "Bearer "+e.token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusConflict || out.Error.Code != "resync_required" {
				t.Fatalf("cursor %d: status %d, code %q", cursor, resp.StatusCode, out.Error.Code)
			}
		})
	}
	for _, cursor := range []int64{0, 4, 6} {
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/events?after=%d", e.http.URL, cursor), nil)
		req.Header.Set("Authorization", "Bearer "+e.token)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("valid cursor %d rejected: %d", cursor, resp.StatusCode)
		}
	}
}
