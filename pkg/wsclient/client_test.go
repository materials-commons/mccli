package wsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestClientSendTextMessage(t *testing.T) {
	server := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("server Read() error = %v", err)
			return
		}
		if messageType != websocket.MessageText {
			t.Errorf("messageType = %v, want MessageText", messageType)
			return
		}

		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Errorf("Unmarshal() error = %v", err)
			return
		}
		if msg["command"] != "PING" {
			t.Errorf("command = %v, want PING", msg["command"])
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	client := &Client{}

	err = client.send(ctx, conn, TextMessage{
		"command": "PING",
	})
	if err != nil {
		t.Fatalf("send() error = %v", err)
	}
}

func TestClientSendBinaryFrame(t *testing.T) {
	server := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("server Read() error = %v", err)
			return
		}
		if messageType != websocket.MessageBinary {
			t.Errorf("messageType = %v, want MessageBinary", messageType)
			return
		}

		parts := bytes.SplitN(data, []byte("\n"), 2)
		if len(parts) != 2 {
			t.Fatalf("binary frame missing header separator: %q", string(data))
		}

		var header map[string]any
		if err := json.Unmarshal(parts[0], &header); err != nil {
			t.Errorf("header Unmarshal() error = %v", err)
			return
		}

		if header["transfer_id"] != "transfer-1" {
			t.Errorf("transfer_id = %v, want transfer-1", header["transfer_id"])
		}
		if string(parts[1]) != "hello" {
			t.Errorf("data = %q, want hello", string(parts[1]))
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	client := &Client{}

	err = client.send(ctx, conn, BinaryFrame{
		Header: map[string]any{
			"transfer_id": "transfer-1",
			"sequence":    0,
		},
		Data: []byte("hello"),
	})
	if err != nil {
		t.Fatalf("send() error = %v", err)
	}
}

func TestClientReceiverLoopDispatchesTextMessage(t *testing.T) {
	server := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		err := conn.Write(ctx, websocket.MessageText, []byte(`{"command":"TRANSFER_ACCEPT","payload":{"transfer_id":"t1"}}`))
		if err != nil {
			t.Errorf("server Write() error = %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	got := make(chan TextMessage, 1)
	client := &Client{
		Handle: func(ctx context.Context, msg TextMessage) {
			got <- msg
		},
	}

	go func() {
		_ = client.receiverLoop(ctx, conn)
	}()

	select {
	case msg := <-got:
		if msg["command"] != "TRANSFER_ACCEPT" {
			t.Fatalf("command = %v, want TRANSFER_ACCEPT", msg["command"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for receiver dispatch")
	}
}

func TestClientRunSendsHeaders(t *testing.T) {
	headersSeen := make(chan http.Header, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headersSeen <- r.Header.Clone()

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept() error = %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &Client{
		URL:      wsURL,
		Token:    "token-123",
		ClientID: "client-123",
		Outbound: NewQueue[OutboundMessage](),
	}

	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx)
	}()

	select {
	case headers := <-headersSeen:
		if got := headers.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("Authorization = %q, want Bearer token-123", got)
		}
		if got := headers.Get("MC-Client-ID"); got != "client-123" {
			t.Fatalf("MC-Client-ID = %q, want client-123", got)
		}
		if got := headers.Get("MC-Connection-Type"); got != "cli" {
			t.Fatalf("MC-Connection-Type = %q, want cli", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket headers")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Client.Run to stop")
	}
}

func TestClientSenderLoopRequeuesOnSendFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	badMessage := TextMessage{
		"command": "PING",
		"bad":     func() {},
	}

	queue := NewQueue[OutboundMessage]()
	queue.Push(badMessage)

	client := &Client{Outbound: queue}

	err := client.senderLoop(ctx, nil)
	if err == nil {
		t.Fatal("senderLoop() error = nil, want error")
	}

	if queue.Len() != 1 {
		t.Fatalf("queue.Len() = %d, want 1 requeued message", queue.Len())
	}

	items := queue.Drain()
	if len(items) != 1 {
		t.Fatalf("len(Drain()) = %d, want 1", len(items))
	}

	msg, ok := items[0].(TextMessage)
	if !ok {
		t.Fatalf("requeued message type = %T, want TextMessage", items[0])
	}
	if msg["command"] != "PING" {
		t.Fatalf("requeued command = %v, want PING", msg["command"])
	}
}

func newWSTestServer(t *testing.T, handler func(ctx context.Context, conn *websocket.Conn)) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept() error = %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		handler(r.Context(), conn)
	}))

	server.URL = "ws" + strings.TrimPrefix(server.URL, "http")
	return server
}
