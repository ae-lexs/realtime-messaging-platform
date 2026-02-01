// Package protocol defines WebSocket protocol types per ADR-005.
// These types are used for client-server communication over WebSocket.
package protocol

import "encoding/json"

// FrameType identifies the type of WebSocket frame.
type FrameType string

const (
	// Connection lifecycle
	FrameTypeConnectionAck     FrameType = "connection_ack"
	FrameTypeConnectionClosing FrameType = "connection_closing"

	// Heartbeat
	FrameTypePing FrameType = "ping"
	FrameTypePong FrameType = "pong"

	// Messages (PR-6)
	FrameTypeSendMessage    FrameType = "send_message"
	FrameTypeSendMessageAck FrameType = "send_message_ack"
	FrameTypeMessage        FrameType = "message"
	FrameTypeAck            FrameType = "ack"

	// Sync (PR-6)
	FrameTypeSyncRequest  FrameType = "sync_request"
	FrameTypeSyncResponse FrameType = "sync_response"

	// Errors
	FrameTypeError FrameType = "error"
)

// Frame is the base structure for all WebSocket frames.
type Frame struct {
	Type    FrameType       `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ConnectionAck is sent by the server after successful WebSocket upgrade.
type ConnectionAck struct {
	ConnectionID        string `json:"connection_id"`
	HeartbeatIntervalMs int    `json:"heartbeat_interval_ms"`
}

// ConnectionClosing is sent by the server before closing the connection.
type ConnectionClosing struct {
	Reason string `json:"reason"`
	Code   int    `json:"code"`
}

// Ping is sent by the server to check client liveness.
type Ping struct {
	Timestamp int64 `json:"timestamp"`
}

// Pong is sent by the client in response to Ping.
type Pong struct {
	Timestamp int64 `json:"timestamp"`
}

// SendMessage is sent by the client to send a message.
type SendMessage struct {
	ChatID          string `json:"chat_id"`
	ClientMessageID string `json:"client_message_id"`
	ContentType     string `json:"content_type"`
	Content         string `json:"content"`
}

// SendMessageAck is sent by the server after message persistence.
type SendMessageAck struct {
	ClientMessageID string `json:"client_message_id"`
	MessageID       string `json:"message_id"`
	Sequence        uint64 `json:"sequence"`
	CreatedAt       int64  `json:"created_at"`
}

// Message is sent by the server to deliver a message to a recipient.
type Message struct {
	MessageID       string `json:"message_id"`
	ChatID          string `json:"chat_id"`
	SenderID        string `json:"sender_id"`
	ClientMessageID string `json:"client_message_id"`
	Sequence        uint64 `json:"sequence"`
	ContentType     string `json:"content_type"`
	Content         string `json:"content"`
	CreatedAt       int64  `json:"created_at"`
}

// Ack is sent by the client to acknowledge message receipt.
type Ack struct {
	ChatID   string `json:"chat_id"`
	Sequence uint64 `json:"sequence"`
}

// SyncRequest is sent by the client to request missed messages.
type SyncRequest struct {
	ChatID            string `json:"chat_id"`
	LastAckedSequence uint64 `json:"last_acked_sequence"`
}

// SyncResponse is sent by the server with missed messages.
type SyncResponse struct {
	ChatID   string    `json:"chat_id"`
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
}

// Error is sent by the server to report an error.
type Error struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// NewFrame creates a Frame with the given type and payload.
func NewFrame(frameType FrameType, payload interface{}) (*Frame, error) {
	var payloadBytes json.RawMessage
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	return &Frame{
		Type:    frameType,
		Payload: payloadBytes,
	}, nil
}

// ParsePayload unmarshals the frame payload into the given struct.
func (f *Frame) ParsePayload(v interface{}) error {
	if f.Payload == nil {
		return nil
	}
	return json.Unmarshal(f.Payload, v)
}
