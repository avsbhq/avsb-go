package sticky_test

import (
	"testing"
	"time"

	"github.com/avsbhq/avsb-go/core"
	"github.com/avsbhq/avsb-go/sticky"
)

func TestInMemoryService_SaveAndGet(t *testing.T) {
	svc := sticky.NewInMemoryService()

	assignment := core.StickyAssignment{
		VariationID: "var_treatment",
		RuleID:      "rule_ab_1",
		RuleType:    "ab_test",
		AssignedAt:  time.Now().UnixMilli(),
	}

	svc.SaveAssignment("user_1", "checkout-v2", "rule_ab_1", assignment)
	got := svc.GetAssignment("user_1", "checkout-v2", "rule_ab_1")

	if got == nil {
		t.Fatal("expected non-nil assignment after save")
	}
	if got.VariationID != "var_treatment" {
		t.Errorf("variationID: want var_treatment, got %q", got.VariationID)
	}
}

func TestInMemoryService_MissingKey_ReturnsNil(t *testing.T) {
	svc := sticky.NewInMemoryService()
	got := svc.GetAssignment("unknown_user", "some-flag", "some-rule")
	if got != nil {
		t.Errorf("expected nil for missing assignment, got %+v", got)
	}
}

func TestInMemoryService_OverwriteAssignment(t *testing.T) {
	svc := sticky.NewInMemoryService()

	first := core.StickyAssignment{VariationID: "var_control"}
	second := core.StickyAssignment{VariationID: "var_treatment"}

	svc.SaveAssignment("u1", "flag-x", "rule-1", first)
	svc.SaveAssignment("u1", "flag-x", "rule-1", second)

	got := svc.GetAssignment("u1", "flag-x", "rule-1")
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.VariationID != "var_treatment" {
		t.Errorf("expected overwrite: want var_treatment, got %q", got.VariationID)
	}
	if svc.Len() != 1 {
		t.Errorf("expected 1 stored assignment, got %d", svc.Len())
	}
}

func TestInMemoryService_DifferentKeys_StoredSeparately(t *testing.T) {
	svc := sticky.NewInMemoryService()

	a1 := core.StickyAssignment{VariationID: "var_a"}
	a2 := core.StickyAssignment{VariationID: "var_b"}

	svc.SaveAssignment("u1", "flag-a", "rule-1", a1)
	svc.SaveAssignment("u2", "flag-a", "rule-1", a2)

	got1 := svc.GetAssignment("u1", "flag-a", "rule-1")
	got2 := svc.GetAssignment("u2", "flag-a", "rule-1")

	if got1 == nil || got1.VariationID != "var_a" {
		t.Errorf("u1: want var_a, got %v", got1)
	}
	if got2 == nil || got2.VariationID != "var_b" {
		t.Errorf("u2: want var_b, got %v", got2)
	}
}
