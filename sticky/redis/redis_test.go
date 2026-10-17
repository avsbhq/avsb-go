package redis_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/avsbhq/avsb-go/core"
	stickyredis "github.com/avsbhq/avsb-go/sticky/redis"
)

// The behaviour tests the compile-time interface check could not give: the
// adapter talks to a real Redis protocol server (miniredis, in process), so a
// wrong key, a missing TTL or a swallowed failure shows up here.

var treatment = core.StickyAssignment{
	VariationID: "var_treatment",
	RuleID:      "rule_ab_1",
	RuleType:    "ab_test",
	AssignedAt:  1760000000000,
}

func newService(t *testing.T, opts stickyredis.RedisOptions) (*stickyredis.RedisService, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	// No retries: a failure path should fail at once, not after back-off.
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	opts.Client = client
	return stickyredis.NewRedisService(opts), mr
}

func TestLookupMissReturnsNil(t *testing.T) {
	svc, _ := newService(t, stickyredis.RedisOptions{})
	if got := svc.Lookup("user_1", "checkout-v2"); got != nil {
		t.Fatalf("a miss is nil, not a zero assignment; got %+v", got)
	}
}

func TestSaveThenLookupRoundTripsUnderTheDefaultKeyAndTTL(t *testing.T) {
	svc, mr := newService(t, stickyredis.RedisOptions{})

	svc.Save("user_1", "checkout-v2", treatment)

	got := svc.Lookup("user_1", "checkout-v2")
	if got == nil {
		t.Fatal("expected the stored assignment back after a save")
	}
	if *got != treatment {
		t.Errorf("round trip changed the assignment: want %+v, got %+v", treatment, *got)
	}

	// One hash per identity, one field per flag, under the default prefix.
	var stored core.StickyAssignment
	if err := json.Unmarshal([]byte(mr.HGet("avsb:sticky:user_1", "checkout-v2")), &stored); err != nil {
		t.Fatalf("the hash field is not the assignment's JSON: %v", err)
	}
	if stored != treatment {
		t.Errorf("stored record: want %+v, got %+v", treatment, stored)
	}
	if ttl := mr.TTL("avsb:sticky:user_1"); ttl != 90*24*time.Hour {
		t.Errorf("default TTL: want 90 days, got %v", ttl)
	}

	// Another flag on the same identity is still a miss.
	if other := svc.Lookup("user_1", "pricing-page"); other != nil {
		t.Errorf("a different flag must not see this assignment; got %+v", other)
	}
}

func TestCustomPrefixAndTTLAreHonouredAndExpiryIsAMiss(t *testing.T) {
	svc, mr := newService(t, stickyredis.RedisOptions{KeyPrefix: "tenant-a:", TTL: time.Hour})

	svc.Save("u1", "flag-x", treatment)

	if !mr.Exists("tenant-a:u1") {
		t.Fatalf("expected the key under the custom prefix; keys are %v", mr.Keys())
	}
	if ttl := mr.TTL("tenant-a:u1"); ttl != time.Hour {
		t.Errorf("custom TTL: want 1h, got %v", ttl)
	}

	mr.FastForward(time.Hour + time.Second)
	if got := svc.Lookup("u1", "flag-x"); got != nil {
		t.Errorf("an expired assignment is a miss; got %+v", got)
	}
}

func TestEverySaveRefreshesTheIdentityTTL(t *testing.T) {
	svc, mr := newService(t, stickyredis.RedisOptions{TTL: time.Hour})

	svc.Save("u1", "flag-x", treatment)
	mr.FastForward(40 * time.Minute)
	svc.Save("u1", "flag-y", treatment)

	if ttl := mr.TTL("avsb:sticky:u1"); ttl != time.Hour {
		t.Errorf("a save resets the hash TTL to the full value: want 1h, got %v", ttl)
	}
	mr.FastForward(40 * time.Minute)
	if got := svc.Lookup("u1", "flag-x"); got == nil {
		t.Error("the first flag lives as long as the hash, which the second save extended")
	}
}

func TestACorruptRecordIsAMiss(t *testing.T) {
	svc, mr := newService(t, stickyredis.RedisOptions{})
	mr.HSet("avsb:sticky:u1", "flag-x", "{not json")

	if got := svc.Lookup("u1", "flag-x"); got != nil {
		t.Errorf("an unreadable record must read as a miss, never a zero assignment; got %+v", got)
	}
}

func TestARedisFailureIsAMissOnReadAndALoggedWarningOnWrite(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	svc, mr := newService(t, stickyredis.RedisOptions{Logger: logger})
	mr.SetError("ERR the store is down")

	if got := svc.Lookup("u1", "flag-x"); got != nil {
		t.Errorf("a failed read is a miss; got %+v", got)
	}
	svc.Save("u1", "flag-x", treatment)

	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "avsb/sticky: redis save failed for u1/flag-x") {
		t.Errorf("a failed write must reach the logger as a warning; got %q", out)
	}
}

func TestAFailedWriteWithoutALoggerIsDroppedQuietly(t *testing.T) {
	svc, mr := newService(t, stickyredis.RedisOptions{})
	mr.SetError("ERR the store is down")

	// Must neither panic nor block: the evaluation that called Save carries on.
	svc.Save("u1", "flag-x", treatment)

	mr.SetError("")
	if got := svc.Lookup("u1", "flag-x"); got != nil {
		t.Errorf("the failed write stored nothing; got %+v", got)
	}
}
