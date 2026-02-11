package adapter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// ---------------------------------------------------------------------------
// Stub — implements txDynamoDB for unit tests.
// ---------------------------------------------------------------------------

type stubTxDynamo struct {
	transactWriteItemsFn func(ctx context.Context, params *dynamo.TransactWriteItemsInput, optFns ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error)
}

func (s *stubTxDynamo) TransactWriteItems(ctx context.Context, params *dynamo.TransactWriteItemsInput, optFns ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
	return s.transactWriteItemsFn(ctx, params, optFns...)
}

var _ txDynamoDB = (*stubTxDynamo)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	txOTPTable      = "otp_requests"
	txUsersTable    = "users"
	txSessionsTable = "sessions"
)

func sampleRegistrationParams() app.RegistrationParams {
	return app.RegistrationParams{
		PhoneHash:        "sha256-phone-hash",
		OTPExpiresAt:     "2026-02-10T12:05:00Z",
		OTPMAC:           "hmac-abc123",
		UserID:           "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		PhoneNumber:      "+15551234567",
		Now:              "2026-02-10T12:00:00Z",
		SessionID:        "11111111-2222-3333-4444-555555555555",
		DeviceID:         "dddddddd-eeee-ffff-0000-111111111111",
		RefreshTokenHash: "hash-refresh-abc",
		SessionExpiresAt: "2026-03-12T12:00:00Z",
		SessionTTL:       1741608000,
	}
}

func sampleLoginParams() app.LoginParams {
	return app.LoginParams{
		PhoneHash:        "sha256-phone-hash",
		OTPExpiresAt:     "2026-02-10T12:05:00Z",
		OTPMAC:           "hmac-abc123",
		SessionID:        "11111111-2222-3333-4444-555555555555",
		UserID:           "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		DeviceID:         "dddddddd-eeee-ffff-0000-111111111111",
		RefreshTokenHash: "hash-refresh-abc",
		CreatedAt:        "2026-02-10T12:00:00Z",
		SessionExpiresAt: "2026-03-12T12:00:00Z",
		SessionTTL:       1741608000,
	}
}

// ---------------------------------------------------------------------------
// Tests — VerifyOTPAndCreateUser
// ---------------------------------------------------------------------------

