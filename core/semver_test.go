package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The rows below are the ones packages/avsb-core/src/semver.test.ts runs, so
// every port is held to the same grammar, not just the same corpus.

func TestParseSemverReadsTheGrammar(t *testing.T) {
	cases := []struct {
		input string
		want  parsedSemver
	}{
		{"1.2.3", parsedSemver{"1", "2", "3", nil}},
		{"0.0.0", parsedSemver{"0", "0", "0", nil}},
		{"v1.2.3", parsedSemver{"1", "2", "3", nil}},
		{"V1.2.3", parsedSemver{"1", "2", "3", nil}},
		{"2", parsedSemver{"2", "0", "0", nil}},
		{"2.3", parsedSemver{"2", "3", "0", nil}},
		{"v2", parsedSemver{"2", "0", "0", nil}},
		{"  2.3.0\t", parsedSemver{"2", "3", "0", nil}},
		{"\n\v\f1.0.0\r\n", parsedSemver{"1", "0", "0", nil}},
		{"1.0.0-alpha", parsedSemver{"1", "0", "0", []string{"alpha"}}},
		{"1.0.0-alpha.1", parsedSemver{"1", "0", "0", []string{"alpha", "1"}}},
		{"1.0.0-0.3.7", parsedSemver{"1", "0", "0", []string{"0", "3", "7"}}},
		{"1.0.0-x-y-z.--", parsedSemver{"1", "0", "0", []string{"x-y-z", "--"}}},
		{"1.0.0-0a", parsedSemver{"1", "0", "0", []string{"0a"}}},
		{"1.0.0+20130313144700", parsedSemver{"1", "0", "0", nil}},
		{"1.0.0-beta+exp.sha.5114f85", parsedSemver{"1", "0", "0", []string{"beta"}}},
		{"1.0.0+21AF26D3----117B344092BD", parsedSemver{"1", "0", "0", nil}},
		{"1.0.0+001", parsedSemver{"1", "0", "0", nil}},
		{"2-beta", parsedSemver{"2", "0", "0", []string{"beta"}}},
		{"1-2.3", parsedSemver{"1", "0", "0", []string{"2", "3"}}},
		{"18446744073709551616.0.0", parsedSemver{"18446744073709551616", "0", "0", nil}},
	}
	for _, tc := range cases {
		got, ok := parseSemver(tc.input)
		if !ok {
			t.Errorf("parseSemver(%q) refused a valid version", tc.input)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseSemver(%q) = %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestParseSemverRefusesEverythingElse(t *testing.T) {
	for _, input := range []string{
		"", "   ", "v", "V", "banana", "2.x", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03",
		"1.2.3-01", "1.2.3-", "1.2.3+", "1.2.3-alpha..1", "1.2.3+build..1", "1.", ".1",
		"1..2", "vv1.2.3", "v 1.2.3", "1.2.3 beta", "1.2.3-beta_1", "1.2.3-béta",
		"١.٢.٣", "1.2.3+build+more", "=1.2.3", "-1.2.3", "+1.2.3",
		"1.2.3 ", " 1.2.3", "1.2.3\n2.0.0", " 1 .2.3",
	} {
		if got, ok := parseSemver(input); ok {
			t.Errorf("parseSemver(%q) = %+v, want refused", input, got)
		}
	}
}

func mustParseSemver(t *testing.T, input string) parsedSemver {
	t.Helper()
	v, ok := parseSemver(input)
	if !ok {
		t.Fatalf("test row %q does not parse", input)
	}
	return v
}

func TestCompareSemverFollowsSemVerPrecedence(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "2.1.0", -1},
		{"2.1.0", "2.1.1", -1},
		{"2.10.0", "2.9.1", 1},
		{"1.0.0", "1.0.0", 0},
		{"2", "2.0.0", 0},
		{"v2.3", "2.3.0", 0},
		{"1.0.0+build1", "1.0.0+build2", 0},
		{"1.0.0-rc.1+build.5", "1.0.0-rc.1", 0},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0-Beta", "1.0.0-alpha", -1},
		{"1.0.0-1", "1.0.0-a", -1},
		{"1.0.0-2", "1.0.0-10", -1},
		{"1.0.0-a-b", "1.0.0-a", 1},
		{"18446744073709551616.0.0", "18446744073709551615.0.0", 1},
		{"99999999999999999999.0.0", "100000000000000000000.0.0", -1},
	}
	for _, tc := range cases {
		a, b := mustParseSemver(t, tc.a), mustParseSemver(t, tc.b)
		if got := compareSemver(a, b); got != tc.want {
			t.Errorf("compareSemver(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := compareSemver(b, a); got != -tc.want {
			t.Errorf("compareSemver(%s, %s) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

func TestCompareSemverOrdersThePrereleaseChain(t *testing.T) {
	chain := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta",
		"1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0",
	}
	for i := 0; i < len(chain)-1; i++ {
		lower, higher := mustParseSemver(t, chain[i]), mustParseSemver(t, chain[i+1])
		if compareSemver(lower, higher) != -1 || compareSemver(higher, lower) != 1 {
			t.Errorf("%s should order below %s", chain[i], chain[i+1])
		}
	}
}

var semverOperators = []string{"semver_eq", "semver_neq", "semver_gt", "semver_gte", "semver_lt", "semver_lte"}

func rawJSON(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %v: %v", value, err)
	}
	return raw
}

func TestEvalSemverOperatorAnswersEachOperator(t *testing.T) {
	cases := []struct {
		operator, actual, expected string
		want                       bool
	}{
		{"semver_eq", "2.3.0", "2.3", true},
		{"semver_eq", "2.3.1", "2.3", false},
		{"semver_neq", "2.3.1", "2.3.0", true},
		{"semver_neq", "v2.3.0", "2.3", false},
		{"semver_gt", "2.10.0", "2.9.1", true},
		{"semver_gt", "2.9.1", "2.9.1", false},
		{"semver_gte", "2.9.1", "2.9.1", true},
		{"semver_gte", "2.9.0", "2.9.1", false},
		{"semver_lt", "2.0.0-beta.2", "2.0.0", true},
		{"semver_lt", "2.0.0", "2.0.0", false},
		{"semver_lte", "2.0.0", "2.0.0", true},
		{"semver_lte", "2.0.1", "2.0.0", false},
	}
	for _, tc := range cases {
		got := evalSemverOperator(tc.operator, rawJSON(t, tc.actual), rawJSON(t, tc.expected))
		if got != tc.want {
			t.Errorf("%s %q against %q = %v, want %v", tc.operator, tc.actual, tc.expected, got, tc.want)
		}
	}
}

func TestEvalSemverOperatorReadsANumberLikeJavaScript(t *testing.T) {
	if !evalSemverOperator("semver_eq", json.RawMessage(`2`), rawJSON(t, "2.0.0")) {
		t.Error("the number 2 should be the version 2.0.0")
	}
	if !evalSemverOperator("semver_eq", json.RawMessage(`2.0`), rawJSON(t, "2.0.0")) {
		t.Error("the JSON number 2.0 prints as 2 in JavaScript, the version 2.0.0")
	}
	if !evalSemverOperator("semver_lt", json.RawMessage(`2.5`), rawJSON(t, "2.10.0")) {
		t.Error("the number 2.5 should be the version 2.5.0")
	}
	if !evalSemverOperator("semver_eq", json.RawMessage(`-0`), rawJSON(t, "0.0.0")) {
		t.Error("negative zero prints as 0 in JavaScript, the version 0.0.0")
	}
	if evalSemverOperator("semver_gte", json.RawMessage(`-2`), rawJSON(t, "0.0.0")) {
		t.Error("a negative number is not a version")
	}
	if evalSemverOperator("semver_gte", json.RawMessage(`1e21`), rawJSON(t, "0.0.0")) {
		t.Error("JavaScript prints 1e21 as 1e+21, which is not a version")
	}
}

func TestEvalSemverOperatorFailsClosed(t *testing.T) {
	for _, operator := range semverOperators {
		for _, actual := range []string{`true`, `false`, `null`, `{}`, `["2.0.0"]`} {
			if evalSemverOperator(operator, json.RawMessage(actual), rawJSON(t, "2.0.0")) {
				t.Errorf("%s on %s should be false", operator, actual)
			}
		}
		if evalSemverOperator(operator, nil, rawJSON(t, "2.0.0")) {
			t.Errorf("%s on an absent attribute should be false", operator)
		}
		if evalSemverOperator(operator, rawJSON(t, "3.0.0"), rawJSON(t, "2.x")) {
			t.Errorf("%s with an unparsable rule value should be false", operator)
		}
		if evalSemverOperator(operator, rawJSON(t, "1.2.3.4"), rawJSON(t, "2.0.0")) {
			t.Errorf("%s with an unparsable attribute should be false", operator)
		}
	}
	if evalSemverOperator("semver_eq", rawJSON(t, "2.0.0"), json.RawMessage(`2`)) {
		t.Error("a rule value must be a JSON string")
	}
	if evalSemverOperator("semver_between", rawJSON(t, "2.0.0"), rawJSON(t, "2.0.0")) {
		t.Error("an operator outside the six is false")
	}
}

func TestEvalSemverOperatorReadsTheFirstEntryOfAnArrayValue(t *testing.T) {
	if !evalSemverOperator("semver_gte", rawJSON(t, "v2.3"), rawJSON(t, []string{"2.3.0", "9.9.9"})) {
		t.Error("an array rule value compares against its first entry")
	}
	if evalSemverOperator("semver_gte", rawJSON(t, "2.3.0"), rawJSON(t, []string{})) {
		t.Error("an empty array has no version")
	}
}

func TestSemverOperatorsRouteThroughTheConditionEvaluator(t *testing.T) {
	condition := &Condition{
		Type:      "CUSTOM_ATTRIBUTE",
		Attribute: "appVersion",
		Operator:  "semver_gt",
		Value:     rawJSON(t, "2.9.1"),
	}
	var newer, older EvalContext
	if err := json.Unmarshal([]byte(`{"kind":"user","key":"u1","appVersion":"2.10.0"}`), &newer); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"kind":"user","key":"u2","appVersion":"2.9.0"}`), &older); err != nil {
		t.Fatal(err)
	}
	if !EvaluateCondition(condition, newer, AudienceOptions{}) {
		t.Error("2.10.0 should be greater than 2.9.1")
	}
	if EvaluateCondition(condition, older, AudienceOptions{}) {
		t.Error("2.9.0 should not be greater than 2.9.1")
	}
}
