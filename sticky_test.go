package avsb

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// recordingSticky is a StickyBucketService that reports what the client layer
// asked it to do, so the save RULE can be asserted and not just the save.
type recordingSticky struct {
	mu      sync.Mutex
	stored  map[string]core.StickyAssignment
	lookups []string
	saves   []savedAssignment
}

type savedAssignment struct {
	bucketingID string
	flagKey     string
	assignment  core.StickyAssignment
}

func newRecordingSticky() *recordingSticky {
	return &recordingSticky{stored: map[string]core.StickyAssignment{}}
}

func (r *recordingSticky) Lookup(bucketingID, flagKey string) *core.StickyAssignment {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups = append(r.lookups, bucketingID+"/"+flagKey)
	a, ok := r.stored[bucketingID+"/"+flagKey]
	if !ok {
		return nil
	}
	cp := a
	return &cp
}

func (r *recordingSticky) Save(bucketingID, flagKey string, assignment core.StickyAssignment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stored[bucketingID+"/"+flagKey] = assignment
	r.saves = append(r.saves, savedAssignment{bucketingID, flagKey, assignment})
}

func (r *recordingSticky) savedRecords() []savedAssignment {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]savedAssignment(nil), r.saves...)
}

func (r *recordingSticky) put(bucketingID, flagKey string, assignment core.StickyAssignment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stored[bucketingID+"/"+flagKey] = assignment
}

// abTestDatafile is a 50/50 split plus a separate always-on rollout flag and a
// holdout, so one datafile covers every save and never-save case.
const abTestDatafile = `{
  "version": 2, "sdkKey": "sdk_production_abc123", "publishedAt": "x",
  "collectEndpoint": "https://ingest.avsb.cloud",
  "flags": [
    {
      "id": "flag_split", "key": "split", "type": "string", "enabled": true,
      "defaultVariationId": "var_a",
      "variations": [
        {"id": "var_a", "key": "a", "value": "A"},
        {"id": "var_b", "key": "b", "value": "B"}
      ],
      "overrides": [],
      "rules": [{
        "id": "rule_split", "type": "ab_test", "enabled": true, "audienceIds": [],
        "hashAttribute": "user.key", "trafficAllocation": 1,
        "variations": [
          {"variationId": "var_a", "percentage": 0.5},
          {"variationId": "var_b", "percentage": 0.5}
        ]
      }]
    },
    {
      "id": "flag_rollout", "key": "rollout", "type": "string", "enabled": true,
      "defaultVariationId": "var_off",
      "variations": [
        {"id": "var_off", "key": "off", "value": "OFF"},
        {"id": "var_on", "key": "on", "value": "ON"}
      ],
      "overrides": [],
      "rules": [{
        "id": "rule_rollout", "type": "targeted_delivery", "enabled": true, "audienceIds": [],
        "hashAttribute": "user.key", "trafficAllocation": null,
        "variations": [{"variationId": "var_on", "percentage": 1}]
      }]
    },
    {
      "id": "flag_held", "key": "held", "type": "string", "enabled": true,
      "defaultVariationId": "var_off",
      "variations": [
        {"id": "var_off", "key": "off", "value": "OFF"},
        {"id": "var_on", "key": "on", "value": "ON"}
      ],
      "overrides": [],
      "rules": [{
        "id": "rule_held", "type": "ab_test", "enabled": true, "audienceIds": [],
        "hashAttribute": "user.key", "trafficAllocation": 1,
        "variations": [{"variationId": "var_on", "percentage": 1}]
      }]
    },
    {
      "id": "flag_empty", "key": "empty", "type": "string", "enabled": true,
      "defaultVariationId": "var_only",
      "variations": [{"id": "var_only", "key": "only", "value": "ONLY"}],
      "overrides": [], "rules": []
    }
  ],
  "audiences": [],
  "holdouts": [{
    "id": "holdout_all", "key": "held-out-cohort", "trafficAllocation": 1,
    "flagIds": ["flag_held"], "hashAttribute": "user.key",
    "defaultVariationByFlag": {"flag_held": "var_off"}
  }]
}`