func TestTransactor_VerifyOTPAndCreateUser(t *testing.T) {
	t.Run("success - sends 4 transaction items with correct tables", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, params *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				require.Len(t, params.TransactItems, 4)

				// [0] OTP update — targets otp_requests table.
				assert.NotNil(t, params.TransactItems[0].Update)
				assert.Equal(t, txOTPTable, *params.TransactItems[0].Update.TableName)

				// [1] User put — targets users table.
				assert.NotNil(t, params.TransactItems[1].Put)
				assert.Equal(t, txUsersTable, *params.TransactItems[1].Put.TableName)

				// [2] Phone sentinel put — targets users table.
				assert.NotNil(t, params.TransactItems[2].Put)
				assert.Equal(t, txUsersTable, *params.TransactItems[2].Put.TableName)

				// [3] Session put — targets sessions table.
				assert.NotNil(t, params.TransactItems[3].Put)
				assert.Equal(t, txSessionsTable, *params.TransactItems[3].Put.TableName)

				return &dynamo.TransactWriteItemsOutput{}, nil
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), sampleRegistrationParams())

		require.NoError(t, err)
	})

	t.Run("otp update - verifies condition and key", func(t *testing.T) {
		p := sampleRegistrationParams()
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, params *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				otpUpdate := params.TransactItems[0].Update
				require.NotNil(t, otpUpdate)

				// Key is phone_hash.
				keySV, ok := otpUpdate.Key["phone_hash"].(*dynamo.AttributeValueMemberS)
				require.True(t, ok)
				assert.Equal(t, p.PhoneHash, keySV.Value)

				// Condition includes status = pending AND otp_mac check.
				require.NotNil(t, otpUpdate.ConditionExpression)
				assert.Contains(t, *otpUpdate.ConditionExpression, "#st = :pending")
				assert.Contains(t, *otpUpdate.ConditionExpression, "otp_mac = :mac")

				return &dynamo.TransactWriteItemsOutput{}, nil
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), p)

		require.NoError(t, err)
	})

	t.Run("user put - creates user with condition", func(t *testing.T) {
		p := sampleRegistrationParams()
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, params *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				userPut := params.TransactItems[1].Put
				require.NotNil(t, userPut)
				require.NotNil(t, userPut.ConditionExpression)
				assert.Contains(t, *userPut.ConditionExpression, "attribute_not_exists(user_id)")
				assert.Contains(t, userPut.Item, "user_id")
				assert.Contains(t, userPut.Item, "phone_number")

				return &dynamo.TransactWriteItemsOutput{}, nil
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), p)

		require.NoError(t, err)
	})

	t.Run("session put - creates session with condition", func(t *testing.T) {
		p := sampleRegistrationParams()
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, params *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				sessionPut := params.TransactItems[3].Put
				require.NotNil(t, sessionPut)
				require.NotNil(t, sessionPut.ConditionExpression)
				assert.Contains(t, *sessionPut.ConditionExpression, "attribute_not_exists(session_id)")
				assert.Contains(t, sessionPut.Item, "session_id")
				assert.Contains(t, sessionPut.Item, "refresh_token_hash")

				return &dynamo.TransactWriteItemsOutput{}, nil
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), p)

		require.NoError(t, err)
	})

	t.Run("conditional check failed at user index - returns ErrAlreadyExists", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, dynamo.ErrTransactionCanceled("None", "ConditionalCheckFailed", "None", "None")
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), sampleRegistrationParams())

		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrAlreadyExists)
		assert.Contains(t, err.Error(), "user_put")
	})

	t.Run("conditional check failed at phone sentinel - returns ErrAlreadyExists", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, dynamo.ErrTransactionCanceled("None", "None", "ConditionalCheckFailed", "None")
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), sampleRegistrationParams())

		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrAlreadyExists)
		assert.Contains(t, err.Error(), "phone_sentinel")
	})

	t.Run("non-transaction error - wraps with context", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, errors.New("service unavailable")
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), sampleRegistrationParams())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "transactor: verify otp and create user: service unavailable")
	})

	t.Run("transaction canceled without conditional check - wraps generically", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, dynamo.ErrTransactionCanceled("None", "None", "None", "None")
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateUser(context.Background(), sampleRegistrationParams())

		require.Error(t, err)
		assert.NotErrorIs(t, err, domain.ErrAlreadyExists)
		assert.Contains(t, err.Error(), "transaction canceled")
	})
}

// ---------------------------------------------------------------------------
// Tests — VerifyOTPAndCreateSession
// ---------------------------------------------------------------------------

func TestTransactor_VerifyOTPAndCreateSession(t *testing.T) {
	t.Run("success - sends 2 transaction items with correct tables", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, params *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				require.Len(t, params.TransactItems, 2)

				// [0] OTP update — targets otp_requests table.
				assert.NotNil(t, params.TransactItems[0].Update)
				assert.Equal(t, txOTPTable, *params.TransactItems[0].Update.TableName)

				// [1] Session put — targets sessions table.
				assert.NotNil(t, params.TransactItems[1].Put)
				assert.Equal(t, txSessionsTable, *params.TransactItems[1].Put.TableName)

				return &dynamo.TransactWriteItemsOutput{}, nil
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateSession(context.Background(), sampleLoginParams())

		require.NoError(t, err)
	})

	t.Run("conditional check failed - returns ErrAlreadyExists", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, dynamo.ErrTransactionCanceled("ConditionalCheckFailed", "None")
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateSession(context.Background(), sampleLoginParams())

		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrAlreadyExists)
		assert.Contains(t, err.Error(), "otp_update")
	})

	t.Run("non-transaction error - wraps with context", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, errors.New("network error")
			},
		}
		tx := NewTransactor(stub, txOTPTable, txUsersTable, txSessionsTable)

		err := tx.VerifyOTPAndCreateSession(context.Background(), sampleLoginParams())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "transactor: verify otp and create session: network error")
	})
}
