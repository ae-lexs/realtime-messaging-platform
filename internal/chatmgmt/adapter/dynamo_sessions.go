package adapter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/dynamo"
)

// Compile-time check: SessionStore satisfies app.SessionStore.
var _ app.SessionStore = (*SessionStore)(nil)

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

// toSessionItem converts an app.SessionRecord to the DynamoDB item shape.
func toSessionItem(r app.SessionRecord) sessionItem {
	return sessionItem{
		SessionID:        r.SessionID,
		UserID:           r.UserID,
		DeviceID:         r.DeviceID,
		RefreshTokenHash: r.RefreshTokenHash,
		TokenGeneration:  r.TokenGeneration,
		PrevTokenHash:    r.PrevTokenHash,
		CreatedAt:        r.CreatedAt,
		ExpiresAt:        r.ExpiresAt,
		TTL:              r.TTL,
	}
}

// fromSessionItem converts a DynamoDB item to an app.SessionRecord.
func fromSessionItem(item sessionItem) *app.SessionRecord {
	return &app.SessionRecord{
		SessionID:        item.SessionID,
		UserID:           item.UserID,
		DeviceID:         item.DeviceID,
		RefreshTokenHash: item.RefreshTokenHash,
		PrevTokenHash:    item.PrevTokenHash,
		CreatedAt:        item.CreatedAt,
		ExpiresAt:        item.ExpiresAt,
		TokenGeneration:  item.TokenGeneration,
		TTL:              item.TTL,
	}
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
func (s *SessionStore) Create(ctx context.Context, session app.SessionRecord) error {
	ctx, span := tracer.Start(ctx, "dynamo.sessions.create")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "PutItem"),
	)

	item := toSessionItem(session)

	av, err := dynamo.MarshalMap(item)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("session store: create: %w", err)
	}

	return nil
}

// GetByID retrieves a session record by session ID using a strongly consistent read.
// Returns domain.ErrNotFound when no session exists for the given ID.
func (s *SessionStore) GetByID(ctx context.Context, sessionID string) (*app.SessionRecord, error) {
	ctx, span := tracer.Start(ctx, "dynamo.sessions.get_by_id")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "GetItem"),
	)

	consistentRead := true

	out, err := s.db.GetItem(ctx, &dynamo.GetItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"session_id": &dynamo.AttributeValueMemberS{Value: sessionID},
		},
		ConsistentRead: &consistentRead,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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
func (s *SessionStore) ListByUser(ctx context.Context, userID string) ([]app.SessionRecord, error) {
	ctx, span := tracer.Start(ctx, "dynamo.sessions.list_by_user")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "Query"),
	)

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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("session store: list by user: %w", err)
	}

	sessions := make([]app.SessionRecord, 0, len(out.Items))
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
func (s *SessionStore) Update(ctx context.Context, sessionID string, updates app.SessionUpdate) error {
	ctx, span := tracer.Start(ctx, "dynamo.sessions.update")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "UpdateItem"),
	)

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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("session store: update: %w", err)
	}

	return nil
}

// Delete removes a session record by session ID.
func (s *SessionStore) Delete(ctx context.Context, sessionID string) error {
	ctx, span := tracer.Start(ctx, "dynamo.sessions.delete")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "dynamodb"),
		attribute.String("db.operation", "DeleteItem"),
	)

	_, err := s.db.DeleteItem(ctx, &dynamo.DeleteItemInput{
		TableName: &s.tableName,
		Key: map[string]dynamo.AttributeValue{
			"session_id": &dynamo.AttributeValueMemberS{Value: sessionID},
		},
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("session store: delete: %w", err)
	}

	return nil
}

// unmarshalSession converts a DynamoDB attribute map into an app.SessionRecord.
func (s *SessionStore) unmarshalSession(item map[string]dynamo.AttributeValue) (*app.SessionRecord, error) {
	var si sessionItem
	if err := dynamo.UnmarshalMap(item, &si); err != nil {
		return nil, fmt.Errorf("session store: unmarshal session: %w", err)
	}

	return fromSessionItem(si), nil
}
