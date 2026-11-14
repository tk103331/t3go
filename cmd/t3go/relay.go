package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/relay"
	"github.com/tk/t3go/internal/store"
)

type relayCredentials struct {
	Server        string `json:"server"`
	EnvironmentID string `json:"environmentId"`
	ConnectToken  string `json:"connectToken"`
	DirectURL     string `json:"directUrl,omitempty"`
}

func loadRelay(home string) (relayCredentials, error) {
	var credentials relayCredentials
	raw, err := os.ReadFile(filepath.Join(home, "relay.json"))
	if err != nil {
		return credentials, err
	}
	err = json.Unmarshal(raw, &credentials)
	return credentials, err
}

func relayPairing(credentials relayCredentials, token string) string {
	q := url.Values{"url": {strings.TrimRight(credentials.Server, "/") + "/e/" + credentials.EnvironmentID},
		"token": {token}, "environment": {credentials.EnvironmentID}, "key": {credentials.EnvironmentID}}
	if credentials.DirectURL != "" {
		q.Set("direct", credentials.DirectURL)
	}
	return "t3go://pair?" + q.Encode()
}

// cmdRelay explicitly registers this environment with a trusted, self-hosted hub.
// Administrator credentials are read from a file, not command-line arguments.
func cmdRelay(args []string) int {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	home := fs.String("home", "", "environment data directory")
	address := fs.String("server", "", "trusted hub http(s) URL")
	adminFile := fs.String("admin-token-file", "", "file containing hub administrator token")
	direct := fs.String("direct-url", "", "optional reachable LAN URL for this environment")
	name := fs.String("name", "", "environment name")
	revoke := fs.String("revoke", "", "revoke this environment ID instead of registering")
	allowHTTP := fs.Bool("allow-http", false, "explicitly allow unencrypted HTTP for a trusted network")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	u, err := relay.ValidateURL(*address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "t3go relay:", err)
		return 1
	}
	if u.Scheme != "https" && !*allowHTTP {
		fmt.Fprintln(os.Stderr, "t3go relay: HTTPS required; use --allow-http only on a trusted network")
		return 1
	}
	if *direct != "" {
		if _, err := relay.ValidateURL(*direct); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	admin, err := os.ReadFile(*adminFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "t3go relay: cannot read administrator token file")
		return 1
	}
	cfg, err := config.Load(*home, "", false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	method, target := http.MethodPost, strings.TrimRight(u.String(), "/")+"/api/v1/relay/environments"
	var raw []byte
	var environmentID string
	if *revoke != "" {
		method, target = http.MethodDelete, target+"/"+url.PathEscape(*revoke)
	} else {
		if _, err := os.Stat(filepath.Join(cfg.Home, "relay.json")); err == nil {
			fmt.Fprintln(os.Stderr, "t3go relay: already configured; revoke the old binding before replacing relay.json")
			return 1
		} else if !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		st, err := store.Open(config.DBPath(cfg.Home))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		identity, identityErr := st.EnvironmentIdentity(ctx)
		_ = st.Close()
		if identityErr != nil {
			fmt.Fprintln(os.Stderr, identityErr)
			return 1
		}
		environmentID = identity.ID
		displayName := strings.TrimSpace(*name)
		if displayName == "" {
			displayName, _ = os.Hostname()
		}
		registration := relay.Registration{ID: identity.ID, PublicKey: identity.PublicKey(), Name: displayName, Token: cfg.Token}
		if *direct != "" {
			registration.DirectURLs = []string{*direct}
		}
		raw, _ = json.Marshal(registration)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(admin)))
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "t3go relay: registration connection failed")
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "t3go relay: server rejected request (%d)\n", resp.StatusCode)
		return 1
	}
	if *revoke != "" {
		if saved, err := loadRelay(cfg.Home); err == nil && saved.EnvironmentID == *revoke &&
			strings.TrimRight(saved.Server, "/") == strings.TrimRight(u.String(), "/") {
			if err := os.Remove(filepath.Join(cfg.Home, "relay.json")); err != nil {
				fmt.Fprintln(os.Stderr, "t3go relay: binding revoked, but could not remove local relay.json")
				return 1
			}
		}
		fmt.Println("Relay binding revoked. Local projects and direct access remain unchanged.")
		return 0
	}
	var registered relay.Registered
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&registered); err != nil || len(registered.ConnectToken) < 20 {
		fmt.Fprintln(os.Stderr, "t3go relay: invalid registration response")
		return 1
	}
	credentials := relayCredentials{Server: u.String(), EnvironmentID: environmentID, ConnectToken: registered.ConnectToken, DirectURL: *direct}
	data, _ := json.MarshalIndent(credentials, "", "  ")
	// Exclusive creation avoids silently overwriting another local hub binding.
	f, err := os.OpenFile(filepath.Join(cfg.Home, "relay.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "t3go relay: could not save credentials; revoke the remote binding before registering again")
		return 1
	}
	fmt.Println("Relay registered. Start this environment with: t3go serve --connect-relay")
	fmt.Println(relayPairing(credentials, cfg.Token))
	return 0
}

func startRelayConnector(ctx context.Context, home, environmentID, listen string, logError func(error)) error {
	credentials, err := loadRelay(home)
	if err != nil {
		return err
	}
	if credentials.EnvironmentID != environmentID || len(credentials.ConnectToken) < 20 {
		return fmt.Errorf("saved relay credentials do not match this environment")
	}
	if _, err := relay.ValidateURL(credentials.Server); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	connector := relay.Connector{Server: credentials.Server, EnvironmentID: credentials.EnvironmentID,
		Token: credentials.ConnectToken, Target: net.JoinHostPort(host, port), OnError: logError}
	go connector.Run(ctx)
	return nil
}
