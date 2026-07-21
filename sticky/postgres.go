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
//	    rule_id      TEXT NOT NULL,
//	    assignment   JSONB NOT NULL,
//	    assigned_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
//	    PRIMARY KEY (bucketing_id, flag_key, rule_id)
//	);
package sticky

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avsbhq/avsb-go/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresService is a PostgreSQL-backed StickyBucketService.
type PostgresService struct {
	pool *pgxpool.Pool
}

// NewPostgresService creates a PostgresService using the supplied connection pool.
func NewPostgresService(pool *pgxpool.Pool) *PostgresService {
	return &PostgresService{pool: pool}
}

// GetAssignment implements core.StickyBucketService.
func (s *PostgresService) GetAssignment(bucketingID, flagKey, ruleID string) *core.StickyAssignment {
	ctx := context.Background()
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT assignment FROM avsb_sticky_assignments WHERE bucketing_id=$1 AND flag_key=$2 AND rule_id=$3`,
		bucketingID, flagKey, ruleID,
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

// SaveAssignment implements core.StickyBucketService.
func (s *PostgresService) SaveAssignment(bucketingID, flagKey, ruleID string, assignment core.StickyAssignment) {
	ctx := context.Background()
	raw, err := json.Marshal(assignment)
	if err != nil {
		return
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO avsb_sticky_assignments (bucketing_id, flag_key, rule_id, assignment)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (bucketing_id, flag_key, rule_id)
		 DO UPDATE SET assignment = EXCLUDED.assignment, assigned_at = NOW()`,
		bucketingID, flagKey, ruleID, raw,
	)
	if err != nil {
		_ = fmt.Sprintf("avsb/sticky: postgres save failed: %v", err)
	}
}
