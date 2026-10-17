// Package postgres is the PostgreSQL-backed StickyBucketService for the A vs B Go
// SDK. It is its own module so the PostgreSQL client is a dependency only of
// the services that use it; the root SDK module stays dependency free.
//
//	go get github.com/avsbhq/avsb-go/sticky/postgres
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
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The adapter must keep satisfying the SDK interface; a signature change in the
// root module breaks this line at compile time instead of at a customer's.
var _ avsb.StickyBucketService = (*PostgresService)(nil)

// querier is the slice of the pool this service calls. The service holds it
// rather than the concrete pool so the behaviour tests can stand a recording
// fake in for PostgreSQL; NewPostgresService still takes the real pool, so
// nothing changes for a caller.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// The real pool must keep satisfying the slice above; a pgx signature change
// breaks this line at compile time.
var _ querier = (*pgxpool.Pool)(nil)

// PostgresService is a PostgreSQL-backed StickyBucketService.
type PostgresService struct {
	pool   querier
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
