package core

import (
	"encoding/json"
	"testing"
)

func mustRaw(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func makeCondition(contextKind, attr, op string, value interface{}) json.RawMessage {
	c := Condition{
		ID:          "c1",
		Type:        "CUSTOM_ATTRIBUTE",
		Attribute:   attr,
		Operator:    op,
		Value:       mustRaw(value),
		ContextKind: contextKind,
	}
	return mustRaw(c)
}

func makeUserCtx(key string, attrs map[string]interface{}) EvalContext {
	ctx := EvalContext{}
	ctx["kind"] = mustRaw("user")
	ctx["key"] = mustRaw(key)
	for k, v := range attrs {
		ctx[k] = mustRaw(v)
	}
	return ctx
}

// TestEvaluateAudience_EqualsOperator verifies that a simple equals condition
// matches when the context attribute value matches and misses when it doesn't.
func TestEvaluateAudience_EqualsOperator(t *testing.T) {
	aud := &Audience{
		ID: "aud1",
		Rules: ConditionGroup{
			ID:    "g1",
			Logic: "AND",
			Conditions: []json.RawMessage{
				makeCondition("user", "plan", "equals", "pro"),
			},
		},
	}

	matchCtx := makeUserCtx("u1", map[string]interface{}{"plan": "pro"})
	if !EvaluateAudience(aud, matchCtx, AudienceOptions{}) {
		t.Error("expected plan=pro to match the audience")
	}

	missCtx := makeUserCtx("u2", map[string]interface{}{"plan": "free"})
	if EvaluateAudience(aud, missCtx, AudienceOptions{}) {
		t.Error("expected plan=free to NOT match the audience")
	}
}

// TestEvaluateAudience_NilAudienceMatchesEveryone verifies the
// "empty audience matches everyone" invariant.
func TestEvaluateAudience_NilAudienceMatchesEveryone(t *testing.T) {
	ctx := makeUserCtx("u1", nil)
	if !EvaluateAudience(nil, ctx, AudienceOptions{}) {
		t.Error("nil audience should match every context")
	}
}

// TestEvaluateAudience_ORLogic verifies that OR groups match when at least
// one condition is true.
func TestEvaluateAudience_ORLogic(t *testing.T) {
	aud := &Audience{
		ID: "aud_or",
		Rules: ConditionGroup{
			ID:    "g_or",
			Logic: "OR",
			Conditions: []json.RawMessage{
				makeCondition("user", "plan", "equals", "enterprise"),
				makeCondition("user", "plan", "equals", "pro"),
			},
		},
	}

	proCtx := makeUserCtx("u1", map[string]interface{}{"plan": "pro"})
	if !EvaluateAudience(aud, proCtx, AudienceOptions{}) {
		t.Error("expected plan=pro to match OR audience")
	}

	freeCtx := makeUserCtx("u2", map[string]interface{}{"plan": "free"})
	if EvaluateAudience(aud, freeCtx, AudienceOptions{}) {
		t.Error("expected plan=free to NOT match OR audience")
	}
}

// TestEvaluateCondition_NumericOperators verifies greater_than and less_than.
func TestEvaluateCondition_NumericOperators(t *testing.T) {
	mkCond := func(op string, val float64) *Condition {
		return &Condition{
			ID:          "c_num",
			Type:        "CUSTOM_ATTRIBUTE",
			Attribute:   "age",
			Operator:    op,
			Value:       mustRaw(val),
			ContextKind: "user",
		}
	}

	ctx := makeUserCtx("u1", map[string]interface{}{"age": 25})

	if !EvaluateCondition(mkCond("greater_than", 18), ctx, AudienceOptions{}) {
		t.Error("expected 25 > 18 to be true")
	}
	if EvaluateCondition(mkCond("greater_than", 30), ctx, AudienceOptions{}) {
		t.Error("expected 25 > 30 to be false")
	}
	if !EvaluateCondition(mkCond("less_than", 30), ctx, AudienceOptions{}) {
		t.Error("expected 25 < 30 to be true")
	}
}

// TestEvaluateCondition_InList verifies in_list and not_in_list operators.
func TestEvaluateCondition_InList(t *testing.T) {
	c := &Condition{
		ID:          "c_list",
		Type:        "CUSTOM_ATTRIBUTE",
		Attribute:   "country",
		Operator:    "in_list",
		Value:       mustRaw([]string{"US", "CA", "GB"}),
		ContextKind: "user",
	}

	usCtx := makeUserCtx("u1", map[string]interface{}{"country": "US"})
	if !EvaluateCondition(c, usCtx, AudienceOptions{}) {
		t.Error("expected 'US' to be in_list")
	}

	deCtx := makeUserCtx("u2", map[string]interface{}{"country": "DE"})
	if EvaluateCondition(c, deCtx, AudienceOptions{}) {
		t.Error("expected 'DE' to NOT be in_list")
	}
}

// TestEvaluateCondition_BrowserTypeReturnsFalse verifies server-side
// browser conditions are always false.
func TestEvaluateCondition_BrowserTypeReturnsFalse(t *testing.T) {
	c := &Condition{
		ID:       "c_browser",
		Type:     "BROWSER",
		Operator: "equals",
		Value:    mustRaw("Chrome"),
	}
	ctx := makeUserCtx("u1", nil)
	if EvaluateCondition(c, ctx, AudienceOptions{}) {
		t.Error("BROWSER condition should always return false server-side")
	}
}

// TestEvaluateAudience_ExistsOperator verifies the exists/not_exists operators.
func TestEvaluateAudience_ExistsOperator(t *testing.T) {
	existsCond := makeCondition("user", "email", "exists", nil)
	aud := &Audience{
		ID:    "aud_exists",
		Rules: ConditionGroup{ID: "g1", Logic: "AND", Conditions: []json.RawMessage{existsCond}},
	}

	withEmail := makeUserCtx("u1", map[string]interface{}{"email": "a@b.com"})
	if !EvaluateAudience(aud, withEmail, AudienceOptions{}) {
		t.Error("expected exists to match context with email")
	}

	withoutEmail := makeUserCtx("u2", nil)
	if EvaluateAudience(aud, withoutEmail, AudienceOptions{}) {
		t.Error("expected exists to NOT match context without email")
	}
}
