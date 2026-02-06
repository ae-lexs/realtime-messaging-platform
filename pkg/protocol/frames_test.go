package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFrame_AllTypes(t *testing.T) {
	tests := []struct {
		name      string
		frameType protocol.FrameType
		payload   interface{}
	}{
		{name: "ConnectionAck", frameType: protocol.FrameTypeConnectionAck, payload: protocol.ConnectionAck{ConnectionID: "conn-1", HeartbeatIntervalMs: 30000}},
		{name: "ConnectionClosing", frameType: protocol.FrameTypeConnectionClosing, payload: protocol.ConnectionClosing{Reason: "going away", Code: 1001}},
		{name: "Ping", frameType: protocol.FrameTypePing, payload: protocol.Ping{Timestamp: 1234567890}},
		{name: "Pong", frameType: protocol.FrameTypePong, payload: protocol.Pong{Timestamp: 1234567890}},
		{name: "SendMessage", frameType: protocol.FrameTypeSendMessage, payload: protocol.SendMessage{ChatID: "chat-1", ClientMessageID: "cmid-1", ContentType: "text", Content: "hello"}},
		{name: "SendMessageAck", frameType: protocol.FrameTypeSendMessageAck, payload: protocol.SendMessageAck{ClientMessageID: "cmid-1", MessageID: "msg-1", Sequence: 42, CreatedAt: 1234567890}},
		{name: "Message", frameType: protocol.FrameTypeMessage, payload: protocol.Message{MessageID: "msg-1", ChatID: "chat-1", SenderID: "user-1", ClientMessageID: "cmid-1", Sequence: 1, ContentType: "text", Content: "hello", CreatedAt: 1234567890}},
		{name: "Ack", frameType: protocol.FrameTypeAck, payload: protocol.Ack{ChatID: "chat-1", Sequence: 5}},
		{name: "SyncRequest", frameType: protocol.FrameTypeSyncRequest, payload: protocol.SyncRequest{ChatID: "chat-1", LastAckedSequence: 10}},
		{name: "SyncResponse", frameType: protocol.FrameTypeSyncResponse, payload: protocol.SyncResponse{ChatID: "chat-1", HasMore: true}},
		{name: "Error", frameType: protocol.FrameTypeError, payload: protocol.Error{Code: "INVALID_INPUT", Message: "bad request"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := protocol.NewFrame(tt.frameType, tt.payload)

			require.NoError(t, err)
			assert.Equal(t, tt.frameType, frame.Type)
			assert.NotNil(t, frame.Payload)
		})
	}
}

func TestNewFrame_NilPayload(t *testing.T) {
	frame, err := protocol.NewFrame(protocol.FrameTypePing, nil)

	require.NoError(t, err)
	assert.Equal(t, protocol.FrameTypePing, frame.Type)
	assert.Nil(t, frame.Payload)
}

