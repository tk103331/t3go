// Command t3go runs a T3Go environment: the HTTP/SSE/WebSocket server
// that desktop and mobile clients connect to.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "serve":
		return cmdServe(args[1:])
	case "pair":
		return cmdPair(args[1:])
	case "doctor":
		return cmdDoctor(args[1:])
	case "mcp":
		return cmdMCP(args[1:])
	case "version", "-v", "--version":
		fmt.Printf("t3go %s (%s/%s, go %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return 0
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "t3go: unknown command %q\n\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `t3go %s - a Go T3 Code environment

Usage:
  t3go serve    [--home DIR] [--listen HOST:PORT] [--no-auth] [--web DIR]
  t3go pair     [--home DIR] [--listen HOST:PORT]
  t3go doctor   [--home DIR]
  t3go version

The client API is served at /api/v1; live events at /api/v1/events (SSE)
and /ws (WebSocket). Clients authenticate with the bearer token from
$T3GO_HOME/token, or with ?token= when a header is not possible.
`, version)
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	home := fs.String("home", "", "data directory (default $T3GO_HOME or ~/.t3go)")
	listen := fs.String("listen", "", "listen address (default 127.0.0.1:4870)")
	noAuth := fs.Bool("no-auth", false, "disable auth; requires a loopback listen address")
	quiet := fs.Bool("quiet", false, "only log warnings")
	web := fs.String("web", "", "directory of the built web client to serve at /")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*home, *listen, *noAuth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	cfg.Version = version
	if *web != "" {
		dir, err := filepath.Abs(*web)
		if err != nil {
			fmt.Fprintf(os.Stderr, "t3go: --web: %v\n", err)
			return 1
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			fmt.Fprintf(os.Stderr, "t3go: --web: %s is not a directory\n", dir)
			return 1
		}
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
			fmt.Fprintf(os.Stderr, "t3go: --web: %s has no index.html; run scripts/build-web.sh first\n", dir)
			return 1
		}
		cfg.WebDir = dir
	}
	logger := newLogger(*quiet, cfg.Home)

	srv, err := server.New(cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	defer srv.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: listen: %v\n", err)
		return 1
	}
	if !*quiet {
		fmt.Printf("t3go %s\n", version)
		fmt.Printf("  home    %s\n", cfg.Home)
		fmt.Printf("  listen  http://%s\n", ln.Addr())
		if cfg.WebDir != "" {
			fmt.Printf("  web     http://%s/\n", ln.Addr())
		}
		if !cfg.NoAuth {
			fmt.Printf("  token   %s\n", config.TokenPath(cfg.Home))
			fmt.Printf("  pair    %s\n", pairingURL(ln.Addr().String(), cfg.Token))
		}
	}
	if err := srv.Serve(ctx, ln); err != nil {
		fmt.Fprintf(os.Stderr, "t3go: serve: %v\n", err)
		return 1
	}
	if !*quiet {
		fmt.Println("t3go: stopped")
	}
	return 0
}

func cmdPair(args []string) int {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	home := fs.String("home", "", "data directory (default $T3GO_HOME or ~/.t3go)")
	listen := fs.String("listen", "", "server address as clients reach it (default 127.0.0.1:4870)")
	jsonOut := fs.Bool("json", false, "print the URL and token as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*home, *listen, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t3go: %v\n", err)
		return 1
	}
	url := pairingURL(cfg.Listen, cfg.Token)
	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": cfg.Listen, "token": cfg.Token, "pairing": url})
		return 0
	}
	fmt.Println(url)
	return 0
}

func pairingURL(addr, token string) string {
	if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, "[::]:") {
		if ip := outboundIP(); ip != "" {
			addr = ip + addr[strings.LastIndex(addr, ":"):]
		}
	}
	return fmt.Sprintf("t3go://pair?url=%s&token=%s", "http://"+addr, token)
}

func outboundIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	home := fs.String("home", "", "data directory (default $T3GO_HOME or ~/.t3go)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*home, "", false)
	if err != nil {
		fmt.Printf("config   FAIL %v\n", err)
		return 1
	}
	fmt.Printf("version  %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	fmt.Printf("go       %s\n", runtime.Version())
	fmt.Printf("home     %s\n", cfg.Home)
	fmt.Printf("database %s\n", config.DBPath(cfg.Home))
	fmt.Printf("token    %s\n", config.TokenPath(cfg.Home))

	failed := false
	check := func(name string, ok bool, detail string) {
		status := "ok  "
		if !ok {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("%-8s %s %s\n", name, status, detail)
	}

	// The server must be able to open its database.
	if srv, err := server.New(cfg, newLogger(true, cfg.Home)); err != nil {
		check("db", false, err.Error())
	} else {
		check("db", true, "opened")
		srv.Close()
	}

	for _, provider := range []struct{ id, bin string }{
		{"claude", "claude"}, {"codex", "codex"}, {"opencode", "opencode"},
	} {
		path, err := exec.LookPath(provider.bin)
		if err != nil {
			check(provider.id, false, "not found in PATH")
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, path, "--version").Output()
		cancel()
		if err != nil {
			check(provider.id, false, path)
			continue
		}
		check(provider.id, true, strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
	}

	if out, err := exec.Command("git", "--version").Output(); err != nil {
		check("git", false, "not usable: "+err.Error())
	} else {
		check("git", true, strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
	}

	if failed {
		return 1
	}
	return 0
}

// newLogger writes to stderr and, when the data directory is writable, to
// its log file, so a service started in the background keeps a record.
func newLogger(quiet bool, home string) *log.Logger {
	if quiet {
		return log.New(io.Discard, "", 0)
	}
	out := io.Writer(os.Stderr)
	if home != "" {
		if f, err := os.OpenFile(filepath.Join(home, "logs", "t3go.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			out = io.MultiWriter(os.Stderr, f)
		}
	}
	return log.New(out, "t3go: ", log.LstdFlags)
}
