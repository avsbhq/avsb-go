package core

import (
	"encoding/json"
	"strconv"
	"strings"
)

// JSON-value helpers shared by the audience evaluators.
//
// The evaluator holds context attributes as raw JSON (json.RawMessage) rather
// than decoded Go values, so that a datafile round-trips without losing number
// precision or map ordering. The commerce conditions in the reference engine
// branch on the JavaScript RUNTIME TYPE of an attribute (`typeof actual !==
// 'number'` fails closed, `Array.isArray` fails closed, and so on), so the Go
// port needs the same type questions answered against raw JSON.
//
// Everything here is total: no panics, no errors, a bool second return meaning
// "the value was not of that type", which every caller treats as
// condition-evaluates-false.

// jsonKind reports the JSON type of raw as one of "null", "bool", "number",
// "string", "array", "object", or "" for empty/unparseable input.
func jsonKind(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}
	switch trimmed[0] {
	case 'n':
		if trimmed == "null" {
			return "null"
		}
		return ""
	case 't', 'f':
		if trimmed == "true" || trimmed == "false" {
			return "bool"
		}
		return ""
	case '"':
		return "string"
	case '[':
		return "array"
	case '{':
		return "object"
	default:
		if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return "number"
		}
		return ""
	}
}

// jsonNumber returns the numeric value of raw when it is a JSON number.
func jsonNumber(raw json.RawMessage) (float64, bool) {
	if jsonKind(raw) != "number" {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// jsonBool returns the boolean value of raw when it is a JSON boolean.
func jsonBool(raw json.RawMessage) (bool, bool) {
	if jsonKind(raw) != "bool" {
		return false, false
	}
	return strings.TrimSpace(string(raw)) == "true", true
}

// jsonString returns the decoded value of raw when it is a JSON string.
func jsonString(raw json.RawMessage) (string, bool) {
	if jsonKind(raw) != "string" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// jsonObject returns the members of raw when it is a JSON object.
func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if jsonKind(raw) != "object" {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return m, true
}

// jsonArray returns the elements of raw when it is a JSON array.
func jsonArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if jsonKind(raw) != "array" {
		return nil, false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, false
	}
	return arr, true
}

// jsonStringElements returns the string elements of a JSON array, skipping any
// element that is not a string. Mirrors the reference's
// `actual.filter((v) => typeof v === 'string')`.
func jsonStringElements(raw json.RawMessage) []string {
	arr, ok := jsonArray(raw)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := jsonString(item); ok {
			out = append(out, s)
		}
	}
	return out
}

// numberFromJSString parses a numeric string the way JavaScript's
// Number(string) does for the shapes a condition value can carry.
//
// The reference requires the condition value to be a STRING (money and day
// counts are persisted as strings) and treats an empty or non-numeric string
// as "cannot know" rather than zero, because Number("") is 0 in JavaScript and
// silently turning a blank threshold into 0 would match everybody.
func numberFromJSString(raw json.RawMessage) (float64, bool) {
	s, ok := jsonString(raw)
	if !ok {
		return 0, false
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
