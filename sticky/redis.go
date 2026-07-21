//go:build redis

// Package sticky: Redis-backed StickyBucketService.
//
// Build with: go build -tags redis ./...
//
// Requires the github.com/redis/go-redis/v9 module. Add it to your go.mod:
//
//	go get github.com/redis/go-redis/v9
package sticky

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/avsbhq/avsb-go/core"
	goredis "github.com/redis/go-redis/v9"
)

// RedisService is a Redis-backed StickyBucketService. Assignment records are
// stored as JSON hashes keyed by bucketingID. TTL defaults to 90 days.
type RedisService struct {
	client *goredis.Client
	ttl    time.Duration
	prefix string
}

// RedisOptions configures a RedisService.
type RedisOptions struct {
	// Client is the pre-configured redis client. Required.
	Client *goredis.Client
	// TTL controls how long assignments persist. Defaults to 90 days.
	TTL time.Duration
	// KeyPrefix is prepended to every Redis key. Defaults to "avsb:sticky:".
	KeyPrefix string
}

// NewRedisService creates a RedisService from the supplied options.
func NewRedisService(opts RedisOptions) *RedisService {
	ttl := opts.TTL
	if ttl == 0 {
		ttl = 90 * 24 * time.Hour
	}
	prefix := opts.KeyPrefix
	if prefix == "" {
		prefix = "avsb:sticky:"
	}
	return &RedisService{client: opts.Client, ttl: ttl, prefix: prefix}
}

func (s *RedisService) redisKey(bucketingID string) string {
	return s.prefix + bucketingID
}

func (s *RedisService) fieldKey(flagKey, ruleID string) string {
	return flagKey + ":" + ruleID
}

// GetAssignment implements core.StickyBucketService.
func (s *RedisService) GetAssignment(bucketingID, flagKey, ruleID string) *core.StickyAssignment {
	ctx := context.Background()
	val, err := s.client.HGet(ctx, s.redisKey(bucketingID), s.fieldKey(flagKey, ruleID)).Result()
	if err != nil {
		return nil
	}
	var a core.StickyAssignment
	if err := json.Unmarshal([]byte(val), &a); err != nil {
		return nil
	}
	return &a
}

// SaveAssignment implements core.StickyBucketService.
func (s *RedisService) SaveAssignment(bucketingID, flagKey, ruleID string, assignment core.StickyAssignment) {
	ctx := context.Background()
	raw, err := json.Marshal(assignment)
	if err != nil {
		return
	}
	key := s.redisKey(bucketingID)
	field := s.fieldKey(flagKey, ruleID)
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, field, raw)
	pipe.Expire(ctx, key, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		// Fire-and-forget — log but don't panic.
		_ = fmt.Sprintf("avsb/sticky: redis save failed: %v", err)
	}
}
