// Package dynamodb is the DynamoDB-backed StickyBucketService for the A vs B Go
// SDK. It is its own module so the DynamoDB client is a dependency only of
// the services that use it; the root SDK module stays dependency free.
//
//	go get github.com/avsbhq/avsb-go/sticky/dynamodb
package dynamodb

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/core"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// The adapter must keep satisfying the SDK interface; a signature change in the
// root module breaks this line at compile time instead of at a customer's.
var _ avsb.StickyBucketService = (*DynamoDBService)(nil)

// itemAPI is the slice of the DynamoDB client this service calls. The service
// holds it rather than the concrete client so the behaviour tests can stand a
// recording fake in for DynamoDB; NewDynamoDBService still takes the real
// client, so nothing changes for a caller.
type itemAPI interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

// The real client must keep satisfying the slice above; an SDK signature
// change breaks this line at compile time.
var _ itemAPI = (*dynamodb.Client)(nil)

// DynamoDBService is a DynamoDB-backed StickyBucketService. Each assignment is
// stored as an item in a single table with composite key (pk=bucketingID,
// sk=flagKey).
type DynamoDBService struct {
	client    itemAPI
	tableName string
	logger    *slog.Logger
}

// NewDynamoDBService creates a DynamoDBService. Pass a logger to see write
// failures; nil drops them.
func NewDynamoDBService(client *dynamodb.Client, tableName string, logger *slog.Logger) *DynamoDBService {
	return &DynamoDBService{client: client, tableName: tableName, logger: logger}
}

// Lookup implements avsb.StickyBucketService. Blocking, for the reason given on
// the Redis service: a background-refresh cache would serve the first request a
// miss and the second the stored value, which is not sticky.
func (s *DynamoDBService) Lookup(bucketingID, flagKey string) *core.StickyAssignment {
	ctx := context.Background()
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: bucketingID},
			"sk": &types.AttributeValueMemberS{Value: flagKey},
		},
	})
	if err != nil || result.Item == nil {
		return nil
	}
	raw, ok := result.Item["assignment"]
	if !ok {
		return nil
	}
	av, ok := raw.(*types.AttributeValueMemberS)
	if !ok {
		return nil
	}
	var a core.StickyAssignment
	if err := json.Unmarshal([]byte(av.Value), &a); err != nil {
		return nil
	}
	return &a
}

// Save implements avsb.StickyBucketService. Fire and forget.
func (s *DynamoDBService) Save(bucketingID, flagKey string, assignment core.StickyAssignment) {
	ctx := context.Background()
	raw, err := json.Marshal(assignment)
	if err != nil {
		return
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item: map[string]types.AttributeValue{
			"pk":         &types.AttributeValueMemberS{Value: bucketingID},
			"sk":         &types.AttributeValueMemberS{Value: flagKey},
			"assignment": &types.AttributeValueMemberS{Value: string(raw)},
		},
	})
	if err != nil {
		s.logf("avsb/sticky: dynamodb save failed for %s/%s: %v", bucketingID, flagKey, err)
	}
}

// logf reports a write failure through the configured logger, or drops it when
// there is none.
func (s *DynamoDBService) logf(format string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Warn(fmt.Sprintf(format, args...))
}
