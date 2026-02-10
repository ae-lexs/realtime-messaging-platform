package adapter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// sessionDynamoDB is a narrow, consumer-defined interface for DynamoDB operations
// required by the session store. The *dynamodb.Client satisfies this interface.
type sessionDynamoDB interface {
	GetItem(ctx context.Context, params *dynamo.GetItemInput, optFns ...func(*dynamo.Options)) (*dynamo.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamo.PutItemInput, optFns ...func(*dynamo.Options)) (*dynamo.PutItemOutput, error)
	Query(ctx context.Context, params *dynamo.QueryInput, optFns ...func(*dynamo.Options)) (*dynamo.QueryOutput, error)
	UpdateItem(ctx context.Context, params *dynamo.UpdateItemInput, optFns ...func(*dynamo.Options)) (*dynamo.UpdateItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamo.DeleteItemInput, optFns ...func(*dynamo.Options)) (*dynamo.DeleteItemOutput, error)
}

// sessionItem is the DynamoDB item shape for the sessions table.
type sessionItem struct {
	SessionID        string `dynamodbav:"session_id"`
	UserID           string `dynamodbav:"user_id"`
	DeviceID         string `dynamodbav:"device_id"`
	RefreshTokenHash string `dynamodbav:"refresh_token_hash"`
	TokenGeneration  int64  `dynamodbav:"token_generation"`
	PrevTokenHash    string `dynamodbav:"prev_token_hash"`
	CreatedAt        string `dynamodbav:"created_at"`
	ExpiresAt        string `dynamodbav:"expires_at"`
	TTL              int64  `dynamodbav:"ttl"`
}

// SessionRecord is the adapter-level representation of a session.
type SessionRecord struct {
	SessionID        string
	UserID           string
	DeviceID         string
	RefreshTokenHash string
	TokenGeneration  int64
	PrevTokenHash    string
	CreatedAt        string
	ExpiresAt        string
	TTL              int64
}

// SessionUpdate holds the fields to update during refresh token rotation.
type SessionUpdate struct {
	RefreshTokenHash string
	TokenGeneration  int64
	PrevTokenHash    string
	ExpiresAt        string
	TTL              int64
}

// SessionStore persists session records in DynamoDB.
type SessionStore struct {
	db        sessionDynamoDB
	tableName string
	indexName string
	clock     domain.Clock
}

// NewSessionStore creates a SessionStore backed by the given DynamoDB client.
func NewSessionStore(db sessionDynamoDB, tableName string, clock domain.Clock) *SessionStore {
	return &SessionStore{
		db:        db,
		tableName: tableName,
		indexName: "user_sessions-index",
		clock:     clock,
	}
}

// Create writes a new session record to DynamoDB.
// Returns domain.ErrAlreadyExists if a session with the same ID already exists.
func (s *SessionStore) Create(ctx context.Context, session SessionRecord) error {
	item := sessionItem(session)

	av, err := dynamo.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("session store: marshal session: %w", err)
	}

	condExpr := "attribute_not_exists(session_id)"

	_, err = s.db.PutItem(ctx, &dynamo.PutItemInput{
		TableName:           &s.tableName,
		Item:                av,
		ConditionExpression: &condExpr,
	})
	if err != nil {
		if dynamo.IsConditionalCheckFailed(err) {
			return fmt.Errorf("session store: create: %w", domain.ErrAlreadyExists)
		}
		return fmt.Errorf("session store: create: %w", err)
	}

	return nil
}

// GetByID retrieves a session record by session ID using a strongly consistent read.
// Returns domain.ErrNotFound when no session exists for the given ID.
func (s *SessionStore) GetByID(ctx context.Context, sessionID string) (*SessionRecord, error) {
	consistentRead := true

	out, err := s.db.GetItem(ctx, &dynamo.GetItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"session_id": &dynamo.AttributeValueMemberS{Value: sessionID},
		},
		ConsistentRead: &consistentRead,
	})
	if err != nil {
		return nil, fmt.Errorf("session store: get by id: %w", err)
	}

	if out.Item == nil {
		return nil, fmt.Errorf("session store: get by id: %w", domain.ErrNotFound)
	}

	return s.unmarshalSession(out.Item)
}

