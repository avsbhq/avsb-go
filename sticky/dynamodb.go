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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/avsbhq/avsb-go/core"
)

// DynamoDBService is a DynamoDB-backed StickyBucketService. Each assignment is
// stored as an item in a single table with composite key (pk=bucketingID,
// sk=flagKey:ruleID).
type DynamoDBService struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBService creates a DynamoDBService.
func NewDynamoDBService(client *dynamodb.Client, tableName string) *DynamoDBService {
	return &DynamoDBService{client: client, tableName: tableName}
}

// GetAssignment implements core.StickyBucketService.
func (s *DynamoDBService) GetAssignment(bucketingID, flagKey, ruleID string) *core.StickyAssignment {
	ctx := context.Background()
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: bucketingID},
			"sk": &types.AttributeValueMemberS{Value: flagKey + ":" + ruleID},
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

// SaveAssignment implements core.StickyBucketService.
func (s *DynamoDBService) SaveAssignment(bucketingID, flagKey, ruleID string, assignment core.StickyAssignment) {
	ctx := context.Background()
	raw, err := json.Marshal(assignment)
	if err != nil {
		return
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item: map[string]types.AttributeValue{
			"pk":         &types.AttributeValueMemberS{Value: bucketingID},
			"sk":         &types.AttributeValueMemberS{Value: flagKey + ":" + ruleID},
			"assignment": &types.AttributeValueMemberS{Value: string(raw)},
		},
	})
	if err != nil {
		_ = fmt.Sprintf("avsb/sticky: dynamodb save failed: %v", err)
	}
}
