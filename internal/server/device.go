package server

import (
	"net/http"

	"github.com/tk/t3go/internal/device"
)

// handleDevices lists the local simulators.
func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := device.List(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]any{"devices": []any{}, "detail": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"devices": devices})
}

// handleDeviceBoot starts a simulator.
func (s *Server) handleDeviceBoot(w http.ResponseWriter, r *http.Request) {
	if err := device.Boot(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, 500, "device", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleDeviceShutdown stops a simulator.
func (s *Server) handleDeviceShutdown(w http.ResponseWriter, r *http.Request) {
	if err := device.Shutdown(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, 500, "device", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleDeviceScreenshot streams a simulator's screen as a PNG.
func (s *Server) handleDeviceScreenshot(w http.ResponseWriter, r *http.Request) {
	shot, err := device.Screenshot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, 500, "device", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(shot)
}
