package core

import (
	"encoding/json"
	"math"
	"strings"
)

// Version-number comparison for the six semver_* audience operators, ported
// from packages/avsb-core/src/semver.ts. Every SDK reads one grammar:
//
//   - surrounding ASCII whitespace (space, tab, LF, VT, FF, CR) is trimmed;
//   - one optional leading "v" or "V";
//   - one to three dot-separated numeric parts, digits only, no leading zero
//     unless the part is exactly "0"; a missing minor or patch reads as 0;
//   - an optional "-prerelease" of dot-separated, non-empty [0-9A-Za-z-]
//     identifiers, a digits-only one without a leading zero;
//   - an optional "+build" of the same identifiers (leading zeros allowed),
//     checked and then ignored.
//
// Order is SemVer 2.0.0 section 11. Numeric parts compare as digit strings
// (length first, then byte by byte), so a version past 64 bits orders exactly
// as it does in the TypeScript reference. Any unparsable side, or an
// attribute that is neither a JSON string nor a JSON number, is no match,
// semver_neq included. Pinned by the operator_semver_* conformance cases.

// semverMaxCoreParts is the most numeric parts a version core carries.
const semverMaxCoreParts = 3

// jsExponentThreshold is where JavaScript's String(number) switches to
// exponent notation ("1e+21"), which is not a version. Go's formatter would
// print the digits in full, so a number that large is refused before it is
// formatted.
const jsExponentThreshold = 1e21

// parsedSemver is a version read by parseSemver. Numeric parts are canonical
// digit strings; build metadata is dropped.
type parsedSemver struct {
	major, minor, patch string
	prerelease          []string
}

func isSemverOperator(operator string) bool {
	switch operator {
	case "semver_eq", "semver_neq", "semver_gt", "semver_gte", "semver_lt", "semver_lte":
		return true
	}
	return false
}

func isSemverSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\v' || b == '\f' || b == '\r'
}

func isSemverDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isSemverIdentifierByte(b byte) bool {
	return isSemverDigit(b) || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '-'
}

func isSemverAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isSemverDigit(s[i]) {
			return false
		}
	}
	return true
}

// isSemverNumeric reports digits only, with no leading zero unless the
// identifier is exactly "0".
func isSemverNumeric(s string) bool {
	return isSemverAllDigits(s) && (len(s) == 1 || s[0] != '0')
}

// isSemverDotIdentifier reports a non-empty run of [0-9A-Za-z-]. A byte
// outside ASCII fails, so no multi-byte character can pass.
func isSemverDotIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isSemverIdentifierByte(s[i]) {
			return false
		}
	}
	return true
}

func isSemverPrereleaseIdentifier(s string) bool {
	if !isSemverDotIdentifier(s) {
		return false
	}
	return !isSemverAllDigits(s) || isSemverNumeric(s)
}

func trimSemverSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSemverSpace(s[start]) {
		start++
	}
	for end > start && isSemverSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func allSemver(parts []string, valid func(string) bool) bool {
	for _, part := range parts {
		if !valid(part) {
			return false
		}
	}
	return true
}

// parseSemver reads a version string; ok is false when it does not follow
// the grammar.
func parseSemver(input string) (parsedSemver, bool) {
	text := trimSemverSpace(input)
	if strings.HasPrefix(text, "v") || strings.HasPrefix(text, "V") {
		text = text[1:]
	}

	if plus := strings.IndexByte(text, '+'); plus != -1 {
		if !allSemver(strings.Split(text[plus+1:], "."), isSemverDotIdentifier) {
			return parsedSemver{}, false
		}
		text = text[:plus]
	}

	var prerelease []string
	if dash := strings.IndexByte(text, '-'); dash != -1 {
		prerelease = strings.Split(text[dash+1:], ".")
		if !allSemver(prerelease, isSemverPrereleaseIdentifier) {
			return parsedSemver{}, false
		}
		text = text[:dash]
	}

	parts := strings.Split(text, ".")
	if len(parts) > semverMaxCoreParts || !allSemver(parts, isSemverNumeric) {
		return parsedSemver{}, false
	}
	for len(parts) < semverMaxCoreParts {
		parts = append(parts, "0")
	}
	return parsedSemver{major: parts[0], minor: parts[1], patch: parts[2], prerelease: prerelease}, true
}

