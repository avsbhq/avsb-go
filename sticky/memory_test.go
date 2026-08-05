package sticky_test

import (
	"sync"
	"testing"
	"time"

	"github.com/avsbhq/avsb-go/core"
	"github.com/avsbhq/avsb-go/sticky"
)

func TestInMemoryService_SaveAndLookup(t *testing.T) {
	svc := sticky.NewInMemoryService()

	assignment := core.StickyAssignment{
		VariationID: "var_treatment",
		RuleID:      "rule_ab_1",
		RuleType:    "ab_test",
		AssignedAt:  time.Now().UnixMilli(),
	}

	svc.Save("user_1", "checkout-v2", assignment)
	got := svc.Lookup("user_1", "checkout-v2")

	if got == nil {
		t.Fatal("expected the stored assignment back after a save")
	}
	if got.VariationID != "var_treatment" {
		t.Errorf("variationID: want var_treatment, got %q", got.VariationID)
	}
	if got.RuleID != "rule_ab_1" || got.RuleType != "ab_test" {
		t.Errorf("the rule the assignment came from must survive: got %q/%q", got.RuleID, got.RuleType)
	}
}

func TestInMemoryService_MissReturnsNil(t *testing.T) {
	svc := sticky.NewInMemoryService()
	if got := svc.Lookup("unknown_user", "some-flag"); got != nil {
		t.Errorf("a miss is nil, not a zero assignment; got %+v", got)
	}
}

func TestInMemoryService_OverwriteAssignment(t *testing.T) {
	svc := sticky.NewInMemoryService()

	svc.Save("u1", "flag-x", core.StickyAssignment{VariationID: "var_control"})
	svc.Save("u1", "flag-x", core.StickyAssignment{VariationID: "var_treatment"})

	got := svc.Lookup("u1", "flag-x")
	if got == nil {
		t.Fatal("expected an assignment")
	}
	if got.VariationID != "var_treatment" {
		t.Errorf("the newer save wins: want var_treatment, got %q", got.VariationID)
	}
	if svc.Len() != 1 {
		t.Errorf("one identity and one flag is one record, got %d", svc.Len())
	}
}

// TestInMemoryService_KeyedByIdentityAndFlag covers the composite key. The rule
// id is deliberately NOT part of it: a lookup happens before the evaluator has
// picked a rule, so keying on it would miss every time.
func TestInMemoryService_KeyedByIdentityAndFlag(t *testing.T) {
	svc := sticky.NewInMemoryService()

	svc.Save("u1", "flag-a", core.StickyAssignment{VariationID: "var_a"})
	svc.Save("u2", "flag-a", core.StickyAssignment{VariationID: "var_b"})
	svc.Save("u1", "flag-b", core.StickyAssignment{VariationID: "var_c"})

	cases := []struct {
		bucketingID string
		flagKey     string
		want        string
	}{
		{bucketingID: "u1", flagKey: "flag-a", want: "var_a"},
		{bucketingID: "u2", flagKey: "flag-a", want: "var_b"},
		{bucketingID: "u1", flagKey: "flag-b", want: "var_c"},
	}
	for _, tc := range cases {
		got := svc.Lookup(tc.bucketingID, tc.flagKey)
		if got == nil || got.VariationID != tc.want {
			t.Errorf("%s/%s: want %q, got %+v", tc.bucketingID, tc.flagKey, tc.want, got)
		}
	}
	if svc.Len() != 3 {
		t.Errorf("want three distinct records, got %d", svc.Len())
	}
}

// TestInMemoryService_LookupReturnsACopy stops a caller mutating the store
// through the pointer it was handed.
func TestInMemoryService_LookupReturnsACopy(t *testing.T) {
	svc := sticky.NewInMemoryService()
	svc.Save("u1", "flag-a", core.StickyAssignment{VariationID: "var_a"})

	first := svc.Lookup("u1", "flag-a")
	first.VariationID = "mutated"

	second := svc.Lookup("u1", "flag-a")
	if second.VariationID != "var_a" {
		t.Errorf("the store must be immutable through a returned pointer, got %q", second.VariationID)
	}
}

// TestInMemoryService_IsSafeForConcurrentUse covers the promise a shared Server
// makes: evaluation runs on every request goroutine, and each one may look up
// and save.
func TestInMemoryService_IsSafeForConcurrentUse(t *testing.T) {
	svc := sticky.NewInMemoryService()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%26))
			for j := 0; j < 50; j++ {
				svc.Save(id, "flag-a", core.StickyAssignment{VariationID: "var_a"})
				_ = svc.Lookup(id, "flag-a")
			}
		}(i)
	}
	wg.Wait()
}