func newStickyServer(t *testing.T, service StickyBucketService) *Server {
	t.Helper()
	server, err := NewServer(ServerOptions{
		Bootstrap:           []byte(abTestDatafile),
		PollingDisabled:     true,
		IncludeReasons:      true,
		StickyBucketService: service,
		Events:              EventOptions{Disabled: true},
		Logger:              silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(server.Close)
	return server
}

// TestStickySavesOnABTestOnly is the save rule from the semantics contract. A
// rollout is not an assignment, a holdout is not the visitor's to keep, and a
// default was never decided at all: saving any of them pins a visitor to
// something that was never theirs.
func TestStickySavesOnABTestOnly(t *testing.T) {
	cases := []struct {
		name      string
		flagKey   string
		wantSaved bool
		wantWhy   string
	}{
		{
			name: "an ab_test decision is saved", flagKey: "split", wantSaved: true,
			wantWhy: "an ab_test allocation is the assignment sticky exists to preserve",
		},
		{
			name: "a targeted_delivery rollout is not saved", flagKey: "rollout", wantSaved: false,
			wantWhy: "a rollout is a percentage of traffic, not a per-visitor assignment",
		},
		{
			name: "a holdout decision is not saved", flagKey: "held", wantSaved: false,
			wantWhy: "the holdout decides afresh every time; pinning it would outlive the holdout",
		},
		{
			name: "a default is not saved", flagKey: "empty", wantSaved: false,
			wantWhy: "nothing decided, so there is nothing to remember",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := newRecordingSticky()
			server := newStickyServer(t, service)

			_ = server.GetStringFlag(context.Background(), tc.flagKey, "fallback",
				WithContext(UserContext("u_sticky", nil)))

			saves := service.savedRecords()
			if tc.wantSaved && len(saves) != 1 {
				t.Fatalf("want exactly one save (%s), got %d", tc.wantWhy, len(saves))
			}
			if !tc.wantSaved && len(saves) != 0 {
				t.Fatalf("want no save (%s), got %d: %+v", tc.wantWhy, len(saves), saves)
			}
			if !tc.wantSaved {
				return
			}

			saved := saves[0]
			if saved.bucketingID != "u_sticky" {
				t.Errorf("bucketingID: want u_sticky, got %q", saved.bucketingID)
			}
			if saved.flagKey != "split" {
				t.Errorf("flagKey: want split, got %q", saved.flagKey)
			}
			// The variation ID, never the key. Lookup resolves by id, so a
			// stored key writes a record that can never match again.
			if saved.assignment.VariationID != "var_a" && saved.assignment.VariationID != "var_b" {
				t.Errorf("variationId: want a variation ID (var_a or var_b), got %q. "+
					"Storing the variation KEY corrupts the store", saved.assignment.VariationID)
			}
			if saved.assignment.RuleID != "rule_split" {
				t.Errorf("ruleId: want rule_split, got %q", saved.assignment.RuleID)
			}
			if saved.assignment.RuleType != "ab_test" {
				t.Errorf("ruleType: want ab_test, got %q", saved.assignment.RuleType)
			}
			if saved.assignment.AssignedAt <= 0 {
				t.Errorf("assignedAt: want epoch milliseconds, got %d", saved.assignment.AssignedAt)
			}
		})
	}
}

// TestStickyHitBeatsTheEvaluator covers the pipeline order. A stored assignment
// wins over what the datafile now says, because moving a visitor mid-experiment
// destroys the measurement, and that is what sticky is for.
func TestStickyHitBeatsTheEvaluator(t *testing.T) {
	service := newRecordingSticky()
	server := newStickyServer(t, service)
	ctx := context.Background()

	// What the evaluator would decide on its own.
	fresh := server.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext("u_pinned", nil)))
	if fresh.Source() != "rule" {
		t.Fatalf("want a rule decision first, got %q", fresh.Source())
	}

	// Pin the OTHER variation.
	other := "var_a"
	if fresh.VariationKey() == "a" {
		other = "var_b"
	}
	service.put("u_pinned", "split", core.StickyAssignment{
		VariationID: other,
		RuleID:      "rule_split",
		RuleType:    "ab_test",
		AssignedAt:  time.Now().UnixMilli(),
	})

	pinned := server.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext("u_pinned", nil)))
	if pinned.Source() != "sticky" {
		t.Fatalf("want source sticky, got %q (reasons %v)", pinned.Source(), pinned.Reasons())
	}
	if pinned.VariationKey() == fresh.VariationKey() {
		t.Errorf("the stored variation must win; still serving %q", pinned.VariationKey())
	}
	if pinned.RuleID() != "rule_split" || pinned.RuleType() != "ab_test" {
		t.Errorf("ruleId and ruleType come from the stored assignment, got %q/%q",
			pinned.RuleID(), pinned.RuleType())
	}
	if !pinned.IsEnabled() {
		t.Error("a sticky decision is a real decision, so a truthy value is enabled")
	}
}

