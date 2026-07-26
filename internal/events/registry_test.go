package events_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/events"
)

func TestNewRegistryClientRequiresURLAndToken(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		token   string
		wantErr string
	}{
		{name: "no URL", token: "t", wantErr: "URL is required"},
		{name: "no token", url: "https://example.invalid", wantErr: "token is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			client, err := events.NewRegistryClient(tt.url, tt.token)

			// Assert
			require.Error(t, err)
			assert.Nil(t, client)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestSchemaTextReadsEverySource pins the paths in Sources: the registry stores
// schema text, so a moved or renamed .proto silently publishes nothing until
// this fails.
func TestSchemaTextReadsEverySource(t *testing.T) {
	for _, source := range events.Sources() {
		t.Run(source.Subject, func(t *testing.T) {
			// Act
			schema, err := events.SchemaText(filepath.Join(repoRoot, source.File))

			// Assert
			require.NoError(t, err)
			assert.Contains(t, schema, "syntax = \"proto3\";")
		})
	}
}

// TestSubjectNamesAvoidTheReservedPrefix records a registry rule that is not
// obvious: subjects starting with "google" are rejected outright ("not in valid
// format"), which is why the vendored well-known type is registered under
// wkt.timestamp.
func TestSubjectNamesAvoidTheReservedPrefix(t *testing.T) {
	for _, source := range events.Sources() {
		assert.False(t, strings.HasPrefix(source.Subject, "google"), "%s is not a valid subject name", source.Subject)
	}
}

func TestSchemaTextMissingFile(t *testing.T) {
	// Act
	_, err := events.SchemaText(filepath.Join(t.TempDir(), "absent.proto"))

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read schema")
}

func TestSchemaIDs(t *testing.T) {
	// Arrange
	states := []events.SubjectState{
		{Subject: "messages.persisted-value", ID: 11},
		{Subject: "chats.created-value", ID: 33},
	}

	// Act
	ids := events.SchemaIDs(states)

	// Assert
	assert.Equal(t, map[string]int{"messages.persisted-value": 11, "chats.created-value": 33}, ids)
}
