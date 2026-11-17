package server

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tk/t3go/internal/access"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/store"
)

// maxDeviceGrants caps the tokens one environment hands out, so a script
// cannot grow the list without bound.
const maxDeviceGrants = 32

// revokedDeviceRetention is how long a revoked grant stays in the list,
// long enough to see which device was taken away and when.
const revokedDeviceRetention = 30 * 24 * time.Hour

// handleDeviceGrants lists the environment's paired clients.
func (s *Server) handleDeviceGrants(w http.ResponseWriter, r *http.Request) {
	devices, err := s.Store.Devices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "devices", "could not read the paired devices")
		return
	}
	current := deviceFromRequest(r)
	grants := make([]proto.DeviceGrant, 0, len(devices))
	for _, device := range devices {
		grants = append(grants, deviceGrant(device, current))
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": grants})
}

// handlePairDeviceGrant mints a token for a new client. The token is answered
// once: the environment keeps only its digest.
func (s *Server) handlePairDeviceGrant(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Label string `json:"label"`
	}
	if err := decodeJSON(r, &input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "device", "the request body is not a device")
		return
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		label = "Device"
	}
	if runes := []rune(label); len(runes) > 60 {
		label = string(runes[:60])
	}
	devices, err := s.Store.Devices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "devices", "could not read the paired devices")
		return
	}
	active := 0
	for _, device := range devices {
		if !device.Revoked() {
			active++
		}
	}
	if active >= maxDeviceGrants {
		writeError(w, http.StatusConflict, "devices", "revoke a paired device before adding another")
		return
	}
	// Grants revoked long ago cannot be used or listed usefully any more.
	_ = s.Store.PruneDevices(r.Context(), time.Now().Add(-revokedDeviceRetention))
	token, digest := access.New()
	device := store.Device{ID: id.New("dev"), Label: label, CreatedAt: time.Now().UTC()}
	if err := s.Store.CreateDevice(r.Context(), device, digest); err != nil {
		writeError(w, http.StatusInternalServerError, "devices", "could not save the paired device")
		return
	}
	writeJSON(w, http.StatusCreated, proto.DevicePairing{Device: deviceGrant(device, deviceFromRequest(r)), Token: token})
}

// handleRevokeDeviceGrant stops one grant from authenticating. The environment
// token and the other devices keep working.
func (s *Server) handleRevokeDeviceGrant(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("id")
	if !id.Valid(deviceID) {
		writeError(w, http.StatusBadRequest, "device", "that device id is not valid")
		return
	}
	if err := s.Store.RevokeDevice(r.Context(), deviceID, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "device", "no paired device has that id")
			return
		}
		writeError(w, http.StatusInternalServerError, "device", "could not revoke the device")
		return
	}
	// Live streams authenticated by that grant stop now, not on the next
	// request they make.
	s.cutDeviceStreams(deviceID)
	writeJSON(w, http.StatusOK, map[string]string{"revoked": deviceID})
}

// deviceGrant is a stored grant as a client reads it.
func deviceGrant(device store.Device, currentID string) proto.DeviceGrant {
	grant := proto.DeviceGrant{ID: device.ID, Label: device.Label, CreatedAt: device.CreatedAt,
		Current: device.ID == currentID}
	if !device.LastSeenAt.IsZero() {
		seen := device.LastSeenAt
		grant.LastSeenAt = &seen
	}
	if !device.RevokedAt.IsZero() {
		revoked := device.RevokedAt
		grant.RevokedAt = &revoked
	}
	return grant
}
