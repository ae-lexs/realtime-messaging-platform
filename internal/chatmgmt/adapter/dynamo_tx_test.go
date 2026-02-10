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
// Helpers — build minimal TransactWriteItems for testing.
// ---------------------------------------------------------------------------

func dummyTxItem() dynamo.TransactWriteItem {
	tableName := "dummy"
	return dynamo.TransactWriteItem{
		Put: &dynamo.Put{
			TableName: &tableName,
			Item: map[string]dynamo.AttributeValue{
				"pk": &dynamo.AttributeValueMemberS{Value: "val"},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests — VerifyOTPAndCreateUser
// ---------------------------------------------------------------------------

func TestTransactor_VerifyOTPAndCreateUser(t *testing.T) {
	t.Run("success - sends 4 transaction items", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, params *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				assert.Len(t, params.TransactItems, 4)
				return &dynamo.TransactWriteItemsOutput{}, nil
			},
		}
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateUser(
			context.Background(),
			dummyTxItem(), // otpUpdate
			dummyTxItem(), // userPut
			dummyTxItem(), // phoneSentinelPut
			dummyTxItem(), // sessionPut
		)

		require.NoError(t, err)
	})

	t.Run("conditional check failed at user index - returns ErrAlreadyExists", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				// Index 1 (userPut) condition failed.
				return nil, dynamo.ErrTransactionCanceled("None", "ConditionalCheckFailed", "None", "None")
			},
		}
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateUser(
			context.Background(),
			dummyTxItem(), dummyTxItem(), dummyTxItem(), dummyTxItem(),
		)

		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrAlreadyExists)
		assert.Contains(t, err.Error(), "user_put")
	})

	t.Run("conditional check failed at phone sentinel - returns ErrAlreadyExists", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				// Index 2 (phoneSentinelPut) condition failed.
				return nil, dynamo.ErrTransactionCanceled("None", "None", "ConditionalCheckFailed", "None")
			},
		}
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateUser(
			context.Background(),
			dummyTxItem(), dummyTxItem(), dummyTxItem(), dummyTxItem(),
		)

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
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateUser(
			context.Background(),
			dummyTxItem(), dummyTxItem(), dummyTxItem(), dummyTxItem(),
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "transactor: verify otp and create user: service unavailable")
	})

	t.Run("transaction canceled without conditional check - wraps generically", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, dynamo.ErrTransactionCanceled("None", "None", "None", "None")
			},
		}
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateUser(
			context.Background(),
			dummyTxItem(), dummyTxItem(), dummyTxItem(), dummyTxItem(),
		)

		require.Error(t, err)
		assert.NotErrorIs(t, err, domain.ErrAlreadyExists)
		assert.Contains(t, err.Error(), "transaction canceled")
	})
}

// ---------------------------------------------------------------------------
// Tests — VerifyOTPAndCreateSession
// ---------------------------------------------------------------------------

func TestTransactor_VerifyOTPAndCreateSession(t *testing.T) {
	t.Run("success - sends 2 transaction items", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, params *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				assert.Len(t, params.TransactItems, 2)
				return &dynamo.TransactWriteItemsOutput{}, nil
			},
		}
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateSession(
			context.Background(),
			dummyTxItem(), // otpUpdate
			dummyTxItem(), // sessionPut
		)

		require.NoError(t, err)
	})

	t.Run("conditional check failed - returns ErrAlreadyExists", func(t *testing.T) {
		stub := &stubTxDynamo{
			transactWriteItemsFn: func(_ context.Context, _ *dynamo.TransactWriteItemsInput, _ ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error) {
				return nil, dynamo.ErrTransactionCanceled("ConditionalCheckFailed", "None")
			},
		}
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateSession(
			context.Background(),
			dummyTxItem(), dummyTxItem(),
		)

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
		tx := NewTransactor(stub)

		err := tx.VerifyOTPAndCreateSession(
			context.Background(),
			dummyTxItem(), dummyTxItem(),
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "transactor: verify otp and create session: network error")
	})
}
