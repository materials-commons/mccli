package wsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

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
			backoff = c.ReconnectMin
			continue
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
	headers.Set("MC-Connection-Type", "cli")

	conn, _, err := websocket.Dial(ctx, c.URL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	errCh := make(chan error, 3)
	go func() { errCh <- c.senderLoop(ctx, conn) }()
	go func() { errCh <- c.receiverLoop(ctx, conn) }()
	go func() { errCh <- c.heartbeatLoop(ctx) }()

	return <-errCh
}

func (c *Client) senderLoop(ctx context.Context, conn *websocket.Conn) error {
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
			c.Outbound.Push(msg)
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, conn *websocket.Conn, msg OutboundMessage) error {
	switch m := msg.(type) {
	case TextMessage:
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, data)

	case BinaryFrame:
		header, err := json.Marshal(m.Header)
		if err != nil {
			return err
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

func (c *Client) receiverLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if messageType != websocket.MessageText {
			continue
		}

		var msg TextMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		if c.Handle != nil {
			c.Handle(ctx, msg)
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
				"client_id": c.ClientID,
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
	}
}
