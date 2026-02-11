package adapter

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// txDynamoDB is a narrow, consumer-defined interface for DynamoDB transaction
// operations. The *dynamodb.Client satisfies this interface.
type txDynamoDB interface {
	TransactWriteItems(ctx context.Context, params *dynamo.TransactWriteItemsInput, optFns ...func(*dynamo.Options)) (*dynamo.TransactWriteItemsOutput, error)
}

// Transactor orchestrates multi-table DynamoDB transactions for auth flows.
// Each method maps to a specific ADR-015 transaction:
//   - VerifyOTPAndCreateUser: §5.1 — new-user registration
//   - VerifyOTPAndCreateSession: §5.2 — existing-user login
type Transactor struct {
	db txDynamoDB
}

// NewTransactor creates a Transactor backed by the given DynamoDB client.
func NewTransactor(db txDynamoDB) *Transactor {
	return &Transactor{db: db}
}

// VerifyOTPAndCreateUser executes a 4-item TransactWriteItems for new-user
// registration (ADR-015 §5.1). The four items are:
//
//	[0] otpUpdate — marks the OTP as verified in otp_requests
//	[1] userPut — creates the new user in users table
//	[2] phoneSentinelPut — ensures phone uniqueness (condition check)
//	[3] sessionPut — creates the initial session in sessions table
//
// Returns domain.ErrAlreadyExists if the user or phone sentinel already exists
// (cancellation reason "ConditionalCheckFailed" at index 1 or 2).
func (t *Transactor) VerifyOTPAndCreateUser(
	ctx context.Context,
	otpUpdate dynamo.TransactWriteItem,
	userPut dynamo.TransactWriteItem,
	phoneSentinelPut dynamo.TransactWriteItem,
	sessionPut dynamo.TransactWriteItem,
) error {
	ctx, span := tracer.Start(ctx, "dynamo.tx.verify_otp_create_user")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "TransactWriteItems"),
	)

	_, err := t.db.TransactWriteItems(ctx, &dynamo.TransactWriteItemsInput{
		TransactItems: []dynamo.TransactWriteItem{
			otpUpdate,
			userPut,
			phoneSentinelPut,
			sessionPut,
		},
	})
	if err != nil {
		txErr := t.classifyTxError(err, "verify otp and create user",
			"otp_update", "user_put", "phone_sentinel", "session_put")
		span.RecordError(txErr)
		span.SetStatus(codes.Error, txErr.Error())
		return txErr
	}

	return nil
}

// VerifyOTPAndCreateSession executes a 2-item TransactWriteItems for
// existing-user login (ADR-015 §5.2). The two items are:
//
//	[0] otpUpdate — marks the OTP as verified in otp_requests
//	[1] sessionPut — creates a new session in sessions table
//
// Returns domain.ErrAlreadyExists if the session already exists.
func (t *Transactor) VerifyOTPAndCreateSession(
	ctx context.Context,
	otpUpdate dynamo.TransactWriteItem,
	sessionPut dynamo.TransactWriteItem,
) error {
	ctx, span := tracer.Start(ctx, "dynamo.tx.verify_otp_create_session")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "TransactWriteItems"),
	)

	_, err := t.db.TransactWriteItems(ctx, &dynamo.TransactWriteItemsInput{
		TransactItems: []dynamo.TransactWriteItem{
			otpUpdate,
			sessionPut,
		},
	})
	if err != nil {
		txErr := t.classifyTxError(err, "verify otp and create session",
			"otp_update", "session_put")
		span.RecordError(txErr)
		span.SetStatus(codes.Error, txErr.Error())
		return txErr
	}

	return nil
}

// classifyTxError inspects a TransactWriteItems error and wraps it with
// context. For TransactionCanceledException it checks each cancellation
// reason and maps ConditionalCheckFailed to domain.ErrAlreadyExists.
func (t *Transactor) classifyTxError(err error, op string, itemNames ...string) error {
	reasons, ok := dynamo.IsTransactionCanceledException(err)
	if !ok {
		return fmt.Errorf("transactor: %s: %w", op, err)
	}

	for i, reason := range reasons {
		if reason == "ConditionalCheckFailed" {
			name := "unknown"
			if i < len(itemNames) {
				name = itemNames[i]
			}
			return fmt.Errorf("transactor: %s: item %d (%s) condition failed: %w",
				op, i, name, domain.ErrAlreadyExists)
		}
	}

	return fmt.Errorf("transactor: %s: transaction canceled: %w", op, err)
}
