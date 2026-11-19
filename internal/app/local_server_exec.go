//go:build !(js && wasm)

package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// localServerPort is the first port the local environment server tries.
const localServerPort = 4870

// bundledServerPath is the environment server inside the installed
// package: T3Go.app/Contents/Resources/t3go on macOS, t3go beside the
// client binary elsewhere. T3GO_SERVER_BIN overrides it.
func bundledServerPath() string {
	if path := strings.TrimSpace(os.Getenv("T3GO_SERVER_BIN")); path != "" {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	var path string
	switch runtime.GOOS {
	case "darwin":
		path = filepath.Join(dir, "..", "Resources", "t3go")
	case "windows":
		path = filepath.Join(dir, "t3go.exe")
	default:
		path = filepath.Join(dir, "t3go")
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return ""
	}
	return path
}

// localServerHome is where the local environment server keeps its data:
// the same directory a separately installed server uses, so both see one
// workspace.
func localServerHome() string {
	if dir := strings.TrimSpace(os.Getenv("T3GO_HOME")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".t3go"
	}
	return filepath.Join(home, ".t3go")
}

// readLocalToken reads the token the local environment server keeps.
func readLocalToken(home string) string {
	raw, err := os.ReadFile(filepath.Join(home, "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// startLocalServer starts the packaged environment server and returns its
// address and token. A server already serving the same data directory is
// reused instead of a second one. A background server keeps running after
// the client exits, so agent runs it owns finish; a LAN server also listens
// where phones on this network can reach it.
func startLocalServer(ctx context.Context, background, lan bool) (*localServer, error) {
	bin := bundledServerPath()
	if bin == "" {
		return nil, errNoBundledServer
	}
	home := localServerHome()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	token := readLocalToken(home)
	var lastErr error
	for port := localServerPort; port < localServerPort+20; port++ {
		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		if serverAnswers(base) {
			// Another environment server holds the port: reuse it when it
			// keeps this data directory, else take the next port.
			if serverAcceptsToken(base, token) {
				// A server left behind by a client that was killed stops
				// once its parent watch notices: wait it out instead of
				// connecting to one that is about to close.
				time.Sleep(1200 * time.Millisecond)
				if serverAcceptsToken(base, token) {
					health, healthErr := fetchLocalServerHealth(base)
					actualLAN, lanKnown := serverExposesLAN(base)
					actualBackground, backgroundKnown := background, false
					if healthErr == nil && health.Background != nil {
						actualBackground, backgroundKnown = *health.Background, true
					}
					if !lanKnown || !backgroundKnown || actualLAN != lan || actualBackground != background {
						if err := stopLocalServer(base, token); err != nil {
							return nil, fmt.Errorf("restart the local environment server: %w", err)
						}
						srv, err := launchLocalServer(ctx, bin, home, port, background, lan)
						if err != nil {
							return nil, err
						}
						return srv, nil
					}
					stop := func() { _ = stopLocalServer(base, token) }
					return &localServer{baseURL: base, token: token, home: home, stop: stop, background: actualBackground,
						lan: actualLAN, lanIP: lanAddress()}, nil
				}
			}
			continue
		}
		srv, err := launchLocalServer(ctx, bin, home, port, background, lan)
		if err == nil {
			return srv, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		// The port may have been taken between the probe and the listen,
		// or a server this client just stopped may still hold it.
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no free port for the local environment server")
}

// launchLocalServer runs the packaged server and waits for it to answer.
func launchLocalServer(ctx context.Context, bin, home string, port int, background, lan bool) (*localServer, error) {
	logs := filepath.Join(home, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(logs, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	cmd := localServerCommand(bin, home, port, background, lan)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	stop := func() {
		if token := readLocalToken(home); token != "" && stopLocalServer(base, token) == nil {
			return
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	if !background {
		go func() {
			<-ctx.Done()
			stop()
		}()
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			return nil, fmt.Errorf("the local environment server exited: %v", err)
		default:
		}
		if ctx.Err() != nil {
			stop()
			return nil, ctx.Err()
		}
		if serverAnswers(base) {
			if token := readLocalToken(home); token != "" {
				return &localServer{baseURL: base, token: token, home: home, stop: stop, background: background,
					lan: lan, lanIP: lanAddress()}, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	return nil, errors.New("the local environment server did not start")
}

// localServerCommand builds the serve command. A foreground server carries
// the client's process id so it stops with the client; a background one is
// started without it and keeps running.
func localServerCommand(bin, home string, port int, background, lan bool) *exec.Cmd {
	listen := "127.0.0.1"
	if lan {
		listen = "0.0.0.0"
	}
	args := []string{"serve", "--home", home, "--listen", fmt.Sprintf("%s:%d", listen, port), "--quiet"}
	if !background {
		args = append(args, "--parent-pid", strconv.Itoa(os.Getpid()))
	}
	return exec.Command(bin, args...)
}

// lanAddress is the address a phone on this network uses to reach this
// machine. Prefer the interface used by the default route, then fall back
// to a private IPv4 on a physical-looking interface.
func lanAddress() string {
	// Dialing UDP does not send a packet, but it asks the routing table
	// which local address faces the default network.
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && usableLANIP(addr.IP) {
			if iface := interfaceForIP(addr.IP); iface == "" || !virtualInterface(iface) {
				conn.Close()
				return addr.IP.String()
			}
		}
		conn.Close()
	}
	interfaces, err := net.Interfaces()
	if err == nil {
		var fallback string
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || virtualInterface(iface.Name) {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ipnet, ok := addr.(*net.IPNet)
				if !ok {
					continue
				}
				ip := ipnet.IP.To4()
				if !usableLANIP(ip) {
					continue
				}
				if ip.IsPrivate() {
					return ip.String()
				}
				if fallback == "" {
					fallback = ip.String()
				}
			}
		}
		if fallback != "" {
			return fallback
		}
	}
	return ""
}

func usableLANIP(ip net.IP) bool {
	ip = ip.To4()
	return ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

func interfaceForIP(want net.IP) string {
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if ok && ipnet.IP.Equal(want) {
				return iface.Name
			}
		}
	}
	return ""
}

// virtualInterface keeps VPN, container bridges and mobile hotspots from
// winning over a normal Wi-Fi/Ethernet interface when several are present.
func virtualInterface(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"utun", "tun", "tap", "tailscale", "zt", "docker",
		"bridge", "vmnet", "vboxnet", "br-", "veth", "virbr"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
