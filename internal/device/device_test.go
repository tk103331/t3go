package device

import (
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

func TestCheckIDRejectsBadValues(t *testing.T) {
	for _, id := range []string{"", "abc", "../../etc/passwd", "ZZZZ-0000-0000-0000-000000000000"} {
		if checkID(id) == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	if checkID("7E99F39F-299A-4E23-A2D0-7948BC3EE5F4") != nil {
		t.Fatal("rejected a simulator UUID")
	}
}
