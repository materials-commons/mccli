package wsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		Inbound: NewQueue[TextMessage](),
		Handle: func(ctx context.Context, msg TextMessage) {
			got <- msg
		},
	}

	handlerCtx, handlerCancel := context.WithCancel(ctx)
	defer handlerCancel()

	go func() {
		_ = client.handlerLoop(handlerCtx)
	}()

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

	queue := NewQueue[OutboundMessage]()
	queue.Push(TextMessage{"command": "PING"})

	conn := &fakeWebsocketConn{
		writeErr: errors.New("write failed"),
	}

	client := &Client{Outbound: queue}

	err := client.senderLoop(ctx, conn)
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

func TestClientSendTextMessageWithFakeConn(t *testing.T) {
	ctx := context.Background()

	conn := &fakeWebsocketConn{}
	client := &Client{}

	err := client.send(ctx, conn, TextMessage{
		"command": "PING",
	})
	if err != nil {
		t.Fatalf("send() error = %v, want nil", err)
	}

	if conn.lastWriteType != websocket.MessageText {
		t.Fatalf("lastWriteType = %v, want MessageText", conn.lastWriteType)
	}

	var got map[string]any
	if err := json.Unmarshal(conn.lastWriteData, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got["command"] != "PING" {
		t.Fatalf("command = %v, want PING", got["command"])
	}
}

func TestClientSendBinaryFrameWithFakeConn(t *testing.T) {
	ctx := context.Background()

	conn := &fakeWebsocketConn{}
	client := &Client{}

	err := client.send(ctx, conn, BinaryFrame{
		Header: map[string]any{
			"transfer_id": "transfer-1",
			"sequence":    0,
		},
		Data: []byte("hello"),
	})
	if err != nil {
		t.Fatalf("send() error = %v, want nil", err)
	}

	if conn.lastWriteType != websocket.MessageBinary {
		t.Fatalf("lastWriteType = %v, want MessageBinary", conn.lastWriteType)
	}

	parts := bytes.SplitN(conn.lastWriteData, []byte("\n"), 2)
	if len(parts) != 2 {
		t.Fatalf("binary frame missing separator: %q", string(conn.lastWriteData))
	}

	var header map[string]any
	if err := json.Unmarshal(parts[0], &header); err != nil {
		t.Fatalf("Unmarshal(header) error = %v", err)
	}

	if header["transfer_id"] != "transfer-1" {
		t.Fatalf("transfer_id = %v, want transfer-1", header["transfer_id"])
	}
	if string(parts[1]) != "hello" {
		t.Fatalf("data = %q, want hello", string(parts[1]))
	}
}

func TestClientReceiverLoopWithFakeConn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn := &fakeWebsocketConn{
		readMessages: []fakeReadMessage{
			{
				typ:  websocket.MessageText,
				data: []byte(`{"command":"TRANSFER_ACCEPT","payload":{"transfer_id":"t1"}}`),
			},
		},
		readErrAfterMessages: context.Canceled,
	}

	client := &Client{
		Inbound: NewQueue[TextMessage](),
	}

	err := client.receiverLoop(ctx, conn)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("receiverLoop() error = %v, want context.Canceled", err)
	}

	items := client.Inbound.Drain()
	if len(items) != 1 {
		t.Fatalf("len(Inbound.Drain()) = %d, want 1", len(items))
	}
	if items[0]["command"] != "TRANSFER_ACCEPT" {
		t.Fatalf("command = %v, want TRANSFER_ACCEPT", items[0]["command"])
	}
}

func TestClientBuildHeadersIncludesHostnameAndProjects(t *testing.T) {
	client := &Client{
		Token:      "token-123",
		ClientID:   "client-123",
		Hostname:   "host-123",
		ProjectIDs: []int{1, 2, 3},
	}

	headers := client.buildHeaders()

	if got := headers.Get("Authorization"); got != "Bearer token-123" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := headers.Get("MC-Client-ID"); got != "client-123" {
		t.Fatalf("MC-Client-ID = %q", got)
	}
	if got := headers.Get("MC-Client-Hostname"); got != "host-123" {
		t.Fatalf("MC-Client-Hostname = %q", got)
	}
	if got := headers.Get("MC-Client-Projects"); got != "1,2,3" {
		t.Fatalf("MC-Client-Projects = %q", got)
	}
	if got := headers.Get("MC-Connection-Type"); got != "cli" {
		t.Fatalf("MC-Connection-Type = %q", got)
	}
}

