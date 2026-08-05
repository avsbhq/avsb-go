package core

import "testing"

// TestAliasOperators locks the four dashboard alias spellings. Before they were
// handled, an audience saved in the dashboard with "is" fell through to the
// default branch and matched nobody, so a correctly built audience targeted zero
// traffic with nothing logged. Each case here mirrors a conformance fixture.
func TestAliasOperators(t *testing.T) {
	cases := []struct {
		name      string
		attribute string
		operator  string
		value     interface{}
		attrs     map[string]interface{}
		want      bool
	}{
		{
			name:      "is matches an equal value",
			attribute: "plan",
			operator:  "is",
			value:     "enterprise",
			attrs:     map[string]interface{}{"plan": "enterprise"},
			want:      true,
		},
		{
			name:      "is misses a different value",
			attribute: "plan",
			operator:  "is",
			value:     "enterprise",
			attrs:     map[string]interface{}{"plan": "starter"},
			want:      false,
		},
		{
			name:      "is_not matches a different value",
			attribute: "plan",
			operator:  "is_not",
			value:     "starter",
			attrs:     map[string]interface{}{"plan": "enterprise"},
			want:      true,
		},
		{
			name:      "is_not misses an equal value",
			attribute: "plan",
			operator:  "is_not",
			value:     "starter",
			attrs:     map[string]interface{}{"plan": "starter"},
			want:      false,
		},
		{
			name:      "does_not_contain matches when the substring is absent",
			attribute: "email",
			operator:  "does_not_contain",
			value:     "@acme.com",
			attrs:     map[string]interface{}{"email": "someone@other.com"},
			want:      true,
		},
		{
			name:      "does_not_contain is case insensitive, so a differently cased match misses",
			attribute: "email",
			operator:  "does_not_contain",
			value:     "@acme.com",
			attrs:     map[string]interface{}{"email": "someone@ACME.com"},
			want:      false,
		},
		{
			name:      "does_not_exist matches an absent attribute",
			attribute: "betaOptIn",
			operator:  "does_not_exist",
			value:     "",
			attrs:     nil,
			want:      true,
		},
		{
			name:      "does_not_exist misses a present attribute",
			attribute: "betaOptIn",
			operator:  "does_not_exist",
			value:     "",
			attrs:     map[string]interface{}{"betaOptIn": "yes"},
			want:      false,
		},
		{
			name:      "does_not_exist treats an explicit JSON null as absent",
			attribute: "betaOptIn",
			operator:  "does_not_exist",
			value:     "",
			attrs:     map[string]interface{}{"betaOptIn": nil},
			want:      true,
		},
		{
			name:      "does_not_exist misses an empty string, which is present",
			attribute: "betaOptIn",
			operator:  "does_not_exist",
			value:     "",
			attrs:     map[string]interface{}{"betaOptIn": ""},
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			condition := &Condition{
				ID:          "c_alias",
				Type:        "CUSTOM_ATTRIBUTE",
				Attribute:   tc.attribute,
				Operator:    tc.operator,
				Value:       mustRaw(tc.value),
				ContextKind: "user",
			}
			ctx := makeUserCtx("u_alias", tc.attrs)
			if got := EvaluateCondition(condition, ctx, AudienceOptions{}); got != tc.want {
				t.Errorf("%s %q: want %v, got %v", tc.operator, tc.attribute, tc.want, got)
			}
		})
	}
}

// TestAliasOperatorsMatchTheirCanonicalSpelling proves the aliases are not a
// second implementation that could drift: each pair answers identically on the
// same input.
func TestAliasOperatorsMatchTheirCanonicalSpelling(t *testing.T) {
	pairs := []struct {
		canonical string
		alias     string
	}{
		{canonical: "equals", alias: "is"},
		{canonical: "not_equals", alias: "is_not"},
		{canonical: "not_contains", alias: "does_not_contain"},
		{canonical: "not_exists", alias: "does_not_exist"},
	}

	contexts := []map[string]interface{}{
		{"plan": "enterprise"},
		{"plan": "starter"},
		{"plan": ""},
		nil,
	}

	for _, pair := range pairs {
		for i, attrs := range contexts {
			ctx := makeUserCtx("u_pair", attrs)
			build := func(operator string) *Condition {
				return &Condition{
					ID:          "c_pair",
					Type:        "CUSTOM_ATTRIBUTE",
					Attribute:   "plan",
					Operator:    operator,
					Value:       mustRaw("enterprise"),
					ContextKind: "user",
				}
			}
			canonical := EvaluateCondition(build(pair.canonical), ctx, AudienceOptions{})
			alias := EvaluateCondition(build(pair.alias), ctx, AudienceOptions{})
			if canonical != alias {
				t.Errorf("context %d: %q returned %v but its alias %q returned %v",
					i, pair.canonical, canonical, pair.alias, alias)
			}
		}
	}
}

// TestContextKindGate covers the difference between a missing KIND and a missing
// ATTRIBUTE, which the two interaction fixtures pin down: a condition scoped to
// a kind the context does not carry is a hard no-match, even for a negated
// operator, while the same condition against a kind that IS present but lacks
// the attribute matches.
func TestContextKindGate(t *testing.T) {
	condition := &Condition{
		ID:          "c_org_tier",
		Type:        "CUSTOM_ATTRIBUTE",
		Attribute:   "tier",
		Operator:    "does_not_exist",
		Value:       mustRaw(""),
		ContextKind: "organization",
	}

	withOrg := EvalContext{
		"kind":         mustRaw("multi"),
		"user":         mustRaw(map[string]interface{}{"kind": "user", "key": "user_with_org"}),
		"organization": mustRaw(map[string]interface{}{"kind": "organization", "key": "org_no_tier"}),
	}
	if !EvaluateCondition(condition, withOrg, AudienceOptions{}) {
		t.Error("a present organization kind with no tier attribute should satisfy does_not_exist")
	}

	withoutOrg := makeUserCtx("user_no_org", nil)
	if EvaluateCondition(condition, withoutOrg, AudienceOptions{}) {
		t.Error("an absent organization kind must be a hard no-match, not a satisfied does_not_exist")
	}
}
