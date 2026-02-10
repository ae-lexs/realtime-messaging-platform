package adapter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// otpDynamoDB is a narrow, consumer-defined interface for DynamoDB operations
// required by the OTP store. Only the methods this adapter calls are declared.
// The *dynamodb.Client satisfies this interface (optFns is variadic so callers
// may omit it), and test stubs implement it directly.
type otpDynamoDB interface {
	GetItem(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamo.UpdateItemInput, optFns ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error)
}

// otpItem is the DynamoDB item shape for the otp_requests table.
// Struct tags drive attributevalue.MarshalMap / UnmarshalMap serialization.
type otpItem struct {
	PhoneHash     string `dynamodbav:"phone_hash"`
	OTPMAC        string `dynamodbav:"otp_mac"`
	OTPCiphertext string `dynamodbav:"otp_ciphertext"`
	CreatedAt     string `dynamodbav:"created_at"`
	ExpiresAt     string `dynamodbav:"expires_at"`
	AttemptCount  int    `dynamodbav:"attempt_count"`
	Status        string `dynamodbav:"status"`
	TTL           int64  `dynamodbav:"ttl"`
}

// OTPRecord is the adapter-level representation of an OTP request.
// The app layer creates and consumes these; the adapter translates them
// to/from DynamoDB items.
type OTPRecord struct {
	PhoneHash     string
	OTPMAC        string
	OTPCiphertext string
	CreatedAt     string
	ExpiresAt     string
	AttemptCount  int
	Status        string
	TTL           int64
}

// OTPStore persists OTP records in DynamoDB.
// It implements the OTP storage interface defined in the app layer.
type OTPStore struct {
	db        otpDynamoDB
	tableName string
	clock     domain.Clock
}

// NewOTPStore creates an OTPStore backed by the given DynamoDB client.
func NewOTPStore(db otpDynamoDB, tableName string, clock domain.Clock) *OTPStore {
	return &OTPStore{
		db:        db,
		tableName: tableName,
		clock:     clock,
	}
}

// CreateOTP writes an OTP record to DynamoDB with a condition that prevents
// overwriting an active, unverified OTP. The condition allows writes when:
//   - The phone_hash key does not exist (new request).
//   - The existing record's status is "verified" (already consumed).
//   - The existing record has expired (expires_at < now).
//
// On ConditionalCheckFailed the caller receives domain.ErrAlreadyExists,
// signalling that an active OTP already exists for this phone hash.
func (s *OTPStore) CreateOTP(ctx context.Context, record OTPRecord) error {
	item := otpItem(record)

	av, err := dynamo.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("otp store: marshal item: %w", err)
	}

	now := s.clock.Now().UTC().Format(time.RFC3339)

	condExpr := "attribute_not_exists(phone_hash) OR #st = :verified OR #ea < :now"

	_, err = s.db.PutItem(ctx, &dynamo.PutItemInput{
		TableName:           &s.tableName,
		Item:                av,
		ConditionExpression: &condExpr,
		ExpressionAttributeNames: map[string]string{
			"#st": "status",
			"#ea": "expires_at",
		},
		ExpressionAttributeValues: map[string]dynamo.AttributeValue{
			":verified": &dynamo.AttributeValueMemberS{Value: "verified"},
			":now":      &dynamo.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		if dynamo.IsConditionalCheckFailed(err) {
			return fmt.Errorf("otp store: create otp: %w", domain.ErrAlreadyExists)
		}
		return fmt.Errorf("otp store: create otp: %w", err)
	}

	return nil
}

// GetOTP retrieves an OTP record by phone hash using a strongly consistent read.
// Returns domain.ErrNotFound when no record exists for the given phone hash.
func (s *OTPStore) GetOTP(ctx context.Context, phoneHash string) (*OTPRecord, error) {
	consistentRead := true

	out, err := s.db.GetItem(ctx, &dynamo.GetItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"phone_hash": &dynamo.AttributeValueMemberS{Value: phoneHash},
		},
		ConsistentRead: &consistentRead,
	})
	if err != nil {
		return nil, fmt.Errorf("otp store: get otp: %w", err)
	}

	if out.Item == nil {
		return nil, fmt.Errorf("otp store: get otp: %w", domain.ErrNotFound)
	}

	var item otpItem
	if err := dynamo.UnmarshalMap(out.Item, &item); err != nil {
		return nil, fmt.Errorf("otp store: unmarshal otp: %w", err)
	}

	return &OTPRecord{
		PhoneHash:     item.PhoneHash,
		OTPMAC:        item.OTPMAC,
		OTPCiphertext: item.OTPCiphertext,
		CreatedAt:     item.CreatedAt,
		ExpiresAt:     item.ExpiresAt,
		AttemptCount:  item.AttemptCount,
		Status:        item.Status,
		TTL:           item.TTL,
	}, nil
}

// IncrementAttempts atomically increments the attempt_count attribute for
// the OTP record identified by phoneHash.
func (s *OTPStore) IncrementAttempts(ctx context.Context, phoneHash string) error {
	updateExpr := "SET attempt_count = attempt_count + :one"

	_, err := s.db.UpdateItem(ctx, &dynamo.UpdateItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"phone_hash": &dynamo.AttributeValueMemberS{Value: phoneHash},
		},
		UpdateExpression: &updateExpr,
		ExpressionAttributeValues: map[string]dynamo.AttributeValue{
			":one": &dynamo.AttributeValueMemberN{Value: strconv.Itoa(1)},
		},
	})
	if err != nil {
		return fmt.Errorf("otp store: increment attempts: %w", err)
	}

	return nil
}