// ListByUser retrieves all active sessions for a user via the user_sessions-index GSI.
// Only sessions with expires_at > now are returned (application-level filter; DDB TTL
// is eventually consistent).
func (s *SessionStore) ListByUser(ctx context.Context, userID string) ([]SessionRecord, error) {
	keyExpr := "user_id = :uid"
	filterExpr := "expires_at > :now"
	now := s.clock.Now().UTC().Format(time.RFC3339)

	out, err := s.db.Query(ctx, &dynamo.QueryInput{
		TableName:              &s.tableName,
		IndexName:              &s.indexName,
		KeyConditionExpression: &keyExpr,
		FilterExpression:       &filterExpr,
		ExpressionAttributeValues: map[string]dynamo.AttributeValue{
			":uid": &dynamo.AttributeValueMemberS{Value: userID},
			":now": &dynamo.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("session store: list by user: %w", err)
	}

	sessions := make([]SessionRecord, 0, len(out.Items))
	for _, item := range out.Items {
		rec, err := s.unmarshalSession(item)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *rec)
	}

	return sessions, nil
}

// Update applies a SessionUpdate to the session identified by sessionID.
// Used for refresh token rotation: new hash, bumped generation, prev_token_hash.
func (s *SessionStore) Update(ctx context.Context, sessionID string, updates SessionUpdate) error {
	updateExpr := "SET refresh_token_hash = :rth, token_generation = :gen, prev_token_hash = :pth, expires_at = :ea, #ttl = :ttl"

	_, err := s.db.UpdateItem(ctx, &dynamo.UpdateItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"session_id": &dynamo.AttributeValueMemberS{Value: sessionID},
		},
		UpdateExpression: &updateExpr,
		ExpressionAttributeNames: map[string]string{
			"#ttl": "ttl",
		},
		ExpressionAttributeValues: map[string]dynamo.AttributeValue{
			":rth": &dynamo.AttributeValueMemberS{Value: updates.RefreshTokenHash},
			":gen": &dynamo.AttributeValueMemberN{Value: strconv.FormatInt(updates.TokenGeneration, 10)},
			":pth": &dynamo.AttributeValueMemberS{Value: updates.PrevTokenHash},
			":ea":  &dynamo.AttributeValueMemberS{Value: updates.ExpiresAt},
			":ttl": &dynamo.AttributeValueMemberN{Value: strconv.FormatInt(updates.TTL, 10)},
		},
	})
	if err != nil {
		return fmt.Errorf("session store: update: %w", err)
	}

	return nil
}

// Delete removes a session record by session ID.
func (s *SessionStore) Delete(ctx context.Context, sessionID string) error {
	_, err := s.db.DeleteItem(ctx, &dynamo.DeleteItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"session_id": &dynamo.AttributeValueMemberS{Value: sessionID},
		},
	})
	if err != nil {
		return fmt.Errorf("session store: delete: %w", err)
	}

	return nil
}

// unmarshalSession converts a DynamoDB attribute map into a SessionRecord.
func (s *SessionStore) unmarshalSession(item map[string]dynamo.AttributeValue) (*SessionRecord, error) {
	var si sessionItem
	if err := dynamo.UnmarshalMap(item, &si); err != nil {
		return nil, fmt.Errorf("session store: unmarshal session: %w", err)
	}

	return &SessionRecord{
		SessionID:        si.SessionID,
		UserID:           si.UserID,
		DeviceID:         si.DeviceID,
		RefreshTokenHash: si.RefreshTokenHash,
		TokenGeneration:  si.TokenGeneration,
		PrevTokenHash:    si.PrevTokenHash,
		CreatedAt:        si.CreatedAt,
		ExpiresAt:        si.ExpiresAt,
		TTL:              si.TTL,
	}, nil
}
