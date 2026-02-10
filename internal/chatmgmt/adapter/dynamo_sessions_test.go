package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// ---------------------------------------------------------------------------
// Stub — implements sessionDynamoDB for unit tests.
// ---------------------------------------------------------------------------

type stubSessionDynamo struct {
	getItemFn    func(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
	putItemFn    func(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error)
	queryFn      func(ctx context.Context, params *dynamo.QueryInput, optFns ...func(*dynamo.Options)) (*dynamo.QueryOutput, error)
	updateItemFn func(ctx context.Context, params *dynamo.UpdateItemInput, optFns ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error)
	deleteItemFn func(ctx context.Context, params *dynamo.DeleteItemInput, optFns ...func(*dynamo.Options)) (*dynamo.DeleteItemOutput, error)
}

func (s *stubSessionDynamo) GetItem(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
	return s.getItemFn(ctx, params, optFns...)
}

func (s *stubSessionDynamo) PutItem(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error) {
	return s.putItemFn(ctx, params, optFns...)
}

func (s *stubSessionDynamo) Query(ctx context.Context, params *dynamo.QueryInput, optFns ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
	return s.queryFn(ctx, params, optFns...)
}

func (s *stubSessionDynamo) UpdateItem(ctx context.Context, params *dynamo.UpdateItemInput, optFns ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error) {
	return s.updateItemFn(ctx, params, optFns...)
}

func (s *stubSessionDynamo) DeleteItem(ctx context.Context, params *dynamo.DeleteItemInput, optFns ...func(*dynamo.Options)) (*dynamo.DeleteItemOutput, error) {
	return s.deleteItemFn(ctx, params, optFns...)
}

var _ sessionDynamoDB = (*stubSessionDynamo)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const sessionsTable = "sessions"

func sessionFixedTime() time.Time {
	return time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
}

func sampleSessionItem() sessionItem {
	return sessionItem{
		SessionID:        "11111111-2222-3333-4444-555555555555",
		UserID:           "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		DeviceID:         "dddddddd-eeee-ffff-0000-111111111111",
		RefreshTokenHash: "hash-abc123",
		TokenGeneration:  1,
		PrevTokenHash:    "",
		CreatedAt:        "2026-02-10T12:00:00Z",
		ExpiresAt:        "2026-03-12T12:00:00Z",
		TTL:              sessionFixedTime().Add(30 * 24 * time.Hour).Unix(),
	}
}

func sampleSessionRecord() SessionRecord {
	si := sampleSessionItem()
	return SessionRecord(si)
}

// ---------------------------------------------------------------------------
// Tests — Create
// ---------------------------------------------------------------------------

func TestSessionStore_Create(t *testing.T) {
	tests := []struct {
		name      string
		putItemFn func(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error)
		wantErr   error
		errSubstr string
	}{
		{
			name: "success - writes session with condition",
			putItemFn: func(_ context.Context, params *dynamo.PutItemInput, _ ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error) {
				assert.Equal(t, sessionsTable, *params.TableName)
				require.NotNil(t, params.ConditionExpression)
				assert.Contains(t, *params.ConditionExpression, "attribute_not_exists(session_id)")
				assert.Contains(t, params.Item, "session_id")
				assert.Contains(t, params.Item, "user_id")
				assert.Contains(t, params.Item, "refresh_token_hash")
				return &dynamo.PutItemOutput{}, nil
			},
		},
		{
			name: "conditional check failed - returns ErrAlreadyExists",
			putItemFn: func(_ context.Context, _ *dynamo.PutItemInput, _ ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error) {
				return nil, dynamo.ErrConditionalCheckFailed()
			},
			wantErr: domain.ErrAlreadyExists,
		},
		{
			name: "dynamo error - wraps with context",
			putItemFn: func(_ context.Context, _ *dynamo.PutItemInput, _ ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error) {
				return nil, errors.New("connection refused")
			},
			errSubstr: "session store: create: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := domaintest.NewFakeClock(sessionFixedTime())
			store := NewSessionStore(&stubSessionDynamo{putItemFn: tt.putItemFn}, sessionsTable, clock)

			err := store.Create(context.Background(), sampleSessionRecord())

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			if tt.errSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests — GetByID
// ---------------------------------------------------------------------------

func TestSessionStore_GetByID(t *testing.T) {
	tests := []struct {
		name      string
		getItemFn func(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
		wantErr   error
		errSubstr string
	}{
		{
			name: "success - returns parsed session record",
			getItemFn: func(_ context.Context, params *dynamo.GetItemInput, _ ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
				assert.Equal(t, sessionsTable, *params.TableName)
				require.NotNil(t, params.ConsistentRead)
				assert.True(t, *params.ConsistentRead)

				item := sampleSessionItem()
				av, err := dynamo.MarshalMap(item)
				require.NoError(t, err)
				return &dynamo.GetItemOutput{Item: av}, nil
			},
		},
		{
			name: "not found - nil item returns ErrNotFound",
			getItemFn: func(_ context.Context, _ *dynamo.GetItemInput, _ ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
				return &dynamo.GetItemOutput{Item: nil}, nil
			},
			wantErr: domain.ErrNotFound,
		},
		{
			name: "dynamo error - wraps with context",
			getItemFn: func(_ context.Context, _ *dynamo.GetItemInput, _ ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
				return nil, errors.New("throttled")
			},
			errSubstr: "session store: get by id: throttled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := domaintest.NewFakeClock(sessionFixedTime())
			store := NewSessionStore(&stubSessionDynamo{getItemFn: tt.getItemFn}, sessionsTable, clock)

			rec, err := store.GetByID(context.Background(), "11111111-2222-3333-4444-555555555555")

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, rec)
				return
			}
			if tt.errSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rec)
			assert.Equal(t, "11111111-2222-3333-4444-555555555555", rec.SessionID)
			assert.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", rec.UserID)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests — ListByUser
// ---------------------------------------------------------------------------

func TestSessionStore_ListByUser(t *testing.T) {
	t.Run("success - returns active sessions from GSI", func(t *testing.T) {
		item := sampleSessionItem()
		av, err := dynamo.MarshalMap(item)
		require.NoError(t, err)

		clock := domaintest.NewFakeClock(sessionFixedTime())
		store := NewSessionStore(&stubSessionDynamo{
			queryFn: func(_ context.Context, params *dynamo.QueryInput, _ ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
				assert.Equal(t, sessionsTable, *params.TableName)
				assert.Equal(t, "user_sessions-index", *params.IndexName)
				assert.Contains(t, *params.KeyConditionExpression, "user_id = :uid")
				require.NotNil(t, params.FilterExpression)
				assert.Contains(t, *params.FilterExpression, "expires_at > :now")
				return &dynamo.QueryOutput{Items: []map[string]dynamo.AttributeValue{av}}, nil
			},
		}, sessionsTable, clock)

		sessions, err := store.ListByUser(context.Background(), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, item.SessionID, sessions[0].SessionID)
	})

	t.Run("empty result - returns empty slice", func(t *testing.T) {
		clock := domaintest.NewFakeClock(sessionFixedTime())
		store := NewSessionStore(&stubSessionDynamo{
			queryFn: func(_ context.Context, _ *dynamo.QueryInput, _ ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
				return &dynamo.QueryOutput{Items: nil}, nil
			},
		}, sessionsTable, clock)

		sessions, err := store.ListByUser(context.Background(), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	t.Run("query error - wraps with context", func(t *testing.T) {
		clock := domaintest.NewFakeClock(sessionFixedTime())
		store := NewSessionStore(&stubSessionDynamo{
			queryFn: func(_ context.Context, _ *dynamo.QueryInput, _ ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
				return nil, errors.New("timeout")
			},
		}, sessionsTable, clock)

		sessions, err := store.ListByUser(context.Background(), "user-id")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "session store: list by user: timeout")
		assert.Nil(t, sessions)
	})
}

// ---------------------------------------------------------------------------
// Tests — Update
// ---------------------------------------------------------------------------

func TestSessionStore_Update(t *testing.T) {
	t.Run("success - sends correct update expression", func(t *testing.T) {
		clock := domaintest.NewFakeClock(sessionFixedTime())
		store := NewSessionStore(&stubSessionDynamo{
			updateItemFn: func(_ context.Context, params *dynamo.UpdateItemInput, _ ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error) {
				assert.Equal(t, sessionsTable, *params.TableName)
				keySV, ok := params.Key["session_id"].(*dynamo.AttributeValueMemberS)
				require.True(t, ok)
				assert.Equal(t, "session-abc", keySV.Value)
				require.NotNil(t, params.UpdateExpression)
				assert.Contains(t, *params.UpdateExpression, "refresh_token_hash = :rth")
				assert.Contains(t, *params.UpdateExpression, "token_generation = :gen")
				assert.Contains(t, *params.UpdateExpression, "prev_token_hash = :pth")
				return &dynamo.UpdateItemOutput{}, nil
			},
		}, sessionsTable, clock)

		err := store.Update(context.Background(), "session-abc", SessionUpdate{
			RefreshTokenHash: "new-hash",
			TokenGeneration:  2,
			PrevTokenHash:    "old-hash",
			ExpiresAt:        "2026-03-12T12:00:00Z",
			TTL:              sessionFixedTime().Add(30 * 24 * time.Hour).Unix(),
		})

		require.NoError(t, err)
	})

	t.Run("dynamo error - wraps with context", func(t *testing.T) {
		clock := domaintest.NewFakeClock(sessionFixedTime())
		store := NewSessionStore(&stubSessionDynamo{
			updateItemFn: func(_ context.Context, _ *dynamo.UpdateItemInput, _ ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error) {
				return nil, errors.New("internal error")
			},
		}, sessionsTable, clock)

		err := store.Update(context.Background(), "session-abc", SessionUpdate{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "session store: update: internal error")
	})
}

// ---------------------------------------------------------------------------
// Tests — Delete
// ---------------------------------------------------------------------------

func TestSessionStore_Delete(t *testing.T) {
	t.Run("success - deletes by session ID", func(t *testing.T) {
		clock := domaintest.NewFakeClock(sessionFixedTime())
		store := NewSessionStore(&stubSessionDynamo{
			deleteItemFn: func(_ context.Context, params *dynamo.DeleteItemInput, _ ...func(*dynamo.Options)) (*dynamo.DeleteItemOutput, error) {
				assert.Equal(t, sessionsTable, *params.TableName)
				keySV, ok := params.Key["session_id"].(*dynamo.AttributeValueMemberS)
				require.True(t, ok)
				assert.Equal(t, "session-to-delete", keySV.Value)
				return &dynamo.DeleteItemOutput{}, nil
			},
		}, sessionsTable, clock)

		err := store.Delete(context.Background(), "session-to-delete")

		require.NoError(t, err)
	})

	t.Run("dynamo error - wraps with context", func(t *testing.T) {
		clock := domaintest.NewFakeClock(sessionFixedTime())
		store := NewSessionStore(&stubSessionDynamo{
			deleteItemFn: func(_ context.Context, _ *dynamo.DeleteItemInput, _ ...func(*dynamo.Options)) (*dynamo.DeleteItemOutput, error) {
				return nil, errors.New("access denied")
			},
		}, sessionsTable, clock)

		err := store.Delete(context.Background(), "session-abc")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "session store: delete: access denied")
	})
}
