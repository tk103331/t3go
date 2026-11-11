package device

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseDevices(t *testing.T) {
	raw := []byte(`{"devices":{
	  "com.apple.CoreSimulator.SimRuntime.iOS-17-0":[
	    {"udid":"AAA-1","name":"iPhone 15","state":"Booted","isAvailable":true},
	    {"udid":"AAA-2","name":"iPhone 15 Pro","state":"Shutdown","isAvailable":true},
	    {"udid":"AAA-3","name":"Old phone","state":"Shutdown","isAvailable":false}
	  ],
	  "com.apple.CoreSimulator.SimRuntime.iOS-18-0":[
	    {"udid":"BBB-1","name":"iPad Pro","state":"Shutdown","isAvailable":true}
	  ]}}`)
	devices, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("devices = %#v", devices)
	}
	if devices[0].ID != "BBB-1" || devices[0].OS != "iOS.18.0" || devices[0].Platform != "ios" {
		t.Fatalf("first = %#v", devices[0])
	}
	if devices[1].ID != "AAA-1" || devices[1].State != "booted" {
		t.Fatalf("second = %#v", devices[1])
	}
}

func TestValidDeviceID(t *testing.T) {
	for _, id := range []string{"emulator-5554", "avd:Pixel_6", "avd:Pixel-7.API_34"} {
		if validDeviceID(id) != nil {
			t.Fatalf("rejected %q", id)
		}
	}
}

func TestCheckIDRejectsBadValues(t *testing.T) {
	for _, id := range []string{"", "abc", "../../etc/passwd", "ZZZZ-0000-0000-0000-000000000000"} {
		if validDeviceID(id) == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	if validDeviceID("7E99F39F-299A-4E23-A2D0-7948BC3EE5F4") != nil {
		t.Fatal("rejected a simulator UUID")
	}
}

func TestListAndroidEmulators(t *testing.T) {
	dir := t.TempDir()
	adb := `#!/bin/sh
case "$1" in
devices) echo "List of devices attached"; printf 'emulator-5554\tdevice\n' ;;
-s) case "$3" in emu) echo "Pixel_6"; echo "OK" ;; esac ;;
esac
`
	emulator := "#!/bin/sh\necho Pixel_6\necho Pixel_7\n"
	if err := os.WriteFile(filepath.Join(dir, "adb"), []byte(adb), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "emulator"), []byte(emulator), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ANDROID_HOME", "")
	t.Setenv("ANDROID_SDK_ROOT", "")
	devices, err := List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %#v", devices)
	}
	if devices[0].ID != "emulator-5554" || devices[0].Name != "Pixel_6" || devices[0].State != "booted" {
		t.Fatalf("running = %#v", devices[0])
	}
	if devices[1].ID != "avd:Pixel_7" || devices[1].State != "shutdown" || devices[1].Platform != "android" {
		t.Fatalf("configured = %#v", devices[1])
	}
}
