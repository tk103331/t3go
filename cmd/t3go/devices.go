package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tk/t3go/internal/access"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/store"
)

// pairDeviceToken stores a new device grant and returns the token the
// device pairs with. The environment keeps only the token's digest.
func pairDeviceToken(home, label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Device"
	}
	st, err := store.Open(config.DBPath(home))
	if err != nil {
		return "", err
	}
	defer st.Close()
	token, digest := access.New()
	device := store.Device{ID: id.New("dev"), Label: label, CreatedAt: time.Now().UTC()}
	if err := st.CreateDevice(context.Background(), device, digest); err != nil {
		return "", err
	}
	return token, nil
}

// cmdDevices lists the paired devices, mints one, or revokes one.
func cmdDevices(args []string) int {
	action := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
		args = args[1:]
	}
	switch action {
	case "list":
		return cmdListDevices(args)
	case "add", "pair":
		return cmdAddDevice(args)
	case "revoke":
		return cmdRevokeDevice(args)
	default:
		fmt.Fprintf(os.Stderr, "t3go devices: unknown action %q\n\n", action)
		usage()
		return 2
	}
}

func cmdListDevices(args []string) int {
	fs := flag.NewFlagSet("devices", flag.ExitOnError)
	home := fs.String("home", "", "data directory (default $T3GO_HOME or ~/.t3go)")
	jsonOut := fs.Bool("json", false, "print the devices as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "t3go devices: put the action and its arguments after the flags, as in `t3go devices --home DIR list`")
		return 2
	}
	cfg, err := config.Load(*home, "", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	st, err := store.Open(config.DBPath(cfg.Home))
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	defer st.Close()
	devices, err := st.Devices(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	if *jsonOut {
		type row struct {
			ID         string `json:"id"`
			Label      string `json:"label"`
			Status     string `json:"status"`
			CreatedAt  string `json:"createdAt"`
			LastSeenAt string `json:"lastSeenAt,omitempty"`
		}
		rows := make([]row, 0, len(devices))
		for _, device := range devices {
			entry := row{ID: device.ID, Label: device.Label, Status: deviceStatus(device),
				CreatedAt: device.CreatedAt.UTC().Format(time.RFC3339)}
			if !device.LastSeenAt.IsZero() {
				entry.LastSeenAt = device.LastSeenAt.UTC().Format(time.RFC3339)
			}
			rows = append(rows, entry)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"devices": rows})
		return 0
	}
	if len(devices) == 0 {
		fmt.Println("No paired devices. Pair one with `t3go pair --device NAME`.")
		return 0
	}
	table := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tLABEL\tCREATED\tLAST SEEN\tSTATUS")
	for _, device := range devices {
		seen := "never"
		if !device.LastSeenAt.IsZero() {
			seen = device.LastSeenAt.UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", device.ID, device.Label,
			device.CreatedAt.UTC().Format("2006-01-02 15:04"), seen, deviceStatus(device))
	}
	_ = table.Flush()
	return 0
}

func deviceStatus(device store.Device) string {
	if device.Revoked() {
		return "revoked"
	}
	return "active"
}

func cmdAddDevice(args []string) int {
	fs := flag.NewFlagSet("devices add", flag.ExitOnError)
	home := fs.String("home", "", "data directory (default $T3GO_HOME or ~/.t3go)")
	label := fs.String("label", "", "name the device list shows")
	listen := fs.String("listen", "", "server address as clients reach it (default 127.0.0.1:4870)")
	jsonOut := fs.Bool("json", false, "print the URL and token as JSON")
	useRelay := fs.Bool("relay", false, "print the registered relay pairing link")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return printDevicePairing(*home, *label, *listen, *useRelay, *jsonOut)
}

// printDevicePairing mints a grant and prints the links that use it.
func printDevicePairing(home, label, listen string, useRelay, jsonOut bool) int {
	cfg, err := config.Load(home, listen, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	token, err := pairDeviceToken(cfg.Home, label)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	url := pairingURL(cfg.Listen, token)
	if useRelay {
		credentials, err := loadRelay(cfg.Home)
		if err != nil {
			fmt.Fprintln(os.Stderr, "t3go: no saved relay configuration")
			return 1
		}
		url = relayPairing(credentials, token)
	}
	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": cfg.Listen, "token": token,
			"pairing": url, "t3": upstreamPairingURL(cfg.Listen, token)})
		return 0
	}
	fmt.Println(url)
	return 0
}

func cmdRevokeDevice(args []string) int {
	fs := flag.NewFlagSet("devices revoke", flag.ExitOnError)
	home := fs.String("home", "", "data directory (default $T3GO_HOME or ~/.t3go)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "t3go devices revoke: put the flags before the device id")
		return 2
	}
	deviceID := strings.TrimSpace(fs.Arg(0))
	if !id.Valid(deviceID) {
		fmt.Fprintln(os.Stderr, "t3go devices revoke: name the device id from `t3go devices`")
		return 2
	}
	cfg, err := config.Load(*home, "", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	st, err := store.Open(config.DBPath(cfg.Home))
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	defer st.Close()
	if err := st.RevokeDevice(context.Background(), deviceID, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fmt.Fprintln(os.Stderr, "t3go devices revoke: no device has that id")
			return 1
		}
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	fmt.Printf("revoked %s\n", deviceID)
	return 0
}
