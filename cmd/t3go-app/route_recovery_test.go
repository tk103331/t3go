package main

import (
	"testing"
	"time"

	"github.com/tk/t3go/internal/client"
)

func TestRouteRecoveryWaitsForStableDirectConnection(t *testing.T) {
	now := time.Unix(1000, 0)
	profile := client.EnvironmentProfile{Routes: []client.Route{
		{URL: "direct", Kind: "direct"}, {URL: "relay", Kind: "relay"},
	}}
	var recovery routeRecovery
	recovery.enter(now, "relay")
	recovery.failed(now, "direct")
	if routes := recovery.candidates(now.Add(10*time.Second), profile, "relay", false); len(routes) != 0 {
		t.Fatal("failed route ignored cooldown")
	}
	if routes := recovery.candidates(now.Add(31*time.Second), profile, "relay", true); len(routes) != 0 {
		t.Fatal("relay residence ignored")
	}
	routes := recovery.candidates(now.Add(time.Minute), profile, "relay", true)
	if len(routes) != 1 || routes[0].Kind != "direct" {
		t.Fatalf("direct recovery candidates: %#v", routes)
	}
	if recovery.directReady("direct") {
		t.Fatal("one successful probe promoted direct")
	}
	recovery.failed(now.Add(time.Minute), "direct")
	if recovery.directReady("direct") {
		t.Fatal("failure did not reset success streak")
	}
	if !recovery.directReady("direct") {
		t.Fatal("two successes did not allow promotion")
	}
	profile.Preference = "relay-only"
	if len(recovery.candidates(now.Add(2*time.Minute), profile, "relay", true)) != 0 {
		t.Fatal("relay-only mode probed direct")
	}
	profile.Preference = "direct-only"
	if len(recovery.candidates(now.Add(2*time.Minute), profile, "direct", false)) != 0 {
		t.Fatal("direct-only mode used relay")
	}
}
