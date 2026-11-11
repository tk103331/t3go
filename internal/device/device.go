// Package device lists and controls the local simulators T3Go can show.
package device

import (
	"bytes"
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
var ErrUnavailable = errors.New("no simulator tools; install Xcode or the Android SDK")

// List returns the available iOS simulators and Android emulators.
func List(ctx context.Context) ([]proto.Device, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	devices, iosErr := listIOS(ctx)
	devices = append(devices, listAndroid(ctx)...)
	if len(devices) == 0 {
		if iosErr != nil {
			return nil, iosErr
		}
		return nil, ErrUnavailable
	}
	return devices, nil
}

// listIOS returns the available iOS simulators.
func listIOS(ctx context.Context) ([]proto.Device, error) {
	if runtime.GOOS != "darwin" {
		return nil, ErrUnavailable
	}
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

// listAndroid returns running emulators and the AVDs that can start.
func listAndroid(ctx context.Context) []proto.Device {
	adb := androidTool("adb", "platform-tools", "adb")
	if adb == "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, adb, "devices").Output()
	if err != nil {
		return nil
	}
	running := map[string]bool{}
	var devices []proto.Device
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "device" || !strings.HasPrefix(fields[0], "emulator-") {
			continue
		}
		serial := fields[0]
		name := androidName(ctx, adb, serial)
		running[name] = true
		devices = append(devices, proto.Device{ID: serial, Name: name, Platform: "android",
			OS: "Android", State: "booted"})
	}
	for _, avd := range androidAVDs(ctx) {
		if running[avd] {
			continue
		}
		devices = append(devices, proto.Device{ID: "avd:" + avd, Name: avd, Platform: "android",
			OS: "Android", State: "shutdown"})
	}
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].State != devices[j].State {
			return devices[i].State == "booted"
		}
		return devices[i].Name < devices[j].Name
	})
	return devices
}

// androidName asks an emulator for its AVD name.
func androidName(ctx context.Context, adb, serial string) string {
	out, err := exec.CommandContext(ctx, adb, "-s", serial, "emu", "avd", "name").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			name := strings.TrimSpace(line)
			if name != "" && name != "OK" {
				return name
			}
		}
	}
	return serial
}

// androidAVDs lists the configured Android virtual devices.
func androidAVDs(ctx context.Context) []string {
	emulator := androidTool("emulator", "emulator", "emulator")
	if emulator == "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, emulator, "-list-avds").Output()
	if err != nil {
		return nil
	}
	var avds []string
	for _, line := range strings.Split(string(out), "\n") {
		if avd := strings.TrimSpace(line); avd != "" {
			avds = append(avds, avd)
		}
	}
	return avds
}

// androidTool finds a tool on PATH or under the Android SDK.
func androidTool(name, dir, binary string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, root := range []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT")} {
		if root == "" {
			continue
		}
		path := filepath.Join(root, dir, binary)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// Boot starts a simulator or emulator.
func Boot(ctx context.Context, id string) error {
	if err := validDeviceID(id); err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(id, "avd:"):
		emulator := androidTool("emulator", "emulator", "emulator")
		if emulator == "" {
			return errors.New("the Android emulator is not installed")
		}
		cmd := exec.Command(emulator, "-avd", strings.TrimPrefix(id, "avd:"))
		return cmd.Start()
	case strings.HasPrefix(id, "emulator-"):
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "boot", id).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "current state: booted") {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// Shutdown stops a simulator or emulator.
func Shutdown(ctx context.Context, id string) error {
	if err := validDeviceID(id); err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(id, "avd:"):
		return nil
	case strings.HasPrefix(id, "emulator-"):
		adb := androidTool("adb", "platform-tools", "adb")
		if adb == "" {
			return errors.New("adb is not installed")
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, adb, "-s", id, "emu", "kill").CombinedOutput(); err != nil {
			return errors.New(strings.TrimSpace(string(out)))
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "shutdown", id).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "current state: shutdown") {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// Screenshot returns the device's current screen as PNG bytes.
func Screenshot(ctx context.Context, id string) ([]byte, error) {
	if err := validDeviceID(id); err != nil {
		return nil, err
	}
	if strings.HasPrefix(id, "avd:") {
		return nil, errors.New("boot the emulator first")
	}
	if strings.HasPrefix(id, "emulator-") {
		adb := androidTool("adb", "platform-tools", "adb")
		if adb == "" {
			return nil, errors.New("adb is not installed")
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, adb, "-s", id, "exec-out", "screencap", "-p")
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, errors.New("could not read the emulator screen")
		}
		return out.Bytes(), nil
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

// validDeviceID accepts a simulator UUID, an emulator serial or an AVD
// name, and nothing that could be an argument of its own.
func validDeviceID(id string) error {
	if strings.HasPrefix(id, "avd:") {
		name := strings.TrimPrefix(id, "avd:")
		if name == "" || len(name) > 64 {
			return errors.New("invalid device id")
		}
		for _, r := range name {
			if !strings.ContainsRune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_.-", r) {
				return errors.New("invalid device id")
			}
		}
		return nil
	}
	if strings.HasPrefix(id, "emulator-") {
		if len(id) < 10 || len(id) > 24 {
			return errors.New("invalid device id")
		}
		for _, r := range strings.TrimPrefix(id, "emulator-") {
			if r < '0' || r > '9' {
				return errors.New("invalid device id")
			}
		}
		return nil
	}
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
