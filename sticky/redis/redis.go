// Package redis is the Redis-backed StickyBucketService for the A vs B Go
// SDK. It is its own module so the Redis client is a dependency only of
// the services that use it; the root SDK module stays dependency free.
//
//	go get github.com/avsbhq/avsb-go/sticky/redis
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/core"
	goredis "github.com/redis/go-redis/v9"
)

// The adapter must keep satisfying the SDK interface; a signature change in the
// root module breaks this line at compile time instead of at a customer's.
var _ avsb.StickyBucketService = (*RedisService)(nil)

// RedisService is a Redis-backed StickyBucketService. Assignment records are
// stored as JSON hashes keyed by bucketingID. TTL defaults to 90 days.
type RedisService struct {
	client *goredis.Client
	ttl    time.Duration
	prefix string
	logger *slog.Logger
}

// RedisOptions configures a RedisService.
type RedisOptions struct {
	// Client is the pre-configured redis client. Required.
	Client *goredis.Client
	// TTL controls how long assignments persist. Defaults to 90 days.
	TTL time.Duration
	// KeyPrefix is prepended to every Redis key. Defaults to "avsb:sticky:".
	KeyPrefix string
	// Logger receives write failures. Nil means they are dropped.
	Logger *slog.Logger
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
	return &RedisService{client: opts.Client, ttl: ttl, prefix: prefix, logger: opts.Logger}
}

func (s *RedisService) redisKey(bucketingID string) string {
	return s.prefix + bucketingID
}

// Lookup implements avsb.StickyBucketService.
//
// This is a BLOCKING read on the evaluation hot path. It is honest rather than
// clever: a Redis round trip is a millisecond on a local network, and a cache
// layer that returned a miss while it fetched in the background would hand the
// first request a fresh bucketing and the second one the stored value, which is
// the opposite of sticky. Front it with your own cache if the latency matters.
func (s *RedisService) Lookup(bucketingID, flagKey string) *core.StickyAssignment {
	ctx := context.Background()
	val, err := s.client.HGet(ctx, s.redisKey(bucketingID), flagKey).Result()
	if err != nil {
		return nil
	}
	var a core.StickyAssignment
	if err := json.Unmarshal([]byte(val), &a); err != nil {
		return nil
	}
	return &a
}

// Save implements avsb.StickyBucketService. Fire and forget: a failed write
// means the visitor gets re-bucketed next request, which is a worse experiment
// but not a broken response, so it must never surface as an error mid-evaluation.
func (s *RedisService) Save(bucketingID, flagKey string, assignment core.StickyAssignment) {
	ctx := context.Background()
	raw, err := json.Marshal(assignment)
	if err != nil {
		return
	}
	key := s.redisKey(bucketingID)
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, flagKey, raw)
	pipe.Expire(ctx, key, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		s.logf("avsb/sticky: redis save failed for %s/%s: %v", bucketingID, flagKey, err)
	}
}

// logf reports a write failure through the configured logger, or drops it when
// there is none. The previous version formatted the message into a discarded
// string, so a Redis outage was completely invisible.
func (s *RedisService) logf(format string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Warn(fmt.Sprintf(format, args...))
}
