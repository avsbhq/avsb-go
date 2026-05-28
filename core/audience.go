package core

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// EvaluateAudience returns true when the supplied context satisfies the
// audience condition tree. Mirrors the TypeScript audience evaluator
// semantics including the "empty audience matches everyone" rule.
func EvaluateAudience(a *Audience, ctx EvalContext) bool {
	if a == nil {
		return true
	}
	return evaluateGroup(&a.Rules, ctx)
}

func evaluateGroup(g *ConditionGroup, ctx EvalContext) bool {
	if g == nil || len(g.Conditions) == 0 {
		return true
	}
	if g.Logic == "AND" {
		for _, child := range g.Conditions {
			if !evaluateChild(child, ctx) {
				return false
			}
		}
		return true
	}
	// OR (and default).
	for _, child := range g.Conditions {
		if evaluateChild(child, ctx) {
			return true
		}
	}
	return false
}

func evaluateChild(raw json.RawMessage, ctx EvalContext) bool {
	if isConditionGroup(raw) {
		var g ConditionGroup
		if err := json.Unmarshal(raw, &g); err != nil {
			return false
		}
		return evaluateGroup(&g, ctx)
	}
	var c Condition
	if err := json.Unmarshal(raw, &c); err != nil {
		return false
	}
	return EvaluateCondition(&c, ctx)
}

// isConditionGroup mirrors the TypeScript `isConditionGroup` predicate:
// `'logic' in item && 'conditions' in item`. We probe for both fields
// being present in the JSON object.
func isConditionGroup(raw json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	_, hasLogic := probe["logic"]
	_, hasConditions := probe["conditions"]
	return hasLogic && hasConditions
}

// EvaluateCondition dispatches one leaf condition. Browser-detected condition
// types (DEVICE/BROWSER/PLATFORM/LANGUAGE/COOKIE/QUERY_PARAM) always evaluate
// to false because the SDK runs server-side; matches the TypeScript
// "isBrowser" gate behaviour for non-browser environments.
func EvaluateCondition(c *Condition, ctx EvalContext) bool {
	switch c.Type {
	case "CUSTOM_ATTRIBUTE":
		return evalCustomAttribute(c, ctx)
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

	// Match TypeScript "missing kind is a hard no-match" gate.
	if c.ContextKind != "" {
		if ContextByKind(ctx, c.ContextKind) == nil {
			return false
		}
	}

	actualRaw := ResolveAttribute(ctx, c.ContextKind, c.Attribute)
	expected := firstStringValue(c.Value)

	switch c.Operator {
	case "exists":
		return actualRaw != nil && !isJSONNull(actualRaw)
	case "not_exists":
		return actualRaw == nil || isJSONNull(actualRaw)
	}

	actual := RawToString(actualRaw)

	switch c.Operator {
	case "equals":
		return actual == expected
	case "not_equals":
		return actual != expected
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
	case "not_contains":
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

// firstStringValue mirrors TypeScript `Array.isArray(value) ? value[0] : value`
// behaviour for picking a single comparison target out of the Condition.
func firstStringValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '[' {
		var arr []string
		if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
			return arr[0]
		}
		var anyArr []json.RawMessage
		if err := json.Unmarshal(raw, &anyArr); err == nil && len(anyArr) > 0 {
			return RawToString(anyArr[0])
		}
		return ""
	}
	return RawToString(raw)
}

func listValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil
		}
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			out = append(out, RawToString(v))
		}
		return out
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
	return string(raw) == "null"
}
