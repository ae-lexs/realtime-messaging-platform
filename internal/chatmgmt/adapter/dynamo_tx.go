package adapter

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// Compile-time check: Transactor satisfies app.AuthTransactor.
var _ app.AuthTransactor = (*Transactor)(nil)

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
	db            txDynamoDB
	otpTable      string
	usersTable    string
	sessionsTable string
}

// NewTransactor creates a Transactor backed by the given DynamoDB client.
func NewTransactor(db txDynamoDB, otpTable, usersTable, sessionsTable string) *Transactor {
	return &Transactor{
		db:            db,
		otpTable:      otpTable,
		usersTable:    usersTable,
		sessionsTable: sessionsTable,
	}
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
func (t *Transactor) VerifyOTPAndCreateUser(ctx context.Context, p app.RegistrationParams) error {
	ctx, span := tracer.Start(ctx, "dynamo.tx.verify_otp_create_user")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "TransactWriteItems"),
	)

	otpUpdate := t.buildOTPVerifyUpdate(p.PhoneHash, p.OTPExpiresAt, p.OTPMAC)
	userPut := t.buildUserPut(p.UserID, p.PhoneNumber, p.Now)
	phoneSentinelPut := t.buildPhoneSentinelPut(p.PhoneNumber, p.UserID)
	sessionPut := t.buildSessionPut(p.SessionID, p.UserID, p.DeviceID, p.RefreshTokenHash, p.Now, p.SessionExpiresAt, p.SessionTTL)

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
func (t *Transactor) VerifyOTPAndCreateSession(ctx context.Context, p app.LoginParams) error {
	ctx, span := tracer.Start(ctx, "dynamo.tx.verify_otp_create_session")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "TransactWriteItems"),
	)

	otpUpdate := t.buildOTPVerifyUpdate(p.PhoneHash, p.OTPExpiresAt, p.OTPMAC)
	sessionPut := t.buildSessionPut(p.SessionID, p.UserID, p.DeviceID, p.RefreshTokenHash, p.CreatedAt, p.SessionExpiresAt, p.SessionTTL)

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

// buildOTPVerifyUpdate creates a TransactWriteItem that marks an OTP as verified.
func (t *Transactor) buildOTPVerifyUpdate(phoneHash, expiresAt, otpMAC string) dynamo.TransactWriteItem {
	updateExpr := "SET #st = :verified"
	condExpr := "#st = :pending AND expires_at = :ea AND otp_mac = :mac"
	return dynamo.TransactWriteItem{
		Update: &dynamo.Update{
			TableName: &t.otpTable,
			Key: map[string]dynamo.AttributeValue{
				"phone_hash": &dynamo.AttributeValueMemberS{Value: phoneHash},
			},
			UpdateExpression:    &updateExpr,
			ConditionExpression: &condExpr,
			ExpressionAttributeNames: map[string]string{
				"#st": "status",
			},
			ExpressionAttributeValues: map[string]dynamo.AttributeValue{
				":verified": &dynamo.AttributeValueMemberS{Value: "verified"},
				":pending":  &dynamo.AttributeValueMemberS{Value: "pending"},
				":ea":       &dynamo.AttributeValueMemberS{Value: expiresAt},
				":mac":      &dynamo.AttributeValueMemberS{Value: otpMAC},
			},
		},
	}
}

// buildUserPut creates a TransactWriteItem that inserts a new user.
func (t *Transactor) buildUserPut(userID, phoneNumber, now string) dynamo.TransactWriteItem {
	condExpr := "attribute_not_exists(user_id)"
	item, _ := dynamo.MarshalMap(userItem{
		UserID:      userID,
		PhoneNumber: phoneNumber,
		DisplayName: "",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	return dynamo.TransactWriteItem{
		Put: &dynamo.Put{
			TableName:           &t.usersTable,
			Item:                item,
			ConditionExpression: &condExpr,
		},
	}
}

// buildPhoneSentinelPut creates a TransactWriteItem that enforces phone uniqueness.
func (t *Transactor) buildPhoneSentinelPut(phoneNumber, userID string) dynamo.TransactWriteItem {
	condExpr := "attribute_not_exists(user_id)"
	item := map[string]dynamo.AttributeValue{
		"user_id":      &dynamo.AttributeValueMemberS{Value: "phone#" + phoneNumber},
		"phone_number": &dynamo.AttributeValueMemberS{Value: phoneNumber},
		"owner_id":     &dynamo.AttributeValueMemberS{Value: userID},
	}
	return dynamo.TransactWriteItem{
		Put: &dynamo.Put{
			TableName:           &t.usersTable,
			Item:                item,
			ConditionExpression: &condExpr,
		},
	}
}

// buildSessionPut creates a TransactWriteItem that inserts a new session.
func (t *Transactor) buildSessionPut(sessionID, userID, deviceID, refreshTokenHash, createdAt, expiresAt string, ttl int64) dynamo.TransactWriteItem {
	condExpr := "attribute_not_exists(session_id)"
	item, _ := dynamo.MarshalMap(sessionItem{
		SessionID:        sessionID,
		UserID:           userID,
		DeviceID:         deviceID,
		RefreshTokenHash: refreshTokenHash,
		TokenGeneration:  1,
		PrevTokenHash:    "",
		CreatedAt:        createdAt,
		ExpiresAt:        expiresAt,
		TTL:              ttl,
	})
	return dynamo.TransactWriteItem{
		Put: &dynamo.Put{
			TableName:           &t.sessionsTable,
			Item:                item,
			ConditionExpression: &condExpr,
		},
	}
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
