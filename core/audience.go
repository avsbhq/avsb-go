package core

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// AudienceOptions carries the datafile-level facts an audience needs.
type AudienceOptions struct {
	// CurrencyExponent is the project's money exponent, from the datafile.
	// Commerce money conditions carry minor units and convert with it. nil
	// means the default of 2.
	CurrencyExponent *int
}

// EvaluateAudience reports whether the context satisfies the audience's
// condition tree. An audience with no conditions matches everyone, which is
// how a rule with an empty audience becomes a full rollout.
func EvaluateAudience(a *Audience, ctx EvalContext, opts AudienceOptions) bool {
	if a == nil {
		return true
	}
	return evaluateGroup(&a.Rules, ctx, opts)
}

func evaluateGroup(g *ConditionGroup, ctx EvalContext, opts AudienceOptions) bool {
	if g == nil || len(g.Conditions) == 0 {
		return true
	}
	if g.Logic == "AND" {
		for _, child := range g.Conditions {
			if !evaluateChild(child, ctx, opts) {
				return false
			}
		}
		return true
	}
	// OR (and any unrecognised logic value).
	for _, child := range g.Conditions {
		if evaluateChild(child, ctx, opts) {
			return true
		}
	}
	return false
}

func evaluateChild(raw json.RawMessage, ctx EvalContext, opts AudienceOptions) bool {
	if isConditionGroup(raw) {
		var g ConditionGroup
		if err := json.Unmarshal(raw, &g); err != nil {
			return false
		}
		return evaluateGroup(&g, ctx, opts)
	}
	var c Condition
	if err := json.Unmarshal(raw, &c); err != nil {
		return false
	}
	return EvaluateCondition(&c, ctx, opts)
}

// isConditionGroup mirrors the reference `isConditionGroup` predicate:
// `'logic' in item && 'conditions' in item`.
func isConditionGroup(raw json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	_, hasLogic := probe["logic"]
	_, hasConditions := probe["conditions"]
	return hasLogic && hasConditions
}

// EvaluateCondition dispatches one leaf condition.
//
// Browser-detected families (DEVICE, BROWSER, PLATFORM, LANGUAGE, COOKIE,
// QUERY_PARAM) always evaluate false here: they read a user agent, a cookie
// jar, or a query string, none of which exist on a server. This matches the
// reference's `isBrowser` gate, which returns false in a non-browser runtime.
func EvaluateCondition(c *Condition, ctx EvalContext, opts AudienceOptions) bool {
	switch c.Type {
	case "CUSTOM_ATTRIBUTE":
		return evalCustomAttribute(c, ctx)
	case "CART_VALUE", "PRODUCTS_VIEWED", "PURCHASE_HISTORY":
		return evaluateCommerceCondition(c, func(attribute string) json.RawMessage {
			return ResolveAttribute(ctx, c.ContextKind, attribute)
		}, opts.CurrencyExponent)
	case "DEVICE", "BROWSER", "PLATFORM", "LANGUAGE", "COOKIE", "QUERY_PARAM":
		return false
	default:
		return false
	}
}

func evalCustomAttribute(c *Condition, ctx EvalContext) bool {
	if c.Attribute == "" {
		return false
	}

	// When the condition names a context kind, that sub-context must exist
	// for the condition to be evaluable at all. A missing KIND is a hard
	// no-match, which is not the same as a missing ATTRIBUTE (that can still
	// satisfy does_not_exist).
	if c.ContextKind != "" && ContextByKind(ctx, c.ContextKind) == nil {
		return false
	}

	actualRaw := ResolveAttribute(ctx, c.ContextKind, c.Attribute)
	expected := firstStringValue(c.Value)

	// Existence operators run before stringification: "" and a missing
	// attribute are different answers.
	//
	// The alias spellings (is, is_not, does_not_contain, does_not_exist) are
	// what the dashboard's audience builder writes for the device and browser
	// condition families, and they reach CUSTOM_ATTRIBUTE conditions too.
	// Before they were handled here, an audience saved with "is" fell through
	// to the default branch and silently matched nobody.
	switch c.Operator {
	case "exists":
		return actualRaw != nil && !isJSONNull(actualRaw)
	case "not_exists", "does_not_exist":
		return actualRaw == nil || isJSONNull(actualRaw)
	}

	actual := RawToString(actualRaw)

	switch c.Operator {
	case "equals", "is":
		return actual == expected
	case "not_equals", "is_not":
		return actual != expected
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
	case "not_contains", "does_not_contain":
		return !strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
	case "starts_with":
		return strings.HasPrefix(strings.ToLower(actual), strings.ToLower(expected))
	case "ends_with":
		return strings.HasSuffix(strings.ToLower(actual), strings.ToLower(expected))
	case "regex":
		re, err := regexp.Compile("(?i)" + expected)
		if err != nil {
			return false
		}
		return re.MatchString(actual)
	case "greater_than":
		return numericCompare(actual, expected, func(a, b float64) bool { return a > b })
	case "less_than":
		return numericCompare(actual, expected, func(a, b float64) bool { return a < b })
	case "gte":
		return numericCompare(actual, expected, func(a, b float64) bool { return a >= b })
	case "lte":
		return numericCompare(actual, expected, func(a, b float64) bool { return a <= b })
	case "in_list":
		for _, v := range listValues(c.Value) {
			if actual == v {
				return true
			}
		}
		return false
	case "not_in_list":
		for _, v := range listValues(c.Value) {
			if actual == v {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// firstStringValue mirrors the reference's
// `Array.isArray(value) ? value[0] : value` choice of a single comparison
// target.
func firstStringValue(raw json.RawMessage) string {
	if arr, ok := jsonArray(raw); ok {
		if len(arr) == 0 {
			return ""
		}
		return RawToString(arr[0])
	}
	return RawToString(raw)
}

func listValues(raw json.RawMessage) []string {
	if arr, ok := jsonArray(raw); ok {
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			out = append(out, RawToString(v))
		}
		return out
	}
	if len(raw) == 0 {
		return nil
	}
	return []string{RawToString(raw)}
}

func numericCompare(actual, expected string, cmp func(a, b float64) bool) bool {
	a, err := strconv.ParseFloat(strings.TrimSpace(actual), 64)
	if err != nil {
		return false
	}
	b, err := strconv.ParseFloat(strings.TrimSpace(expected), 64)
	if err != nil {
		return false
	}
	return cmp(a, b)
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