// TestStickyMissFallsThroughUnchanged covers the normal case: a miss is not an
// error and the evaluator answers as if sticky were not configured.
func TestStickyMissFallsThroughUnchanged(t *testing.T) {
	withSticky := newStickyServer(t, newRecordingSticky())
	withoutSticky := newStickyServer(t, nil)
	ctx := context.Background()

	a := withSticky.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext("u_miss", nil)))
	b := withoutSticky.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext("u_miss", nil)))

	if a.Source() != b.Source() || a.VariationKey() != b.VariationKey() {
		t.Errorf("a sticky miss must not change the answer: got %s/%s with sticky, %s/%s without",
			a.Source(), a.VariationKey(), b.Source(), b.VariationKey())
	}
}

// TestStickyUnknownVariationIsAMiss covers the deleted-variation case. The stored
// variation no longer exists, so serving it is impossible; treating it as a miss
// lets the evaluator decide afresh instead of returning a broken value.
func TestStickyUnknownVariationIsAMiss(t *testing.T) {
	service := newRecordingSticky()
	server := newStickyServer(t, service)
	service.put("u_ghost", "split", core.StickyAssignment{
		VariationID: "var_deleted_last_month",
		RuleID:      "rule_split",
		RuleType:    "ab_test",
		AssignedAt:  time.Now().UnixMilli(),
	})

	flag := server.GetStringFlag(context.Background(), "split", "fallback",
		WithContext(UserContext("u_ghost", nil)))

	if flag.Source() != "rule" {
		t.Errorf("want the evaluator's own answer, got source %q", flag.Source())
	}
	if flag.VariationKey() != "a" && flag.VariationKey() != "b" {
		t.Errorf("want a live variation, got %q", flag.VariationKey())
	}
}

// TestStickyIsSkippedWithoutAnIdentity covers the guard against a shared record:
// with no bucketing id every anonymous request would read and write the same
// sticky row.
func TestStickyIsSkippedWithoutAnIdentity(t *testing.T) {
	service := newRecordingSticky()
	server := newStickyServer(t, service)

	_ = server.GetStringFlag(context.Background(), "split", "fallback")

	if len(service.savedRecords()) != 0 {
		t.Error("an evaluation with no identity must not write a sticky record")
	}
	service.mu.Lock()
	lookups := len(service.lookups)
	service.mu.Unlock()
	if lookups != 0 {
		t.Errorf("an evaluation with no identity must not look one up, got %d lookups", lookups)
	}
}

// TestStickyOrphanServesTheValueAndSuppressesTheExposure covers the case the
// semantics contract singles out. The stored variation still exists but its rule
// no longer matches the datafile, so the value is served (the visitor keeps their
// variation) and NO exposure is recorded, because the rule it would be attributed
// to is gone and a wrong attribution corrupts the results.
func TestStickyOrphanServesTheValueAndSuppressesTheExposure(t *testing.T) {
	rec := &captured{}
	ingest := httptest.NewServer(rec.handler())
	defer ingest.Close()

	service := newRecordingSticky()
	server, err := NewServer(ServerOptions{
		Bootstrap:           []byte(abTestDatafile),
		CollectEndpoint:     ingest.URL,
		PollingDisabled:     true,
		IncludeReasons:      true,
		StickyBucketService: service,
		Events:              EventOptions{FlushInterval: time.Hour, BatchSize: 100},
		Logger:              silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	// A rule id the datafile does not carry: the rule was edited or deleted
	// after this visitor was bucketed.
	service.put("u_orphan", "split", core.StickyAssignment{
		VariationID: "var_b",
		RuleID:      "rule_that_was_deleted",
		RuleType:    "ab_test",
		AssignedAt:  time.Now().UnixMilli(),
	})

	ctx := context.Background()
	flag := server.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext("u_orphan", nil)))
	server.Flush(ctx)

	if flag.Source() != "sticky" {
		t.Errorf("the visitor keeps their variation, so source is sticky; got %q", flag.Source())
	}
	if flag.Value() != "B" {
		t.Errorf("want the stored variation's value B, got %q", flag.Value())
	}
	if _, _, requests := rec.snapshot(); requests != 0 {
		t.Errorf("an orphaned sticky assignment records no exposure, got %d requests", requests)
	}
}

