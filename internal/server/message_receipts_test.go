package server

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
)

func TestMessageReceiptAPI(t *testing.T) {
	e := newTestEnv(t)
	e.server.Service.Providers.Register(recordingRunner{prompts: make(chan string, 8)})
	project := e.project(t)
	thread := e.threadWithProvider(t, project.ID, "record-fake", "record-fake-1")
	var state proto.State
	e.request(t, http.MethodGet, "/api/v1/state", nil, e.token, &state)
	if !slices.Contains(state.Capabilities, proto.CapabilityIdempotentMessages) {
		t.Fatal("missing capability")
	}
	path := "/api/v1/threads/" + thread.ID
	body := map[string]any{"content": "one delivery", "requestId": "send_once"}
	var first, duplicate proto.Run
	if code := e.request(t, http.MethodPost, path+"/messages", body, e.token, &first); code != http.StatusAccepted {
		t.Fatalf("send: %d", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.server.Service.Wait(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if code := e.request(t, http.MethodPost, path+"/messages", body, e.token, &duplicate); code != http.StatusAccepted || duplicate.ID != first.ID {
		t.Fatalf("retry: %d, %#v", code, duplicate)
	}
	body["content"] = "different delivery"
	if code := e.request(t, http.MethodPost, path+"/messages", body, e.token, nil); code != http.StatusConflict {
		t.Fatalf("conflict: %d", code)
	}
	for _, invalid := range []string{"..", ".", "invalid/key"} {
		body["requestId"] = invalid
		if code := e.request(t, http.MethodPost, path+"/messages", body, e.token, nil); code != http.StatusBadRequest {
			t.Fatalf("invalid key: %d", code)
		}
	}
	receiptPath := path + "/receipts/send_once"
	if code := e.request(t, http.MethodGet, receiptPath, nil, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated receipt: %d", code)
	}
	var receipt proto.MessageReceipt
	if code := e.request(t, http.MethodGet, receiptPath, nil, e.token, &receipt); code != http.StatusOK || receipt.RunID != first.ID || receipt.RequestID != "send_once" {
		t.Fatalf("receipt: %d, %#v", code, receipt)
	}
	other := e.threadWithProvider(t, project.ID, "record-fake", "record-fake-1")
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+other.ID+"/receipts/send_once", nil, e.token, nil); code != http.StatusNotFound {
		t.Fatalf("receipt crossed threads: %d", code)
	}
}
