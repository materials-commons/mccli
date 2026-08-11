package wsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

var (
	// ErrRequeueFailed indicates that a message could not be restored to the
	// outbound queue after a websocket send failure.
	ErrRequeueFailed = errors.New("failed to requeue outbound websocket message")
)

// websocketConn is the subset of *websocket.Conn used by Client.
//
// This keeps sender and receiver loops unit-testable without a real websocket
// server.
type websocketConn interface {
	Write(ctx context.Context, typ websocket.MessageType, data []byte) error
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Close(code websocket.StatusCode, reason string) error
}

// Handler handles inbound text websocket messages.
type Handler func(ctx context.Context, msg TextMessage)

// Client is a reconnecting websocket client.
type Client struct {
	URL      string
	Token    string
	ClientID string
	Headers  http.Header
	Outbound *Queue[OutboundMessage]
	Handle   Handler

	Hostname   string
	ProjectIDs []int
	UserID     int

	ReconnectMin time.Duration
	ReconnectMax time.Duration
}

// Run connects and reconnects until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	if c.Outbound == nil {
		c.Outbound = NewQueue[OutboundMessage]()
	}
	if c.ReconnectMin == 0 {
		c.ReconnectMin = time.Second
	}
	if c.ReconnectMax == 0 {
		c.ReconnectMax = 30 * time.Second
	}

	backoff := c.ReconnectMin

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := c.runOnce(ctx)
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > c.ReconnectMax {
			backoff = c.ReconnectMax
		}
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	headers := c.buildHeaders()

	conn, _, err := websocket.Dial(ctx, c.URL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 3)

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		errCh <- c.senderLoop(runCtx, conn)
	}()
	go func() {
		defer wg.Done()
		errCh <- c.receiverLoop(runCtx, conn)
	}()
	go func() {
		defer wg.Done()
		errCh <- c.heartbeatLoop(runCtx)
	}()

	firstErr := <-errCh
	cancel()

	_ = conn.Close(websocket.StatusNormalClosure, "")

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	return firstErr
}

func (c *Client) buildHeaders() http.Header {
	headers := c.Headers.Clone()
	if headers == nil {
		headers = http.Header{}
	}

	if c.Token != "" {
		headers.Set("Authorization", "Bearer "+c.Token)
	}
	if c.ClientID != "" {
		headers.Set("MC-Client-ID", c.ClientID)
	}

	hostname := c.Hostname
	if hostname == "" {
		if value, err := os.Hostname(); err == nil {
			hostname = value
		}
	}
	if hostname != "" {
		headers.Set("MC-Client-Hostname", hostname)
	}

	if len(c.ProjectIDs) > 0 {
		parts := make([]string, 0, len(c.ProjectIDs))
		for _, id := range c.ProjectIDs {
			parts = append(parts, strconv.Itoa(id))
		}
		headers.Set("MC-Client-Projects", strings.Join(parts, ","))
	} else if headers.Get("MC-Client-Projects") == "" {
		headers.Set("MC-Client-Projects", "")
	}

	headers.Set("MC-Connection-Type", "cli")

	return headers
}

func (c *Client) senderLoop(ctx context.Context, conn websocketConn) error {
	for {
		msg, ok, err := c.Outbound.Pop(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		if err := c.send(ctx, conn, msg); err != nil {
			// Requeue so reconnect can retry.
			if ok := c.Outbound.Push(msg); !ok {
				return fmt.Errorf("%w: original send error: %v", ErrRequeueFailed, err)
			}
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, conn websocketConn, msg OutboundMessage) error {
	if conn == nil {
		return fmt.Errorf("websocket connection is nil")
	}

	switch m := msg.(type) {
	case TextMessage:
		data, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshal websocket text message: %w", err)
		}
		return conn.Write(ctx, websocket.MessageText, data)

	case BinaryFrame:
		header, err := json.Marshal(m.Header)
		if err != nil {
			return fmt.Errorf("marshal websocket binary frame header: %w", err)
		}

		var frame bytes.Buffer
		frame.Write(header)
		frame.WriteByte('\n')
		frame.Write(m.Data)

		return conn.Write(ctx, websocket.MessageBinary, frame.Bytes())

	default:
		return fmt.Errorf("unknown outbound message type %T", msg)
	}
}

func (c *Client) receiverLoop(ctx context.Context, conn websocketConn) error {
	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if messageType != websocket.MessageText {
			continue
		}

		if err := c.dispatchRaw(ctx, data); err != nil {
			continue
		}
	}
}

func (c *Client) heartbeatLoop(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.Outbound.Push(TextMessage{
				"command":   "HEARTBEAT",
				"clientId":  c.ClientID,
				"client_id": c.ClientID,
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
	}
}

func (c *Client) dispatchRaw(ctx context.Context, data []byte) error {
	var one TextMessage
	if err := json.Unmarshal(data, &one); err == nil {
		c.dispatch(ctx, one)
		return nil
	}

	var many []TextMessage
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}

	for _, msg := range many {
		c.dispatch(ctx, msg)
	}

	return nil
}

func (c *Client) dispatch(ctx context.Context, msg TextMessage) {
	kind, _ := msg["type"].(string)
	if kind == "" {
		kind, _ = msg["command"].(string)
	}

	if kind == "connected" {
		if payload, _ := msg["payload"].(map[string]any); payload != nil {
			if userID, ok := numberAsInt(payload["user_id"]); ok {
				c.UserID = userID
			}
		}
		return
	}

	if c.Handle != nil {
		c.Handle(ctx, msg)
	}
}

func numberAsInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func (c *Client) heartbeatMessage() TextMessage {
	return TextMessage{
		"command":   "HEARTBEAT",
		"clientId":  c.ClientID,
		"client_id": c.ClientID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
}
