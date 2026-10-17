package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/avsbhq/avsb-go/core"
)

// Behaviour tests against a recording fake of the two pool calls the service
// makes. The fake keeps one row per (bucketing_id, flag_key), the table's
// primary key, answers a missing row with pgx.ErrNoRows the way pgx does, and
// fails on demand. It only stands in for the database: the SQL itself is
// pinned by asserting on the statement text and the argument order.

var treatment = core.StickyAssignment{
	VariationID: "var_treatment",
	RuleID:      "rule_ab_1",
	RuleType:    "ab_test",
	AssignedAt:  1760000000000,
}

type call struct {
	sql  string
	args []any
}

type fakeRow struct {
	raw []byte
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return fmt.Errorf("fake: want one destination, got %d", len(dest))
	}
	target, ok := dest[0].(*[]byte)
	if !ok {
		return fmt.Errorf("fake: want a *[]byte destination for a JSONB column, got %T", dest[0])
	}
	*target = append([]byte(nil), r.raw...)
	return nil
}

type fakePool struct {
	rows     map[[2]string][]byte
	queries  []call
	execs    []call
	queryErr error
	execErr  error
}

func newFake() *fakePool {
	return &fakePool{rows: map[[2]string][]byte{}}
}

func keyArgs(args []any) [2]string {
	if len(args) < 2 {
		return [2]string{}
	}
	id, _ := args[0].(string)
	flag, _ := args[1].(string)
	return [2]string{id, flag}
}

func (f *fakePool) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.queries = append(f.queries, call{sql, args})
	if f.queryErr != nil {
		return fakeRow{err: f.queryErr}
	}
	raw, ok := f.rows[keyArgs(args)]
	if !ok {
		return fakeRow{err: pgx.ErrNoRows}
	}
	return fakeRow{raw: raw}
}

func (f *fakePool) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execs = append(f.execs, call{sql, args})
	if f.execErr != nil {
		return pgconn.CommandTag{}, f.execErr
	}
	if len(args) != 3 {
		return pgconn.CommandTag{}, fmt.Errorf("fake: want three arguments, got %d", len(args))
	}
	raw, ok := args[2].([]byte)
	if !ok {
		return pgconn.CommandTag{}, fmt.Errorf("fake: want the assignment as []byte, got %T", args[2])
	}
	f.rows[keyArgs(args)] = raw
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

var spaces = regexp.MustCompile(`\s+`)

// oneLine collapses the statement's whitespace so the assertions read the SQL,
// not its indentation.
func oneLine(sql string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(sql, " "))
}

func TestLookupMissReturnsNilAndQueriesByBothKeyColumnsInOrder(t *testing.T) {
	fake := newFake()
	svc := &PostgresService{pool: fake}

	if got := svc.Lookup("user_1", "checkout-v2"); got != nil {
		t.Fatalf("a missing row is nil, not a zero assignment; got %+v", got)
	}
	if len(fake.queries) != 1 {
		t.Fatalf("want one query, got %d", len(fake.queries))
	}
	q := fake.queries[0]
	want := "SELECT assignment FROM avsb_sticky_assignments WHERE bucketing_id=$1 AND flag_key=$2"
	if oneLine(q.sql) != want {
		t.Errorf("query: want %q, got %q", want, oneLine(q.sql))
	}
	if len(q.args) != 2 || q.args[0] != "user_1" || q.args[1] != "checkout-v2" {
		t.Errorf("arguments: want [user_1 checkout-v2] in that order, got %v", q.args)
	}
}

func TestSaveThenLookupRoundTripsThroughAnUpsert(t *testing.T) {
	fake := newFake()
	svc := &PostgresService{pool: fake}

	svc.Save("user_1", "checkout-v2", treatment)

	if len(fake.execs) != 1 {
		t.Fatalf("want one statement, got %d", len(fake.execs))
	}
	stmt := oneLine(fake.execs[0].sql)
	for _, part := range []string{
		"INSERT INTO avsb_sticky_assignments (bucketing_id, flag_key, assignment) VALUES ($1, $2, $3)",
		"ON CONFLICT (bucketing_id, flag_key) DO UPDATE SET assignment = EXCLUDED.assignment, assigned_at = NOW()",
	} {
		if !strings.Contains(stmt, part) {
			t.Errorf("the save must be an upsert on the primary key, so a re-bucketing overwrites; missing %q in %q", part, stmt)
		}
	}
	args := fake.execs[0].args
	if len(args) != 3 || args[0] != "user_1" || args[1] != "checkout-v2" {
		t.Fatalf("arguments: want [user_1 checkout-v2 <json>], got %v", args)
	}
	var stored core.StickyAssignment
	if err := json.Unmarshal(args[2].([]byte), &stored); err != nil || stored != treatment {
		t.Errorf("the third argument is the assignment's JSON: want %+v, got %+v (%v)", treatment, stored, err)
	}

	got := svc.Lookup("user_1", "checkout-v2")
	if got == nil || *got != treatment {
		t.Fatalf("round trip: want %+v, got %+v", treatment, got)
	}
	if other := svc.Lookup("user_1", "pricing-page"); other != nil {
		t.Errorf("a different flag must not see this assignment; got %+v", other)
	}
}

func TestACorruptRowOrAFailedQueryIsAMiss(t *testing.T) {
	fake := newFake()
	fake.rows[[2]string{"u1", "flag-x"}] = []byte("{not json")
	svc := &PostgresService{pool: fake}

	if got := svc.Lookup("u1", "flag-x"); got != nil {
		t.Errorf("an unreadable row is a miss; got %+v", got)
	}

	fake.rows[[2]string{"u1", "flag-x"}] = []byte(`{"variationId":"var_treatment"}`)
	fake.queryErr = errors.New("connection refused")
	if got := svc.Lookup("u1", "flag-x"); got != nil {
		t.Errorf("a failed query is a miss; got %+v", got)
	}
}

func TestAFailedWriteIsALoggedWarningAndNeverPanics(t *testing.T) {
	var logs bytes.Buffer
	fake := newFake()
	fake.execErr = errors.New(`relation "avsb_sticky_assignments" does not exist`)

	(&PostgresService{pool: fake, logger: slog.New(slog.NewTextHandler(&logs, nil))}).Save("u1", "flag-x", treatment)

	out := logs.String()
	if !strings.Contains(out, "level=WARN") ||
		!strings.Contains(out, "avsb/sticky: postgres save failed for u1/flag-x") ||
		!strings.Contains(out, "does not exist") {
		t.Errorf("a failed write must reach the logger as a warning with the cause; got %q", out)
	}

	// Without a logger the failure is dropped quietly: no panic, no row.
	(&PostgresService{pool: fake}).Save("u1", "flag-x", treatment)
	if len(fake.rows) != 0 {
		t.Errorf("a failed write stores nothing; got %v", fake.rows)
	}
}