func semverSign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// compareSemverNumeric orders two canonical digit strings as the integers
// they spell, at any size.
func compareSemverNumeric(a, b string) int {
	if len(a) != len(b) {
		return semverSign(len(a) - len(b))
	}
	return strings.Compare(a, b)
}

func compareSemverPrereleaseIdentifier(a, b string) int {
	aNumeric, bNumeric := isSemverAllDigits(a), isSemverAllDigits(b)
	switch {
	case aNumeric && bNumeric:
		return compareSemverNumeric(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	}
	// Both are [0-9A-Za-z-] only, so byte order is ASCII order.
	return strings.Compare(a, b)
}

// compareSemver returns -1 when a is lower, 1 when higher, 0 when equal, by
// SemVer 2.0.0 precedence.
func compareSemver(a, b parsedSemver) int {
	if c := compareSemverNumeric(a.major, b.major); c != 0 {
		return c
	}
	if c := compareSemverNumeric(a.minor, b.minor); c != 0 {
		return c
	}
	if c := compareSemverNumeric(a.patch, b.patch); c != 0 {
		return c
	}

	aRelease, bRelease := len(a.prerelease) == 0, len(b.prerelease) == 0
	switch {
	case aRelease && bRelease:
		return 0
	case aRelease:
		return 1
	case bRelease:
		return -1
	}

	shared := len(a.prerelease)
	if len(b.prerelease) < shared {
		shared = len(b.prerelease)
	}
	for i := 0; i < shared; i++ {
		if c := compareSemverPrereleaseIdentifier(a.prerelease[i], b.prerelease[i]); c != 0 {
			return c
		}
	}
	return semverSign(len(a.prerelease) - len(b.prerelease))
}

// semverActualText is the version text of a context attribute: a JSON string
// as is, a JSON number as JavaScript's String(n) prints it, anything else
// (absent, null, boolean, array, object) has none.
func semverActualText(raw json.RawMessage) (string, bool) {
	if s, ok := jsonString(raw); ok {
		return s, true
	}
	n, ok := jsonNumber(raw)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) >= jsExponentThreshold {
		return "", false
	}
	if n == 0 {
		// JavaScript prints negative zero as "0".
		n = 0
	}
	return formatNumberLikeJS(n), true
}

// semverRuleValue is the condition's version: value[0] when the value is an
// array, the value itself otherwise, and only when it is a JSON string.
func semverRuleValue(raw json.RawMessage) (string, bool) {
	if arr, ok := jsonArray(raw); ok {
		if len(arr) == 0 {
			return "", false
		}
		return jsonString(arr[0])
	}
	return jsonString(raw)
}

// evalSemverOperator answers one semver_* condition. It never panics;
// anything unreadable is false.
func evalSemverOperator(operator string, actualRaw, ruleValue json.RawMessage) bool {
	actualText, ok := semverActualText(actualRaw)
	if !ok {
		return false
	}
	expectedText, ok := semverRuleValue(ruleValue)
	if !ok {
		return false
	}
	actual, ok := parseSemver(actualText)
	if !ok {
		return false
	}
	expected, ok := parseSemver(expectedText)
	if !ok {
		return false
	}

	order := compareSemver(actual, expected)
	switch operator {
	case "semver_eq":
		return order == 0
	case "semver_neq":
		return order != 0
	case "semver_gt":
		return order > 0
	case "semver_gte":
		return order >= 0
	case "semver_lt":
		return order < 0
	case "semver_lte":
		return order <= 0
	}
	return false
}
