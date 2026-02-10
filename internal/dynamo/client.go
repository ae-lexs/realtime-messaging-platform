// Package dynamo provides a shared DynamoDB client factory.
// Only this package may import the DynamoDB SDK — adapters in other packages
// use the re-exported types and helpers defined here.
// See CONTRIBUTING.md: "Only internal/dynamo/ may import aws-sdk-go-v2/service/dynamodb".
package dynamo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Config holds DynamoDB connection parameters.
type Config struct {
	// Endpoint overrides the default AWS endpoint.
	// Set to a LocalStack URL (e.g. "http://localhost:4566") for local development.
	// When empty, the default AWS endpoint resolver is used.
	Endpoint string

	// Region is the AWS region for the DynamoDB client (e.g. "us-east-2").
	Region string

	// Timeout is the HTTP client timeout for DynamoDB requests.
	Timeout time.Duration
}

// Client wraps the AWS DynamoDB SDK client.
// Adapters access the underlying SDK client via the DB field.
type Client struct {
	// DB is the underlying AWS DynamoDB SDK client.
	DB *dynamodb.Client
}

// NewClient creates a DynamoDB client configured from cfg.
// When cfg.Endpoint is non-empty, BaseEndpoint is set on the service client
// for LocalStack compatibility.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}

	if cfg.Endpoint != "" {
		opts = append(opts,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider("test", "test", ""),
			),
		)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	if cfg.Timeout > 0 {
		awsCfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}

	var dbOpts []func(*dynamodb.Options)
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		dbOpts = append(dbOpts, func(o *dynamodb.Options) {
			o.BaseEndpoint = &endpoint
		})
	}

	return &Client{
		DB: dynamodb.NewFromConfig(awsCfg, dbOpts...),
	}, nil
}

// ---------------------------------------------------------------------------
// Type aliases — adapters import dynamo.GetItemInput instead of the SDK.
// ---------------------------------------------------------------------------

// Core CRUD operation types.
type (
	GetItemInput     = dynamodb.GetItemInput
	GetItemOutput    = dynamodb.GetItemOutput
	PutItemInput     = dynamodb.PutItemInput
	PutItemOutput    = dynamodb.PutItemOutput
	QueryInput       = dynamodb.QueryInput
	QueryOutput      = dynamodb.QueryOutput
	UpdateItemInput  = dynamodb.UpdateItemInput
	UpdateItemOutput = dynamodb.UpdateItemOutput
	DeleteItemInput  = dynamodb.DeleteItemInput
	DeleteItemOutput = dynamodb.DeleteItemOutput
)

// Transaction types.
type (
	TransactWriteItemsInput  = dynamodb.TransactWriteItemsInput
	TransactWriteItemsOutput = dynamodb.TransactWriteItemsOutput
	TransactWriteItem        = types.TransactWriteItem
)

// Transaction operation types for building TransactWriteItem.
type (
	Put            = types.Put
	Update         = types.Update
	Delete         = types.Delete
	ConditionCheck = types.ConditionCheck
)

// Attribute value types.
type (
	AttributeValue           = types.AttributeValue
	AttributeValueMemberS    = types.AttributeValueMemberS
	AttributeValueMemberN    = types.AttributeValueMemberN
	AttributeValueMemberBOOL = types.AttributeValueMemberBOOL
)

// Expression builder types.
type (
	ConditionBuilder = expression.ConditionBuilder
	UpdateBuilder    = expression.UpdateBuilder
	NameBuilder      = expression.NameBuilder
	ValueBuilder     = expression.ValueBuilder
	KeyBuilder       = expression.KeyBuilder
)

// Options is the DynamoDB client options type.
// Re-exported so adapter-defined interfaces can reference optFns variadic params.
type Options = dynamodb.Options

// ---------------------------------------------------------------------------
// AWS helper re-exports — so adapters avoid importing the aws top-level package.
// ---------------------------------------------------------------------------

// Bool returns a pointer to a bool value.
var Bool = aws.Bool

// String returns a pointer to a string value.
var String = aws.String

// MarshalMap serializes a Go value into a DynamoDB attribute value map.
// Re-exported from the SDK attributevalue package so adapters do not need
// a direct SDK import.
var MarshalMap = attributevalue.MarshalMap

// UnmarshalMap deserializes a DynamoDB attribute value map into a Go value.
// Re-exported from the SDK attributevalue package so adapters do not need
// a direct SDK import.
var UnmarshalMap = attributevalue.UnmarshalMap

// ---------------------------------------------------------------------------
// Error classification helpers — adapters check error types without SDK import.
// ---------------------------------------------------------------------------

// IsConditionalCheckFailed reports whether err is a DynamoDB
// ConditionalCheckFailedException. Adapters use this to detect condition
// expression violations (e.g., item already exists).
func IsConditionalCheckFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	return errors.As(err, &ccf)
}

// ErrConditionalCheckFailed returns a ConditionalCheckFailedException suitable
// for testing. Production code should never construct this error — DynamoDB
// returns it. This helper exists so adapter tests can exercise the
// IsConditionalCheckFailed code path without importing the SDK types package.
func ErrConditionalCheckFailed() error {
	return &types.ConditionalCheckFailedException{
		Message: aws.String("The conditional request failed"),
	}
}

// ErrTransactionCanceled returns a TransactionCanceledException with the given
// cancellation reason codes, suitable for testing. Each code corresponds to a
// transaction item; empty string means that item succeeded. Production code
// should never construct this error — DynamoDB returns it.
func ErrTransactionCanceled(codes ...string) error {
	reasons := make([]types.CancellationReason, len(codes))
	for i, code := range codes {
		if code != "" {
			c := code
			reasons[i] = types.CancellationReason{Code: &c}
		}
	}
	msg := "Transaction cancelled"
	return &types.TransactionCanceledException{
		Message:             &msg,
		CancellationReasons: reasons,
	}
}

// IsTransactionCanceledException reports whether err is a DynamoDB
// TransactionCanceledException. When true, it returns the cancellation reason
// codes (one per transaction item, empty string if that item succeeded).
func IsTransactionCanceledException(err error) ([]string, bool) {
	var tce *types.TransactionCanceledException
	if !errors.As(err, &tce) {
		return nil, false
	}
	reasons := make([]string, len(tce.CancellationReasons))
	for i, r := range tce.CancellationReasons {
		if r.Code != nil {
			reasons[i] = *r.Code
		}
	}
	return reasons, true
}
