package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDevicesKeepDigestsAndRevokeOneGrant(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "devices.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	created := time.Now().UTC().Truncate(time.Second)
	first := Device{ID: "dev_1", Label: "Pixel 8", CreatedAt: created}
	second := Device{ID: "dev_2", Label: "Laptop", CreatedAt: created}
	if err := st.CreateDevice(ctx, first, "digest-1"); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDevice(ctx, second, "digest-2"); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDevice(ctx, Device{ID: "dev_3", Label: "Again", CreatedAt: created}, "digest-1"); err == nil {
		t.Fatal("two devices were stored for one token digest")
	}

	got, err := st.DeviceByDigest(ctx, "digest-1")
	if err != nil || got.ID != first.ID || got.Label != first.Label {
		t.Fatalf("device by digest = %+v, %v", got, err)
	}
	if got.Revoked() || !got.LastSeenAt.IsZero() {
		t.Fatalf("a new grant reads as used or revoked: %+v", got)
	}
	seen := created.Add(time.Minute)
	if err := st.TouchDevice(ctx, first.ID, seen); err != nil {
		t.Fatal(err)
	}
	if got, err = st.DeviceByDigest(ctx, "digest-1"); err != nil || !got.LastSeenAt.Equal(seen) {
		t.Fatalf("last seen = %v, %v", got.LastSeenAt, err)
	}

	revoked := created.Add(2 * time.Minute)
	if err := st.RevokeDevice(ctx, first.ID, revoked); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeviceByDigest(ctx, "digest-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a revoked device still authenticates: %v", err)
	}
	// Revoking again keeps the first time and the other grants working.
	if err := st.RevokeDevice(ctx, first.ID, created.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeviceByDigest(ctx, "digest-2"); err != nil {
		t.Fatalf("revoking one device touched another: %v", err)
	}
	if err := st.TouchDevice(ctx, first.ID, created.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	devices, err := st.Devices(ctx)
	if err != nil || len(devices) != 2 {
		t.Fatalf("devices = %+v, %v", devices, err)
	}
	if !devices[0].Revoked() || !devices[0].RevokedAt.Equal(revoked) {
		t.Fatalf("revocation time = %v, want %v", devices[0].RevokedAt, revoked)
	}
	if devices[0].LastSeenAt.Equal(created.Add(2 * time.Hour)) {
		t.Fatal("a revoked grant recorded a new visit")
	}
	if devices[1].Revoked() {
		t.Fatalf("the second grant was revoked too: %+v", devices[1])
	}
	if err := st.RevokeDevice(ctx, "dev_missing", revoked); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking an unknown device = %v", err)
	}

	// Old revocations fall away; the active grant stays.
	if err := st.PruneDevices(ctx, revoked.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if devices, err = st.Devices(ctx); err != nil || len(devices) != 1 || devices[0].ID != second.ID {
		t.Fatalf("devices after pruning = %+v, %v", devices, err)
	}
}
