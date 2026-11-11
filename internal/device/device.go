// Package device lists and controls the local simulators T3Go can show.
package device

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// ErrUnavailable says the platform has no simulator tools.
var ErrUnavailable = errors.New("no simulator tools; install Xcode and its iOS simulators")

// List returns the available iOS simulators.
func List(ctx context.Context) ([]proto.Device, error) {
	if runtime.GOOS != "darwin" {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "available", "--json").Output()
	if err != nil {
		return nil, ErrUnavailable
	}
	return Parse(out)
}

// Parse reads simctl's device JSON.
func Parse(raw []byte) ([]proto.Device, error) {
	var payload struct {
		Devices map[string][]struct {
			UDID        string `json:"udid"`
			Name        string `json:"name"`
			State       string `json:"state"`
			IsAvailable bool   `json:"isAvailable"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var devices []proto.Device
	for runtimeID, list := range payload.Devices {
		osName := strings.TrimPrefix(runtimeID, "com.apple.CoreSimulator.SimRuntime.")
		osName = strings.ReplaceAll(osName, "-", ".")
		for _, item := range list {
			if !item.IsAvailable {
				continue
			}
			devices = append(devices, proto.Device{ID: item.UDID, Name: item.Name, Platform: "ios",
				OS: osName, State: strings.ToLower(item.State)})
		}
	}
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].OS != devices[j].OS {
			return devices[i].OS > devices[j].OS
		}
		return devices[i].Name < devices[j].Name
	})
	return devices, nil
}

// Boot starts a simulator and brings the Simulator app forward.
func Boot(ctx context.Context, id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "boot", id).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "current state: booted") {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// Shutdown stops a simulator.
func Shutdown(ctx context.Context, id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "shutdown", id).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "current state: shutdown") {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// Screenshot returns the simulator's current screen as PNG bytes.
func Screenshot(ctx context.Context, id string) ([]byte, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	path := filepath.Join(os.TempDir(), "t3go-device-"+id+".png")
	defer os.Remove(path)
	if out, err := exec.CommandContext(ctx, "xcrun", "simctl", "io", id, "screenshot", path).CombinedOutput(); err != nil {
		return nil, errors.New(strings.TrimSpace(string(out)))
	}
	return os.ReadFile(path)
}

// checkID rejects anything that is not a simulator UUID.
func checkID(id string) error {
	if len(id) < 32 || len(id) > 40 {
		return errors.New("invalid device id")
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdefABCDEF-", r) {
			return errors.New("invalid device id")
		}
	}
	return nil
}
