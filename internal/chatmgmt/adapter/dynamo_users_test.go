package adapter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// ---------------------------------------------------------------------------
// Stub — implements userDynamoDB for unit tests.
// ---------------------------------------------------------------------------

type stubUserDynamo struct {
	getItemFn func(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
	queryFn   func(ctx context.Context, params *dynamo.QueryInput, optFns ...func(*dynamo.Options)) (*dynamo.QueryOutput, error)
}

func (s *stubUserDynamo) GetItem(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
	return s.getItemFn(ctx, params, optFns...)
}

func (s *stubUserDynamo) Query(ctx context.Context, params *dynamo.QueryInput, optFns ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
	return s.queryFn(ctx, params, optFns...)
}

var _ userDynamoDB = (*stubUserDynamo)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const usersTable = "users"

func sampleUserItem() userItem {
	return userItem{
		UserID:      "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		PhoneNumber: "+15551234567",
		DisplayName: "Test User",
		CreatedAt:   "2026-02-10T12:00:00Z",
		UpdatedAt:   "2026-02-10T12:00:00Z",
	}
}

// ---------------------------------------------------------------------------
// Tests — GetByID
// ---------------------------------------------------------------------------

func TestUserStore_GetByID(t *testing.T) {
	tests := []struct {
		name      string
		getItemFn func(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
		wantErr   error
		errSubstr string
		wantUser  *UserRecord
	}{
		{
			name: "success - returns parsed user record",
			getItemFn: func(_ context.Context, params *dynamo.GetItemInput, _ ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
				assert.Equal(t, usersTable, *params.TableName)
				require.NotNil(t, params.ConsistentRead)
				assert.True(t, *params.ConsistentRead)

				item := sampleUserItem()
				av, err := dynamo.MarshalMap(item)
				require.NoError(t, err)
				return &dynamo.GetItemOutput{Item: av}, nil
			},
			wantUser: &UserRecord{
				UserID:      "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				PhoneNumber: "+15551234567",
				DisplayName: "Test User",
				CreatedAt:   "2026-02-10T12:00:00Z",
				UpdatedAt:   "2026-02-10T12:00:00Z",
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
				return nil, errors.New("connection refused")
			},
			errSubstr: "user store: get by id: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewUserStore(&stubUserDynamo{getItemFn: tt.getItemFn}, usersTable)

			rec, err := store.GetByID(context.Background(), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, rec)
				return
			}
			if tt.errSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				assert.Nil(t, rec)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, rec)
			assert.Equal(t, tt.wantUser.UserID, rec.UserID)
			assert.Equal(t, tt.wantUser.PhoneNumber, rec.PhoneNumber)
			assert.Equal(t, tt.wantUser.DisplayName, rec.DisplayName)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests — FindByPhone
// ---------------------------------------------------------------------------

func TestUserStore_FindByPhone(t *testing.T) {
	t.Run("success - queries GSI then fetches full record", func(t *testing.T) {
		item := sampleUserItem()
		av, err := dynamo.MarshalMap(item)
		require.NoError(t, err)

		stub := &stubUserDynamo{
			queryFn: func(_ context.Context, params *dynamo.QueryInput, _ ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
				assert.Equal(t, usersTable, *params.TableName)
				assert.NotNil(t, params.IndexName)
				assert.Equal(t, "phone_number-index", *params.IndexName)
				assert.Contains(t, *params.KeyConditionExpression, "phone_number = :phone")

				// Return projected item with just user_id.
				projected, marshalErr := dynamo.MarshalMap(struct {
					UserID string `dynamodbav:"user_id"`
				}{UserID: item.UserID})
				require.NoError(t, marshalErr)
				return &dynamo.QueryOutput{
					Items: []map[string]dynamo.AttributeValue{projected},
				}, nil
			},
			getItemFn: func(_ context.Context, params *dynamo.GetItemInput, _ ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
				keySV, ok := params.Key["user_id"].(*dynamo.AttributeValueMemberS)
				require.True(t, ok)
				assert.Equal(t, item.UserID, keySV.Value)
				return &dynamo.GetItemOutput{Item: av}, nil
			},
		}

		store := NewUserStore(stub, usersTable)

		rec, err := store.FindByPhone(context.Background(), "+15551234567")

		require.NoError(t, err)
		require.NotNil(t, rec)
		assert.Equal(t, item.UserID, rec.UserID)
		assert.Equal(t, item.PhoneNumber, rec.PhoneNumber)
	})

	t.Run("not found - empty GSI result returns ErrNotFound", func(t *testing.T) {
		stub := &stubUserDynamo{
			queryFn: func(_ context.Context, _ *dynamo.QueryInput, _ ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
				return &dynamo.QueryOutput{Items: nil}, nil
			},
		}
		store := NewUserStore(stub, usersTable)

		rec, err := store.FindByPhone(context.Background(), "+15559999999")

		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrNotFound)
		assert.Nil(t, rec)
	})

	t.Run("query error - wraps with context", func(t *testing.T) {
		stub := &stubUserDynamo{
			queryFn: func(_ context.Context, _ *dynamo.QueryInput, _ ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
				return nil, errors.New("throttled")
			},
		}
		store := NewUserStore(stub, usersTable)

		rec, err := store.FindByPhone(context.Background(), "+15551234567")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "user store: find by phone query: throttled")
		assert.Nil(t, rec)
	})

	t.Run("respects context cancellation between steps", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		stub := &stubUserDynamo{
			queryFn: func(_ context.Context, _ *dynamo.QueryInput, _ ...func(*dynamo.Options)) (*dynamo.QueryOutput, error) {
				// Cancel context after query succeeds but before GetItem.
				cancel()
				projected, err := dynamo.MarshalMap(struct {
					UserID string `dynamodbav:"user_id"`
				}{UserID: "some-id"})
				require.NoError(t, err)
				return &dynamo.QueryOutput{
					Items: []map[string]dynamo.AttributeValue{projected},
				}, nil
			},
			getItemFn: func(_ context.Context, _ *dynamo.GetItemInput, _ ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
				t.Fatal("GetItem should not be called after context cancellation")
				return nil, nil
			},
		}
		store := NewUserStore(stub, usersTable)

		_, err := store.FindByPhone(ctx, "+15551234567")

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}
