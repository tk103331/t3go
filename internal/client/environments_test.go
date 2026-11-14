package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/relay"
)

func TestRouteAuthorizationRejectionDoesNotTryOtherRoutes(t *testing.T) {
	good, srv := newTestClient(t)
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var fallbackRequests atomic.Int32
			rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/state" {
					w.WriteHeader(code)
					return
				}
				srv.Handler().ServeHTTP(w, r)
			}))
			defer rejected.Close()
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackRequests.Add(1)
				srv.Handler().ServeHTTP(w, r)
			}))
			defer fallback.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			profile := EnvironmentProfile{ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: good.Token,
				Routes: []Route{{URL: rejected.URL, Kind: "direct"}, {URL: fallback.URL, Kind: "relay"}}}
			_, _, err := ConnectEnvironment(ctx, profile)
			if !IsAuthorizationError(err) || fallbackRequests.Load() != 0 {
				t.Fatalf("authorization bypassed via another route: %v, %d requests", err, fallbackRequests.Load())
			}
			if code == http.StatusUnauthorized && !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("lost auth error: %v", err)
			}
		})
	}
}

func TestRouteSelectionPinsIdentityBeforeSendingCredentials(t *testing.T) {
	good, srv := newTestClient(t)
	var credentialLeak atomic.Bool
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			credentialLeak.Store(true)
		}
		http.Error(w, "wrong environment", 404)
	}))
	defer wrong.Close()
	p := EnvironmentProfile{ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: good.Token,
		Routes: []Route{{URL: wrong.URL, Kind: "direct"}, {URL: good.BaseURL, Kind: "relay"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, st, err := ConnectEnvironment(ctx, p)
	if err != nil || c.BaseURL != good.BaseURL || st.EnvironmentID != p.ID {
		t.Fatal("relay fallback failed", err)
	}
	if credentialLeak.Load() {
		t.Fatal("credentials sent to an unverified route")
	}
	p.Routes = []Route{{URL: good.BaseURL, Kind: "direct"}, {URL: wrong.URL, Kind: "relay"}}
	c, _, err = ConnectEnvironment(ctx, p)
	if err != nil || c.BaseURL != good.BaseURL {
		t.Fatal("direct route not preferred", err)
	}
	p.Preference = "relay-only"
	if _, _, err = ConnectEnvironment(ctx, p); err == nil {
		t.Fatal("forced relay mode ignored")
	}
}

func TestEnvironmentClientThroughReverseTunnel(t *testing.T) {
	local, srv := newTestClient(t)
	const admin = "test-hub-administrator-credentials"
	hub, err := relay.New(t.TempDir(), admin)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	hub.Mount(mux)
	cloud := httptest.NewServer(mux)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(func() { cancel(); hub.Close(); cloud.Close() })
	raw, _ := json.Marshal(relay.Registration{ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: local.Token})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cloud.URL+"/api/v1/relay/environments", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var credentials relay.Registered
	err = json.NewDecoder(resp.Body).Decode(&credentials)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("registration: %d, %v", resp.StatusCode, err)
	}
	connector := relay.Connector{Server: cloud.URL, EnvironmentID: srv.Identity.ID, Token: credentials.ConnectToken,
		Target: strings.TrimPrefix(local.BaseURL, "http://")}
	go connector.Run(ctx)
	remote := New(cloud.URL+"/e/"+srv.Identity.ID, local.Token)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := remote.Probe(ctx, srv.Identity.ID, srv.Identity.PublicKey()); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("tunnel did not become ready")
		case <-tick.C:
		}
	}
	p := EnvironmentProfile{ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: local.Token,
		Routes: []Route{{URL: remote.BaseURL, Kind: "relay"}}}
	c, st, err := ConnectEnvironment(ctx, p)
	if err != nil || st.EnvironmentID != p.ID {
		t.Fatalf("connect through relay: %v", err)
	}
	project, err := c.CreateProject(ctx, "Created through relay", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := srv.Store.Project(ctx, project.ID); err != nil || got.Name != project.Name {
		t.Fatalf("write did not reach the computer: %#v, %v", got, err)
	}
	gotEvent := make(chan proto.Event, 1)
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	go c.Stream(streamCtx, st.Seq, func(e proto.Event) {
		if e.Type == proto.EventProject {
			select {
			case gotEvent <- e:
			default:
			}
		}
	}, nil)
	select {
	case <-gotEvent:
	case <-ctx.Done():
		t.Fatal("project event did not stream through the tunnel")
	}
	// Accept on direct transport but drop its response, then confirm and retry
	// through the real reverse tunnel. Only one provider turn may execute.
	runner := &deliveryRunner{}
	srv.Service.Providers.Register(runner)
	thread, err := c.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Provider: runner.ID(), Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	dropped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		srv.Handler().ServeHTTP(recorder, r)
		if recorder.Code != http.StatusAccepted {
			t.Errorf("request not accepted before dropping response: %d", recorder.Code)
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer dropped.Close()
	lossy := New(dropped.URL, local.Token)
	if _, err := lossy.SendWithRequest(ctx, thread.ID, "one delivery", nil, "send_lost"); err == nil {
		t.Fatal("expected lost response")
	}
	receipt, err := c.MessageReceipt(ctx, thread.ID, "send_lost")
	if err != nil || receipt.RunID == "" {
		t.Fatalf("receipt through relay: %#v, %v", receipt, err)
	}
	again, err := c.SendWithRequest(ctx, thread.ID, "one delivery", nil, "send_lost")
	if err != nil || again.ID != receipt.RunID {
		t.Fatalf("retry through relay: %#v, %v", again, err)
	}
	if err := srv.Service.Wait(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	runs, err := srv.Store.Runs(ctx, thread.ID)
	if err != nil || len(runs) != 1 || runner.calls.Load() != 1 {
		t.Fatalf("duplicate execution after route switch: %d runs, %d calls, %v", len(runs), runner.calls.Load(), err)
	}
	// Administrator privileges on the hub do not grant access to a computer.
	remote.Token = admin
	if _, err := remote.State(ctx); err != ErrUnauthorized {
		t.Fatalf("hub administrator obtained environment access: %v", err)
	}
}

type deliveryRunner struct{ calls atomic.Int32 }

func (*deliveryRunner) ID() string   { return "delivery-test" }
func (*deliveryRunner) Name() string { return "Delivery test" }
func (*deliveryRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "delivery-test", Installed: true}
}
func (r *deliveryRunner) Run(_ context.Context, _ provider.Turn, sink provider.Sink) error {
	r.calls.Add(1)
	sink.Text("accepted")
	return nil
}

func TestMultiRoutePairingRoundTrip(t *testing.T) {
	q := url.Values{"url": {"https://relay.example/e/id"}, "token": {"access-token"},
		"direct": {"http://192.168.1.2:4870"}, "environment": {"id"}, "key": {"public-key"}}
	p, err := PairingProfile("t3go://pair?"+q.Encode(), "")
	if err != nil || len(p.Routes) != 2 || p.Routes[0].Kind != "direct" || p.Routes[1].Kind != "relay" || p.Token != "access-token" {
		t.Fatalf("pairing = %#v, %v", p, err)
	}
	q.Del("key")
	if _, err := PairingProfile("t3go://pair?"+q.Encode(), ""); err == nil {
		t.Fatal("unpinned multi-route pairing accepted")
	}
}
