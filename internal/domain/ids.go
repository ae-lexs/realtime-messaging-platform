// Package domain contains pure business logic and types.
// No external dependencies allowed - this is the innermost ring of Clean Architecture.
package domain

import (
	"fmt"

	"github.com/google/uuid"
)

// ChatID is a value object representing a unique chat identifier.
// Always valid in memory - use NewChatID to construct.
type ChatID struct {
	value string
}

// NewChatID creates a ChatID from a raw string, validating it is a valid UUID.
func NewChatID(raw string) (ChatID, error) {
	if raw == "" {
		return ChatID{}, ErrEmptyID
	}
	if _, err := uuid.Parse(raw); err != nil {
		return ChatID{}, fmt.Errorf("invalid chat ID %q: %w", raw, ErrInvalidID)
	}
	return ChatID{value: raw}, nil
}

// MustChatID creates a ChatID, panicking on invalid input. Use only in tests.
func MustChatID(raw string) ChatID {
	id, err := NewChatID(raw)
	if err != nil {
		panic(err)
	}
	return id
}

// GenerateChatID creates a new random ChatID.
func GenerateChatID() ChatID {
	return ChatID{value: uuid.NewString()}
}

func (id ChatID) String() string { return id.value }
func (id ChatID) IsZero() bool   { return id.value == "" }

// UserID is a value object representing a unique user identifier.
type UserID struct {
	value string
}

// NewUserID creates a UserID from a raw string, validating it is a valid UUID.
func NewUserID(raw string) (UserID, error) {
	if raw == "" {
		return UserID{}, ErrEmptyID
	}
	if _, err := uuid.Parse(raw); err != nil {
		return UserID{}, fmt.Errorf("invalid user ID %q: %w", raw, ErrInvalidID)
	}
	return UserID{value: raw}, nil
}

// MustUserID creates a UserID, panicking on invalid input. Use only in tests.
func MustUserID(raw string) UserID {
	id, err := NewUserID(raw)
	if err != nil {
		panic(err)
	}
	return id
}

// GenerateUserID creates a new random UserID.
func GenerateUserID() UserID {
	return UserID{value: uuid.NewString()}
}

func (id UserID) String() string { return id.value }
func (id UserID) IsZero() bool   { return id.value == "" }

// MessageID is a value object representing a unique message identifier.
type MessageID struct {
	value string
}

// NewMessageID creates a MessageID from a raw string, validating it is a valid UUID.
func NewMessageID(raw string) (MessageID, error) {
	if raw == "" {
		return MessageID{}, ErrEmptyID
	}
	if _, err := uuid.Parse(raw); err != nil {
		return MessageID{}, fmt.Errorf("invalid message ID %q: %w", raw, ErrInvalidID)
	}
	return MessageID{value: raw}, nil
}

// MustMessageID creates a MessageID, panicking on invalid input. Use only in tests.
func MustMessageID(raw string) MessageID {
	id, err := NewMessageID(raw)
	if err != nil {
		panic(err)
	}
	return id
}

// GenerateMessageID creates a new random MessageID.
func GenerateMessageID() MessageID {
	return MessageID{value: uuid.NewString()}
}

func (id MessageID) String() string { return id.value }
func (id MessageID) IsZero() bool   { return id.value == "" }

// SessionID is a value object representing a unique session identifier.
type SessionID struct {
	value string
}

// NewSessionID creates a SessionID from a raw string, validating it is a valid UUID.
func NewSessionID(raw string) (SessionID, error) {
	if raw == "" {
		return SessionID{}, ErrEmptyID
	}
	if _, err := uuid.Parse(raw); err != nil {
		return SessionID{}, fmt.Errorf("invalid session ID %q: %w", raw, ErrInvalidID)
	}
	return SessionID{value: raw}, nil
}