func TestParsePayload_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		frameType protocol.FrameType
		payload   interface{}
		target    interface{}
		assert    func(t *testing.T, target interface{})
	}{
		{
			name:      "ConnectionAck",
			frameType: protocol.FrameTypeConnectionAck,
			payload:   protocol.ConnectionAck{ConnectionID: "conn-1", HeartbeatIntervalMs: 30000},
			target:    &protocol.ConnectionAck{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.ConnectionAck)
				assert.Equal(t, "conn-1", got.ConnectionID)
				assert.Equal(t, 30000, got.HeartbeatIntervalMs)
			},
		},
		{
			name:      "ConnectionClosing",
			frameType: protocol.FrameTypeConnectionClosing,
			payload:   protocol.ConnectionClosing{Reason: "going away", Code: 1001},
			target:    &protocol.ConnectionClosing{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.ConnectionClosing)
				assert.Equal(t, "going away", got.Reason)
				assert.Equal(t, 1001, got.Code)
			},
		},
		{
			name:      "Ping",
			frameType: protocol.FrameTypePing,
			payload:   protocol.Ping{Timestamp: 1234567890},
			target:    &protocol.Ping{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.Ping)
				assert.Equal(t, int64(1234567890), got.Timestamp)
			},
		},
		{
			name:      "Pong",
			frameType: protocol.FrameTypePong,
			payload:   protocol.Pong{Timestamp: 1234567890},
			target:    &protocol.Pong{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.Pong)
				assert.Equal(t, int64(1234567890), got.Timestamp)
			},
		},
		{
			name:      "SendMessage",
			frameType: protocol.FrameTypeSendMessage,
			payload:   protocol.SendMessage{ChatID: "chat-1", ClientMessageID: "cmid-1", ContentType: "text", Content: "hello"},
			target:    &protocol.SendMessage{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.SendMessage)
				assert.Equal(t, "chat-1", got.ChatID)
				assert.Equal(t, "cmid-1", got.ClientMessageID)
				assert.Equal(t, "text", got.ContentType)
				assert.Equal(t, "hello", got.Content)
			},
		},
		{
			name:      "SendMessageAck",
			frameType: protocol.FrameTypeSendMessageAck,
			payload:   protocol.SendMessageAck{ClientMessageID: "cmid-1", MessageID: "msg-1", Sequence: 42, CreatedAt: 1234567890},
			target:    &protocol.SendMessageAck{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.SendMessageAck)
				assert.Equal(t, "cmid-1", got.ClientMessageID)
				assert.Equal(t, "msg-1", got.MessageID)
				assert.Equal(t, uint64(42), got.Sequence)
				assert.Equal(t, int64(1234567890), got.CreatedAt)
			},
		},
		{
			name:      "Message",
			frameType: protocol.FrameTypeMessage,
			payload:   protocol.Message{MessageID: "msg-1", ChatID: "chat-1", SenderID: "user-1", ClientMessageID: "cmid-1", Sequence: 1, ContentType: "text", Content: "hello", CreatedAt: 1234567890},
			target:    &protocol.Message{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.Message)
				assert.Equal(t, "msg-1", got.MessageID)
				assert.Equal(t, "chat-1", got.ChatID)
				assert.Equal(t, "user-1", got.SenderID)
				assert.Equal(t, "cmid-1", got.ClientMessageID)
				assert.Equal(t, uint64(1), got.Sequence)
				assert.Equal(t, "text", got.ContentType)
				assert.Equal(t, "hello", got.Content)
				assert.Equal(t, int64(1234567890), got.CreatedAt)
			},
		},
		{
			name:      "Ack",
			frameType: protocol.FrameTypeAck,
			payload:   protocol.Ack{ChatID: "chat-1", Sequence: 5},
			target:    &protocol.Ack{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.Ack)
				assert.Equal(t, "chat-1", got.ChatID)
				assert.Equal(t, uint64(5), got.Sequence)
			},
		},
		{
			name:      "SyncRequest",
			frameType: protocol.FrameTypeSyncRequest,
			payload:   protocol.SyncRequest{ChatID: "chat-1", LastAckedSequence: 10},
			target:    &protocol.SyncRequest{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.SyncRequest)
				assert.Equal(t, "chat-1", got.ChatID)
				assert.Equal(t, uint64(10), got.LastAckedSequence)
			},
		},
		{
			name:      "SyncResponse",
			frameType: protocol.FrameTypeSyncResponse,
			payload:   protocol.SyncResponse{ChatID: "chat-1", Messages: []protocol.Message{{MessageID: "msg-1"}}, HasMore: true},
			target:    &protocol.SyncResponse{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.SyncResponse)
				assert.Equal(t, "chat-1", got.ChatID)
				require.Len(t, got.Messages, 1)
				assert.Equal(t, "msg-1", got.Messages[0].MessageID)
				assert.True(t, got.HasMore)
			},
		},
		{
			name:      "Error",
			frameType: protocol.FrameTypeError,
			payload:   protocol.Error{Code: "INVALID_INPUT", Message: "bad request", Details: map[string]string{"field": "chat_id"}},
			target:    &protocol.Error{},
			assert: func(t *testing.T, target interface{}) {
				t.Helper()
				got := target.(*protocol.Error)
				assert.Equal(t, "INVALID_INPUT", got.Code)
				assert.Equal(t, "bad request", got.Message)
				assert.Equal(t, "chat_id", got.Details["field"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create frame
			frame, err := protocol.NewFrame(tt.frameType, tt.payload)
			require.NoError(t, err)

			// Marshal to JSON and back
			data, err := json.Marshal(frame)
			require.NoError(t, err)

			var decoded protocol.Frame
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			// Parse payload
			err = decoded.ParsePayload(tt.target)
			require.NoError(t, err)

			tt.assert(t, tt.target)
		})
	}
}

func TestParsePayload_NilPayload(t *testing.T) {
	frame := &protocol.Frame{Type: protocol.FrameTypePing, Payload: nil}
	var target protocol.Ping

	err := frame.ParsePayload(&target)

	require.NoError(t, err)
	assert.Equal(t, protocol.Ping{}, target)
}

func TestNewFrame_UnmarshalablePayload(t *testing.T) {
	// Channels cannot be marshaled to JSON
	ch := make(chan int)

	_, err := protocol.NewFrame(protocol.FrameTypePing, ch)

	require.Error(t, err)
}

func TestFrameJSONStructure(t *testing.T) {
	frame, err := protocol.NewFrame(protocol.FrameTypePing, protocol.Ping{Timestamp: 100})
	require.NoError(t, err)

	data, err := json.Marshal(frame)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	assert.Contains(t, raw, "type")
	assert.Contains(t, raw, "payload")
}