func TestClientDispatchRawQueuesArray(t *testing.T) {
	client := &Client{
		Inbound: NewQueue[TextMessage](),
	}

	err := client.dispatchRaw([]byte(`[
		{"command":"ONE"},
		{"command":"TWO"}
	]`))
	if err != nil {
		t.Fatalf("dispatchRaw() error = %v", err)
	}

	items := client.Inbound.Drain()
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}

	if items[0]["command"] != "ONE" || items[1]["command"] != "TWO" {
		t.Fatalf("items = %#v, want ONE then TWO", items)
	}
}

func TestClientDispatchConnectedStoresUserID(t *testing.T) {
	called := false

	client := &Client{
		Inbound: NewQueue[TextMessage](),
		Handle: func(ctx context.Context, msg TextMessage) {
			called = true
		},
	}

	client.dispatch(TextMessage{
		"command": "connected",
		"payload": map[string]any{
			"user_id": float64(42),
		},
	})

	if client.UserID != 42 {
		t.Fatalf("UserID = %d, want 42", client.UserID)
	}
	if called {
		t.Fatal("handler called for connected message, want not called")
	}
	if client.Inbound.Len() != 0 {
		t.Fatalf("Inbound.Len() = %d, want 0", client.Inbound.Len())
	}
}

func TestClientHeartbeatMessage(t *testing.T) {
	client := &Client{ClientID: "client-123"}

	msg := client.heartbeatMessage()

	if msg["command"] != "HEARTBEAT" {
		t.Fatalf("command = %v, want HEARTBEAT", msg["command"])
	}
	if msg["clientId"] != "client-123" {
		t.Fatalf("clientId = %v, want client-123", msg["clientId"])
	}
	if msg["client_id"] != "client-123" {
		t.Fatalf("client_id = %v, want client-123", msg["client_id"])
	}
	if msg["timestamp"] == "" {
		t.Fatal("timestamp is empty")
	}
}

func TestClientSenderLoopReturnsRequeueFailedWhenQueueClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	badMessage := TextMessage{
		"command": "PING",
		"bad":     func() {},
	}

	queue := NewQueue[OutboundMessage]()
	queue.Push(badMessage)

	client := &Client{Outbound: queue}

	// Close after Pop succeeds but before requeue.
	go func() {
		time.Sleep(10 * time.Millisecond)
		queue.Close()
	}()

	err := client.senderLoop(ctx, nil)
	if err == nil {
		t.Fatal("senderLoop() error = nil, want error")
	}
}

func TestClientReceiverLoopDoesNotBlockOnHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn := &fakeWebsocketConn{
		readMessages: []fakeReadMessage{
			{
				typ:  websocket.MessageText,
				data: []byte(`{"command":"ONE"}`),
			},
			{
				typ:  websocket.MessageText,
				data: []byte(`{"command":"TWO"}`),
			},
		},
		readErrAfterMessages: context.Canceled,
	}

	blockHandler := make(chan struct{})
	handlerStarted := make(chan string, 1)

	client := &Client{
		Inbound: NewQueue[TextMessage](),
		Handle: func(ctx context.Context, msg TextMessage) {
			command, _ := msg["command"].(string)
			handlerStarted <- command
			<-blockHandler
		},
	}

	handlerCtx, handlerCancel := context.WithCancel(ctx)
	defer handlerCancel()

	go func() {
		_ = client.handlerLoop(handlerCtx)
	}()

	err := client.receiverLoop(ctx, conn)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("receiverLoop() error = %v, want context.Canceled", err)
	}

	select {
	case first := <-handlerStarted:
		if first != "ONE" {
			t.Fatalf("first handler command = %q, want ONE", first)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for handler to start")
	}

	if client.Inbound.Len() != 1 {
		t.Fatalf("Inbound.Len() = %d, want 1 queued message while handler is blocked", client.Inbound.Len())
	}

	close(blockHandler)
}

func TestClientHandlerLoopProcessesInboundMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	client := &Client{
		Inbound: NewQueue[TextMessage](),
	}

	got := make(chan string, 2)
	client.Handle = func(ctx context.Context, msg TextMessage) {
		command, _ := msg["command"].(string)
		got <- command
	}

	client.Inbound.Push(TextMessage{"command": "ONE"})
	client.Inbound.Push(TextMessage{"command": "TWO"})

	go func() {
		_ = client.handlerLoop(ctx)
	}()

	for _, want := range []string{"ONE", "TWO"} {
		select {
		case gotCommand := <-got:
			if gotCommand != want {
				t.Fatalf("handler command = %q, want %q", gotCommand, want)
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for handler command %q", want)
		}
	}
}

func TestClientHandlerLoopRecoversPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	client := &Client{
		Inbound: NewQueue[TextMessage](),
	}

	handled := make(chan string, 1)
	count := 0

	client.Handle = func(ctx context.Context, msg TextMessage) {
		count++
		if count == 1 {
			panic("boom")
		}
		command, _ := msg["command"].(string)
		handled <- command
	}

	client.Inbound.Push(TextMessage{"command": "PANIC"})
	client.Inbound.Push(TextMessage{"command": "AFTER"})

	go func() {
		_ = client.handlerLoop(ctx)
	}()

	select {
	case got := <-handled:
		if got != "AFTER" {
			t.Fatalf("handled command = %q, want AFTER", got)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for handler after panic")
	}
}

func TestClassifyRunError(t *testing.T) {
	err := classifyRunError(errors.New("read failed"))
	if !errors.Is(err, ErrReconnect) {
		t.Fatalf("classifyRunError() = %v, want ErrReconnect", err)
	}

	err = classifyRunError(context.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("classifyRunError() = %v, want context.Canceled", err)
	}

	err = classifyRunError(ErrRequeueFailed)
	if !errors.Is(err, ErrRequeueFailed) {
		t.Fatalf("classifyRunError() = %v, want ErrRequeueFailed", err)
	}
}

func TestClientSenderLoopDoesNotRequeueInvalidOutboundMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	queue := NewQueue[OutboundMessage]()
	queue.Push(TextMessage{
		"command": "PING",
		"bad":     func() {},
	})

	client := &Client{Outbound: queue}

	err := client.senderLoop(ctx, &fakeWebsocketConn{})
	if !errors.Is(err, ErrInvalidOutboundMessage) {
		t.Fatalf("senderLoop() error = %v, want ErrInvalidOutboundMessage", err)
	}

	if queue.Len() != 0 {
		t.Fatalf("queue.Len() = %d, want 0 because invalid message should not be retried", queue.Len())
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

type fakeReadMessage struct {
	typ  websocket.MessageType
	data []byte
}

type fakeWebsocketConn struct {
	writeErr error
	readErr  error

	lastWriteType websocket.MessageType
	lastWriteData []byte

	readMessages         []fakeReadMessage
	readErrAfterMessages error

	closed bool
}

func (f *fakeWebsocketConn) Write(ctx context.Context, typ websocket.MessageType, data []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}

	f.lastWriteType = typ
	f.lastWriteData = append([]byte(nil), data...)
	return nil
}

func (f *fakeWebsocketConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	if f.readErr != nil {
		return 0, nil, f.readErr
	}

	if len(f.readMessages) == 0 {
		if f.readErrAfterMessages != nil {
			return 0, nil, f.readErrAfterMessages
		}
		return 0, nil, context.Canceled
	}

	msg := f.readMessages[0]
	f.readMessages = f.readMessages[1:]

	return msg.typ, msg.data, nil
}

func (f *fakeWebsocketConn) Close(code websocket.StatusCode, reason string) error {
	f.closed = true
	return nil
}
