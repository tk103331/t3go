package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

func TestDeliverPromptRetriesSameRequestAfterLostResponse(t *testing.T) {
	var requestIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/threads/thread/receipts/send_retry":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"receipt not found"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/threads/thread/messages":
			var body struct {
				RequestID string `json:"requestId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			requestIDs = append(requestIDs, body.RequestID)
			w.Header().Set("Content-Type", "application/json")
			if len(requestIDs) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":{"code":"tunnel","message":"environment connection interrupted"}}`))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(proto.Run{ID: "run_retry"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recovered, err := deliverPrompt(ctx, client.New(server.URL, ""), "thread",
		queuedPrompt{Text: "retry me", RequestID: "send_retry"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered {
		t.Fatal("retry was not reported as recovered")
	}
	if len(requestIDs) != 2 || requestIDs[0] != "send_retry" || requestIDs[1] != requestIDs[0] {
		t.Fatalf("request IDs = %#v", requestIDs)
	}
}

func TestDeliverPromptUsesReceiptWithoutResending(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			posts++
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"code":"tunnel","message":"response lost"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/threads/thread/receipts/send_receipt":
			_ = json.NewEncoder(w).Encode(proto.MessageReceipt{
				RequestID: "send_receipt", RunID: "run_existing", AcceptedAt: time.Now(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recovered, err := deliverPrompt(ctx, client.New(server.URL, ""), "thread",
		queuedPrompt{Text: "already accepted", RequestID: "send_receipt"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered || posts != 1 {
		t.Fatalf("recovered=%v posts=%d", recovered, posts)
	}
}

func TestDeliverPromptDoesNotRetryRejectedRequest(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"run","message":"message is empty"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := deliverPrompt(ctx, client.New(server.URL, ""), "thread",
		queuedPrompt{Text: "rejected", RequestID: "send_rejected"}, nil, true)
	if err == nil {
		t.Fatal("rejected request unexpectedly succeeded")
	}
	if posts != 1 {
		t.Fatalf("rejected request retried %d times", posts)
	}
}
