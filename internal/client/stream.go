package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// Stream reads events from the environment until ctx ends, reconnecting
// with backoff after a broken connection. onEvent runs on the calling
// goroutine; a client that renders should hand work to its UI loop.
//
// after is the last sequence the caller has seen; pass 0 to replay what
// the retention window still holds.
func (c *Client) Stream(ctx context.Context, after int64, onEvent func(proto.Event), onError func(error)) error {
	if c.BaseURL == "" {
		return fmt.Errorf("t3go: no server URL")
	}
	attempt := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.streamOnce(ctx, after, func(e proto.Event) {
			attempt = 0
			if e.Seq > after {
				after = e.Seq
			}
			if onEvent != nil {
				onEvent(e)
			}
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		if onError != nil {
			onError(err)
		}
		timer := time.NewTimer(backoff(attempt))
		attempt++
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) streamOnce(ctx context.Context, after int64, onEvent func(proto.Event)) error {
	u := c.BaseURL + "/api/v1/events?after=" + url.QueryEscape(fmt.Sprint(after))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	// The stream is long-lived; only the response headers may time out.
	hc := *h
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
	}

	reader := bufio.NewReaderSize(resp.Body, 64<<10)
	var data []string
	var eventType string
	dispatch := func() {
		if len(data) == 0 {
			eventType = ""
			return
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		name := eventType
		eventType = ""
		var e proto.Event
		if err := json.Unmarshal([]byte(payload), &e); err != nil {
			return
		}
		if e.Type == "" && name != "" {
			e.Type = name
		}
		onEvent(e)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			dispatch()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			dispatch()
		case strings.HasPrefix(line, ":"):
			// comment, such as a keepalive
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "id:"):
			// The payload carries seq; the id only supports browser reconnect.
		case strings.HasPrefix(line, "retry:"):
		}
	}
}
