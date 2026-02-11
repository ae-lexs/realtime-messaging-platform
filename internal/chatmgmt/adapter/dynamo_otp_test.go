package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// ---------------------------------------------------------------------------
// Stub — implements otpDynamoDB for unit tests.
// ---------------------------------------------------------------------------

type stubOTPDynamo struct {
	getItemFn    func(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
	putItemFn    func(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error)
	updateItemFn func(ctx context.Context, params *dynamo.UpdateItemInput, optFns ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error)
}

func (s *stubOTPDynamo) GetItem(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
	return s.getItemFn(ctx, params, optFns...)
}

func (s *stubOTPDynamo) PutItem(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error) {
	return s.putItemFn(ctx, params, optFns...)
}

func (s *stubOTPDynamo) UpdateItem(ctx context.Context, params *dynamo.UpdateItemInput, optFns ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error) {
	return s.updateItemFn(ctx, params, optFns...)
}

// Compile-time check: stubOTPDynamo satisfies otpDynamoDB.
var _ otpDynamoDB = (*stubOTPDynamo)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testTable = "otp_requests"

func fixedTime() time.Time {
	return time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
}

func sampleRecord() app.OTPRecord {
	return app.OTPRecord{
		PhoneHash:     "abc123hash",
		OTPMAC:        "mac-value",
		OTPCiphertext: "encrypted-otp",
		CreatedAt:     "2026-02-10T12:00:00Z",
		ExpiresAt:     "2026-02-10T12:05:00Z",
		AttemptCount:  0,
		Status:        "pending",
		TTL:           fixedTime().Add(1 * time.Hour).Unix(),
	}
}

// ---------------------------------------------------------------------------
// Tests — CreateOTP
// ---------------------------------------------------------------------------

func TestCreateOTP(t *testing.T) {
	tests := []struct {
		name      string
		putItemFn func(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error)
		wantErr   error
		errSubstr string
	}{
		{
			name: "success - writes item to correct table with condition",
			putItemFn: func(_ context.Context, params *dynamo.PutItemInput, _ ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error) {
				// Verify the table name.
				assert.Equal(t, testTable, *params.TableName)

				// Verify the condition expression prevents overwriting active OTPs.
				require.NotNil(t, params.ConditionExpression)
				assert.Contains(t, *params.ConditionExpression, "attribute_not_exists(phone_hash)")
				assert.Contains(t, *params.ConditionExpression, "#st = :verified")
				assert.Contains(t, *params.ConditionExpression, "#ea < :now")

				// Verify expression attribute names map reserved words.
				assert.Equal(t, "status", params.ExpressionAttributeNames["#st"])
				assert.Equal(t, "expires_at", params.ExpressionAttributeNames["#ea"])

				// Verify the item contains the expected DynamoDB attributes.
				assert.Contains(t, params.Item, "phone_hash")
				assert.Contains(t, params.Item, "otp_mac")
				assert.Contains(t, params.Item, "otp_ciphertext")
				assert.Contains(t, params.Item, "status")
				assert.Contains(t, params.Item, "ttl")

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
			errSubstr: "otp store: create otp: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := domaintest.NewFakeClock(fixedTime())
			store := NewOTPStore(&stubOTPDynamo{
				putItemFn: tt.putItemFn,
			}, testTable, clock)

			err := store.CreateOTP(context.Background(), sampleRecord())

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
// Tests — GetOTP
// ---------------------------------------------------------------------------

func TestGetOTP(t *testing.T) {
	tests := []struct {
		name      string
		getItemFn func(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
		wantErr   error
		errSubstr string
		wantRec   *app.OTPRecord
	}{
		{
			name: "success - returns parsed record with correct fields",
			getItemFn: func(_ context.Context, params *dynamo.GetItemInput, _ ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error) {
				// Verify strongly consistent read.
				require.NotNil(t, params.ConsistentRead)
				assert.True(t, *params.ConsistentRead)

				// Verify correct table.
				assert.Equal(t, testTable, *params.TableName)

				// Verify the key used for lookup.
				keySV, ok := params.Key["phone_hash"].(*dynamo.AttributeValueMemberS)
				require.True(t, ok)
				assert.Equal(t, "abc123hash", keySV.Value)

				// Build a response item via marshal round-trip.
				item := otpItem{
					PhoneHash:     "abc123hash",
					OTPMAC:        "mac-value",
					OTPCiphertext: "encrypted-otp",
					CreatedAt:     "2026-02-10T12:00:00Z",
					ExpiresAt:     "2026-02-10T12:05:00Z",
					AttemptCount:  2,
					Status:        "pending",
					TTL:           fixedTime().Add(1 * time.Hour).Unix(),
				}
				av, err := dynamo.MarshalMap(item)
				require.NoError(t, err)
				return &dynamo.GetItemOutput{Item: av}, nil
			},
			wantRec: &app.OTPRecord{
				PhoneHash:     "abc123hash",
				OTPMAC:        "mac-value",
				OTPCiphertext: "encrypted-otp",
				CreatedAt:     "2026-02-10T12:00:00Z",
				ExpiresAt:     "2026-02-10T12:05:00Z",
				AttemptCount:  2,
				Status:        "pending",
				TTL:           fixedTime().Add(1 * time.Hour).Unix(),
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
			errSubstr: "otp store: get otp: throttled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := domaintest.NewFakeClock(fixedTime())
			store := NewOTPStore(&stubOTPDynamo{
				getItemFn: tt.getItemFn,
			}, testTable, clock)

			rec, err := store.GetOTP(context.Background(), "abc123hash")

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
			assert.Equal(t, tt.wantRec.PhoneHash, rec.PhoneHash)
			assert.Equal(t, tt.wantRec.OTPMAC, rec.OTPMAC)
			assert.Equal(t, tt.wantRec.OTPCiphertext, rec.OTPCiphertext)
			assert.Equal(t, tt.wantRec.CreatedAt, rec.CreatedAt)
			assert.Equal(t, tt.wantRec.ExpiresAt, rec.ExpiresAt)
			assert.Equal(t, tt.wantRec.AttemptCount, rec.AttemptCount)
			assert.Equal(t, tt.wantRec.Status, rec.Status)
			assert.Equal(t, tt.wantRec.TTL, rec.TTL)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests — IncrementAttempts
// ---------------------------------------------------------------------------

func TestIncrementAttempts(t *testing.T) {
	tests := []struct {
		name         string
		updateItemFn func(ctx context.Context, params *dynamo.UpdateItemInput, optFns ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error)
		wantErr      bool
		errSubstr    string
	}{
		{
			name: "success - sends correct update expression",
			updateItemFn: func(_ context.Context, params *dynamo.UpdateItemInput, _ ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error) {
				// Verify correct table.
				assert.Equal(t, testTable, *params.TableName)

				// Verify the partition key.
				keySV, ok := params.Key["phone_hash"].(*dynamo.AttributeValueMemberS)
				require.True(t, ok)
				assert.Equal(t, "abc123hash", keySV.Value)

				// Verify the update expression increments attempt_count.
				require.NotNil(t, params.UpdateExpression)
				assert.Contains(t, *params.UpdateExpression, "attempt_count = attempt_count + :one")

				// Verify the :one expression attribute value.
				oneVal, ok := params.ExpressionAttributeValues[":one"].(*dynamo.AttributeValueMemberN)
				require.True(t, ok)
				assert.Equal(t, "1", oneVal.Value)

				return &dynamo.UpdateItemOutput{}, nil
			},
		},
		{
			name: "dynamo error - wraps with context",
			updateItemFn: func(_ context.Context, _ *dynamo.UpdateItemInput, _ ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error) {
				return nil, errors.New("internal server error")
			},
			wantErr:   true,
			errSubstr: "otp store: increment attempts: internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := domaintest.NewFakeClock(fixedTime())
			store := NewOTPStore(&stubOTPDynamo{
				updateItemFn: tt.updateItemFn,
			}, testTable, clock)

			err := store.IncrementAttempts(context.Background(), "abc123hash")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}

			require.NoError(t, err)
		})
	}
}
