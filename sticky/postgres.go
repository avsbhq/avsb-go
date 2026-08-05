//go:build postgres

// Package sticky: PostgreSQL-backed StickyBucketService.
//
// Build with: go build -tags postgres ./...
//
// Requires pgx v5:
//
//	go get github.com/jackc/pgx/v5
//
// The implementation expects a table with the following schema:
//
//	CREATE TABLE IF NOT EXISTS avsb_sticky_assignments (
//	    bucketing_id TEXT NOT NULL,
//	    flag_key     TEXT NOT NULL,
//	    assignment   JSONB NOT NULL,
//	    assigned_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
//	    PRIMARY KEY (bucketing_id, flag_key)
//	);
package sticky

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/avsbhq/avsb-go/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresService is a PostgreSQL-backed StickyBucketService.
type PostgresService struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewPostgresService creates a PostgresService on the supplied pool. Pass a
// logger to see write failures; nil drops them.
func NewPostgresService(pool *pgxpool.Pool, logger *slog.Logger) *PostgresService {
	return &PostgresService{pool: pool, logger: logger}
}

// Lookup implements avsb.StickyBucketService. Blocking, for the reason given on
// the Redis service.
func (s *PostgresService) Lookup(bucketingID, flagKey string) *core.StickyAssignment {
	ctx := context.Background()
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT assignment FROM avsb_sticky_assignments WHERE bucketing_id=$1 AND flag_key=$2`,
		bucketingID, flagKey,
	).Scan(&raw)
	if err != nil {
		return nil
	}
	var a core.StickyAssignment
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil
	}
	return &a
}

// Save implements avsb.StickyBucketService. Fire and forget.
func (s *PostgresService) Save(bucketingID, flagKey string, assignment core.StickyAssignment) {
	ctx := context.Background()
	raw, err := json.Marshal(assignment)
	if err != nil {
		return
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO avsb_sticky_assignments (bucketing_id, flag_key, assignment)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (bucketing_id, flag_key)
		 DO UPDATE SET assignment = EXCLUDED.assignment, assigned_at = NOW()`,
		bucketingID, flagKey, raw,
	)
	if err != nil {
		s.logf("avsb/sticky: postgres save failed for %s/%s: %v", bucketingID, flagKey, err)
	}
}

// logf reports a write failure through the configured logger, or drops it when
// there is none.
func (s *PostgresService) logf(format string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Warn(fmt.Sprintf(format, args...))
}