// MustSessionID creates a SessionID, panicking on invalid input. Use only in tests.
func MustSessionID(raw string) SessionID {
	id, err := NewSessionID(raw)
	if err != nil {
		panic(err)
	}
	return id
}

// GenerateSessionID creates a new random SessionID.
func GenerateSessionID() SessionID {
	return SessionID{value: uuid.NewString()}
}

func (id SessionID) String() string { return id.value }
func (id SessionID) IsZero() bool   { return id.value == "" }

// DeviceID is a value object representing a unique device identifier.
type DeviceID struct {
	value string
}

// NewDeviceID creates a DeviceID from a raw string, validating it is a valid UUID.
func NewDeviceID(raw string) (DeviceID, error) {
	if raw == "" {
		return DeviceID{}, ErrEmptyID
	}
	if _, err := uuid.Parse(raw); err != nil {
		return DeviceID{}, fmt.Errorf("invalid device ID %q: %w", raw, ErrInvalidID)
	}
	return DeviceID{value: raw}, nil
}

// MustDeviceID creates a DeviceID, panicking on invalid input. Use only in tests.
func MustDeviceID(raw string) DeviceID {
	id, err := NewDeviceID(raw)
	if err != nil {
		panic(err)
	}
	return id
}

// GenerateDeviceID creates a new random DeviceID.
func GenerateDeviceID() DeviceID {
	return DeviceID{value: uuid.NewString()}
}

func (id DeviceID) String() string { return id.value }
func (id DeviceID) IsZero() bool   { return id.value == "" }

// ClientMessageID is a value object representing a client-provided message identifier
// for idempotency purposes. Unlike other IDs, this is provided by the client.
type ClientMessageID struct {
	value string
}

// NewClientMessageID creates a ClientMessageID from a raw string.
// Client message IDs must be non-empty but can be any format the client chooses.
func NewClientMessageID(raw string) (ClientMessageID, error) {
	if raw == "" {
		return ClientMessageID{}, ErrEmptyID
	}
	if len(raw) > MaxClientMessageIDLength {
		return ClientMessageID{}, fmt.Errorf("client message ID exceeds max length %d: %w", MaxClientMessageIDLength, ErrInvalidID)
	}
	return ClientMessageID{value: raw}, nil
}

// MustClientMessageID creates a ClientMessageID, panicking on invalid input. Use only in tests.
func MustClientMessageID(raw string) ClientMessageID {
	id, err := NewClientMessageID(raw)
	if err != nil {
		panic(err)
	}
	return id
}

func (id ClientMessageID) String() string { return id.value }
func (id ClientMessageID) IsZero() bool   { return id.value == "" }

// Sequence represents a per-chat monotonically increasing message sequence number.
type Sequence uint64

// NewSequence creates a Sequence from a raw uint64.
func NewSequence(raw uint64) Sequence {
	return Sequence(raw)
}

func (s Sequence) Uint64() uint64 { return uint64(s) }
func (s Sequence) IsZero() bool   { return s == 0 }

// Next returns the next sequence number.
func (s Sequence) Next() Sequence {
	return Sequence(uint64(s) + 1)
}

// ConnectionID is a value object representing a unique WebSocket connection identifier.
type ConnectionID struct {
	value string
}

// NewConnectionID creates a ConnectionID from a raw string, validating it is a valid UUID.
func NewConnectionID(raw string) (ConnectionID, error) {
	if raw == "" {
		return ConnectionID{}, ErrEmptyID
	}
	if _, err := uuid.Parse(raw); err != nil {
		return ConnectionID{}, fmt.Errorf("invalid connection ID %q: %w", raw, ErrInvalidID)
	}
	return ConnectionID{value: raw}, nil
}

// GenerateConnectionID creates a new random ConnectionID.
func GenerateConnectionID() ConnectionID {
	return ConnectionID{value: uuid.NewString()}
}

func (id ConnectionID) String() string { return id.value }
func (id ConnectionID) IsZero() bool   { return id.value == "" }
