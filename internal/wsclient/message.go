// Package wsclient provides a reconnecting websocket client and outbound queue.
package wsclient

import "time"

// TextMessage is a JSON websocket message.
type TextMessage map[string]any

// BinaryFrame is sent as:
//
//	JSON header + "\n" + raw binary data
type BinaryFrame struct {
	Header map[string]any
	Data   []byte
}

// OutboundMessage is either TextMessage or BinaryFrame.
type OutboundMessage interface {
	isOutboundMessage()
}

func (TextMessage) isOutboundMessage() {}
func (BinaryFrame) isOutboundMessage() {}

// Envelope contains common Materials Commons websocket message fields.
type Envelope struct {
	Command   string         `json:"command"`
	ID        string         `json:"id,omitempty"`
	Timestamp time.Time      `json:"timestamp,omitempty"`
	ClientID  string         `json:"client_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}
