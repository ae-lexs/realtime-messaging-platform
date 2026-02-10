package adapter

import (
	"context"
	"fmt"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// userDynamoDB is a narrow, consumer-defined interface for DynamoDB operations
// required by the user store. The *dynamodb.Client satisfies this interface.
type userDynamoDB interface {
	GetItem(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
	Query(ctx context.Context, params *dynamo.QueryInput, optFns ...func(*dynamo.Options)) (*dynamo.QueryOutput, error)
}

// userItem is the DynamoDB item shape for the users table.
type userItem struct {
	UserID      string `dynamodbav:"user_id"`
	PhoneNumber string `dynamodbav:"phone_number"`
	DisplayName string `dynamodbav:"display_name"`
	CreatedAt   string `dynamodbav:"created_at"`
	UpdatedAt   string `dynamodbav:"updated_at"`
}

// UserRecord is the adapter-level representation of a user.
type UserRecord struct {
	UserID      string
	PhoneNumber string
	DisplayName string
	CreatedAt   string
	UpdatedAt   string
}

// UserStore persists user records in DynamoDB.
type UserStore struct {
	db        userDynamoDB
	tableName string
	indexName string
}

// NewUserStore creates a UserStore backed by the given DynamoDB client.
func NewUserStore(db userDynamoDB, tableName string) *UserStore {
	return &UserStore{
		db:        db,
		tableName: tableName,
		indexName: "phone_number-index",
	}
}

// GetByID retrieves a user record by user ID using a strongly consistent read.
// Returns domain.ErrNotFound when no user exists for the given ID.
func (s *UserStore) GetByID(ctx context.Context, userID string) (*UserRecord, error) {
	consistentRead := true

	out, err := s.db.GetItem(ctx, &dynamo.GetItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"user_id": &dynamo.AttributeValueMemberS{Value: userID},
		},
		ConsistentRead: &consistentRead,
	})
	if err != nil {
		return nil, fmt.Errorf("user store: get by id: %w", err)
	}

	if out.Item == nil {
		return nil, fmt.Errorf("user store: get by id: %w", domain.ErrNotFound)
	}

	var item userItem
	if err := dynamo.UnmarshalMap(out.Item, &item); err != nil {
		return nil, fmt.Errorf("user store: unmarshal user: %w", err)
	}

	return &UserRecord{
		UserID:      item.UserID,
		PhoneNumber: item.PhoneNumber,
		DisplayName: item.DisplayName,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}, nil
}

// FindByPhone looks up a user by phone number via the phone_number-index GSI,
// then fetches the full record with a consistent GetItem read.
// Returns domain.ErrNotFound when no user exists for the given phone number.
//
// Per 04_CONTEXT_AND_LIFECYCLE: checks ctx.Err() between the Query and GetItem
// steps to honour cancellation between multi-step operations.
func (s *UserStore) FindByPhone(ctx context.Context, phoneNumber string) (*UserRecord, error) {
	keyExpr := "phone_number = :phone"

	queryOut, err := s.db.Query(ctx, &dynamo.QueryInput{
		TableName:              &s.tableName,
		IndexName:              &s.indexName,
		KeyConditionExpression: &keyExpr,
		ExpressionAttributeValues: map[string]dynamo.AttributeValue{
			":phone": &dynamo.AttributeValueMemberS{Value: phoneNumber},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("user store: find by phone query: %w", err)
	}

	if len(queryOut.Items) == 0 {
		return nil, fmt.Errorf("user store: find by phone: %w", domain.ErrNotFound)
	}

	// Extract user_id from the GSI projection.
	var projected struct {
		UserID string `dynamodbav:"user_id"`
	}
	if err := dynamo.UnmarshalMap(queryOut.Items[0], &projected); err != nil {
		return nil, fmt.Errorf("user store: unmarshal gsi projection: %w", err)
	}

	// Check context between multi-step operations per 04_CONTEXT.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("user store: find by phone: %w", err)
	}

	return s.GetByID(ctx, projected.UserID)
}
