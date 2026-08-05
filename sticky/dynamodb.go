//go:build dynamodb

// Package sticky: DynamoDB-backed StickyBucketService.
//
// Build with: go build -tags dynamodb ./...
//
// Requires the AWS SDK v2 module:
//
//	go get github.com/aws/aws-sdk-go-v2/service/dynamodb
package sticky

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/avsbhq/avsb-go/core"
)

// DynamoDBService is a DynamoDB-backed StickyBucketService. Each assignment is
// stored as an item in a single table with composite key (pk=bucketingID,
// sk=flagKey).
type DynamoDBService struct {
	client    *dynamodb.Client
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
