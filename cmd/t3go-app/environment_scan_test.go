package main

import (
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/tk/t3go/internal/client"
)

func qrImage(t *testing.T, payload string) []byte {
	t.Helper()
	raw, err := qrcode.Encode(payload, qrcode.Medium, 512)
	if err != nil {
		t.Fatalf("encode QR code: %v", err)
	}
	return raw
}

func TestPairingLinkFromQRImage(t *testing.T) {
	want, err := client.PairingLink(client.EnvironmentProfile{
		ID:        "env_0123456789abcdefghijklmnopqrstuvwxyz",
		PublicKey: "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		Token:     "environment-access-token-0123456789",
		Routes: []client.Route{
			{URL: "https://relay.example/e/env_0123456789abcdefghijklmnopqrstuvwxyz", Kind: "relay"},
			{URL: "http://192.168.1.20:4870", Kind: "direct"},
		},
	})
	if err != nil {
		t.Fatalf("create pairing link: %v", err)
	}
	got, err := pairingLinkFromQRImage(qrImage(t, want))
	if err != nil {
		t.Fatalf("decode pairing QR code: %v", err)
	}
	if got != want {
		t.Fatalf("pairing link = %q, want %q", got, want)
	}
}

func TestPairingLinkFromQRImageRejectsOtherCodes(t *testing.T) {
	if _, err := pairingLinkFromQRImage(qrImage(t, "https://example.com")); err == nil {
		t.Fatal("non-T3Go QR code was accepted")
	}
	if _, err := pairingLinkFromQRImage(nil); err == nil {
		t.Fatal("an empty image was accepted")
	}
}

func TestUIMobileAddEnvironmentStartsWithCamera(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	profile := client.EnvironmentProfile{
		ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: cfg.Token,
		Routes: []client.Route{{URL: ts.URL, Kind: "direct"}},
	}
	env := core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone, Camera: true,
		SafeArea: core.Edges{Top: 24, Bottom: 16}}
	app := nsgotest.Start(t, Root, nsgotest.Size(390, 844), nsgotest.Environment(env),
		nsgotest.Storage(map[string]string{"environments:v1": workspaceRegistry(profile)}))
	app.WaitFor("id:settings", 10*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-nav-environments", 5*time.Second)
	app.Press("id:settings-nav-environments")
	app.WaitFor("id:environment-add", 5*time.Second)
	app.Press("id:environment-add")
	app.WaitFor("id:environment-scan-qr", 5*time.Second)
	app.Press("id:environment-scan-qr")
	if len(app.Photos) != 1 {
		t.Fatalf("camera requests = %d, want 1", len(app.Photos))
	}
}