// TestStickyHitRecordsAnExposureWhenTheRuleStillMatches is the other half: a
// healthy sticky hit is still an impression and has to be counted, or a
// long-running experiment stops accruing data for everyone already bucketed.
func TestStickyHitRecordsAnExposureWhenTheRuleStillMatches(t *testing.T) {
	rec := &captured{}
	ingest := httptest.NewServer(rec.handler())
	defer ingest.Close()

	service := newRecordingSticky()
	server, err := NewServer(ServerOptions{
		Bootstrap:           []byte(abTestDatafile),
		CollectEndpoint:     ingest.URL,
		PollingDisabled:     true,
		IncludeReasons:      true,
		StickyBucketService: service,
		Events:              EventOptions{FlushInterval: time.Hour, BatchSize: 100},
		Logger:              silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	service.put("u_healthy", "split", core.StickyAssignment{
		VariationID: "var_b",
		RuleID:      "rule_split",
		RuleType:    "ab_test",
		AssignedAt:  time.Now().UnixMilli(),
	})

	ctx := context.Background()
	_ = server.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext("u_healthy", nil)))
	server.Flush(ctx)

	_, bodies, requests := rec.snapshot()
	if requests != 1 {
		t.Fatalf("want one exposure batch, got %d", requests)
	}
	event := bodies[0]["events"].([]any)[0].(map[string]any)
	if event["visitorId"] != "u_healthy" {
		t.Errorf("visitorId: got %v", event["visitorId"])
	}
	if event["experimentId"] != "rule_split" {
		t.Errorf("experimentId: want the still-matching rule, got %v", event["experimentId"])
	}
	if event["variationId"] != "var_b" {
		t.Errorf("variationId: want the stored variation ID, got %v", event["variationId"])
	}
}

// TestStickyKeepsAVisitorStableAcrossAWeightChange is the customer-visible
// promise, end to end: bucket a visitor, then move the traffic weights, and the
// visitor stays where they were.
func TestStickyKeepsAVisitorStableAcrossAWeightChange(t *testing.T) {
	service := newRecordingSticky()
	server := newStickyServer(t, service)
	ctx := context.Background()

	// Find a visitor the 50/50 split puts in "a", so flipping the weights to
	// 100% "b" would otherwise move them.
	visitor := ""
	var firstKey string
	for i := 0; i < 200; i++ {
		candidate := fmt.Sprintf("u_stable_%d", i)
		flag := server.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext(candidate, nil)))
		if flag.VariationKey() == "a" {
			visitor = candidate
			firstKey = flag.VariationKey()
			break
		}
	}
	if visitor == "" {
		t.Fatal("could not find a visitor bucketed into variation a")
	}

	// Republish with everyone in "b".
	flipped := `{
	  "version": 2, "sdkKey": "sdk_production_abc123", "publishedAt": "y",
	  "collectEndpoint": "https://ingest.avsb.cloud",
	  "flags": [{
	    "id": "flag_split", "key": "split", "type": "string", "enabled": true,
	    "defaultVariationId": "var_a",
	    "variations": [
	      {"id": "var_a", "key": "a", "value": "A"},
	      {"id": "var_b", "key": "b", "value": "B"}
	    ],
	    "overrides": [],
	    "rules": [{
	      "id": "rule_split", "type": "ab_test", "enabled": true, "audienceIds": [],
	      "hashAttribute": "user.key", "trafficAllocation": 1,
	      "variations": [{"variationId": "var_b", "percentage": 1}]
	    }]
	  }],
	  "audiences": []
	}`
	if err := server.loadBytes([]byte(flipped)); err != nil {
		t.Fatalf("loadBytes: %v", err)
	}

	after := server.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext(visitor, nil)))
	if after.Source() != "sticky" {
		t.Fatalf("want source sticky after the weight change, got %q (reasons %v)",
			after.Source(), after.Reasons())
	}
	if after.VariationKey() != firstKey {
		t.Errorf("the visitor must stay in %q, got %q", firstKey, after.VariationKey())
	}
}
