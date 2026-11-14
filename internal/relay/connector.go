package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

type Connector struct {
	Server        string
	EnvironmentID string
	Token         string
	// Target is a fixed local TCP endpoint, never supplied by a hub request.
	Target  string
	OnError func(error)
}

// Run reconnects until ctx is canceled. Existing streams close with their tunnel.
func (c Connector) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		start := time.Now()
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.OnError != nil {
			c.OnError(err)
		}
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
	}
}

func (c Connector) runOnce(ctx context.Context) error {
	base, err := ValidateURL(c.Server)
	if err != nil {
		return err
	}
	if c.Token == "" || c.EnvironmentID == "" {
		return errors.New("missing relay credentials")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/relay/connect/" + c.EnvironmentID
	if base.Scheme == "https" {
		base.Scheme = "wss"
	} else {
		base.Scheme = "ws"
	}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, base.String(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + c.Token}},
		HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	})
	cancel()
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return errors.New("relay handshake failed")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	session, err := yamux.Server(websocket.NetConn(ctx, conn, websocket.MessageBinary), tunnelConfig())
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()
	var workers sync.WaitGroup
	defer func() { _ = session.Close(); workers.Wait() }()
	limit := make(chan struct{}, 64)
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			return err
		}
		select {
		case limit <- struct{}{}:
		default:
			_ = stream.Close()
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-limit }()
			defer stream.Close()
			local, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", c.Target)
			if err != nil {
				return
			}
			defer local.Close()
			stop := context.AfterFunc(ctx, func() { _ = local.Close(); _ = stream.Close() })
			defer stop()
			done := make(chan struct{})
			go func() {
				_, _ = io.Copy(local, stream)
				_ = local.Close()
				close(done)
			}()
			_, _ = io.Copy(stream, local)
			_ = stream.Close()
			_ = local.Close()
			<-done
		}()
	}
}
