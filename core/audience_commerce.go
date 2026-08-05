package core

import (
	"encoding/json"
	"math"
	"time"
)

// Commerce audience condition evaluation.
//
// Port of packages/avsb-core/src/audienceCommerce.ts. Before this existed, an
// e-commerce audience built in the dashboard (cart value, viewed products,
// purchase history) evaluated FALSE on every server SDK with no warning, so a
// working dashboard audience quietly targeted nobody.
//
// Every "cannot know" path (missing or mistyped attribute, non-numeric
// condition value, dataset-backed membership, unknown operator) evaluates to
// false. Nothing here panics.

// defaultCurrencyExponent is the exponent assumed when the datafile does not
// declare one: two decimal places, as USD and EUR use.
const defaultCurrencyExponent = 2

const msPerDay = int64(86_400_000)

// nowMillis is the clock the recency operator reads. It is a package variable
// only so tests can pin it; the conformance fixtures pin their timestamps to
// 1970 and 2100 instead, so the corpus never depends on it.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// commerceAttributeResolver resolves one commerce attribute off the caller's
// context. The audience evaluator supplies it so this file stays free of
// context-shape concerns.
type commerceAttributeResolver func(attribute string) json.RawMessage

// currencyExponentFrom returns the datafile's money exponent, defaulting to 2
// and rejecting negative values.
func currencyExponentFrom(declared *int) int {
	if declared == nil || *declared < 0 {
		return defaultCurrencyExponent
	}
	return *declared
}

// minorToMajor converts a minor-units condition value into decimal major
// units. Returns false for anything non-numeric, which the caller treats as
// condition-evaluates-false.
func minorToMajor(raw json.RawMessage, exponent int) (float64, bool) {
	minor, ok := numberFromJSString(raw)
	if !ok {
		return 0, false
	}
	return minor / math.Pow(10, float64(exponent)), true
}

// singleConditionValue picks the scalar comparison target: element zero of an
// array, otherwise the value itself.
func singleConditionValue(c *Condition) json.RawMessage {
	if arr, ok := jsonArray(c.Value); ok {
		if len(arr) == 0 {
			return nil
		}
		return arr[0]
	}
	return c.Value
}

// evaluateCommerceCondition dispatches one commerce condition. Unknown types
// return false, matching the evaluator's fail-closed default.
func evaluateCommerceCondition(
	c *Condition,
	get commerceAttributeResolver,
	declaredExponent *int,
) bool {
	exponent := currencyExponentFrom(declaredExponent)
	switch c.Type {
	case "CART_VALUE":
		return evalCartValue(c, get, exponent)
	case "PRODUCTS_VIEWED":
		return evalProductsViewed(c, get)
	case "PURCHASE_HISTORY":
		return evalPurchaseHistory(c, get, exponent)
	default:
		return false
	}
}

// --- CART_VALUE ------------------------------------------------------------

func evalCartValue(
	c *Condition,
	get commerceAttributeResolver,
	exponent int,
) bool {
	// cartValue is a decimal major-units NUMBER on the context. A string
	// (a common mistake when reading a form field) is mistyped and fails
	// closed rather than being coerced.
	actual, ok := jsonNumber(get("cartValue"))
	if !ok || math.IsNaN(actual) || math.IsInf(actual, 0) {
		return false
	}

	if c.Operator == "between" {
		bounds, isArray := jsonArray(c.Value)
		if !isArray || len(bounds) < 2 {
			return false
		}
		lower, lowerOK := minorToMajor(bounds[0], exponent)
		upper, upperOK := minorToMajor(bounds[1], exponent)
		if !lowerOK || !upperOK {
			return false
		}
		// between is inclusive on both ends.
		return actual >= lower && actual <= upper
	}

	threshold, ok := minorToMajor(singleConditionValue(c), exponent)
	if !ok {
		return false
	}
	switch c.Operator {
	case "greater_than":
		return actual > threshold
	case "gte":
		return actual >= threshold
	case "less_than":
		return actual < threshold
	case "lte":
		return actual <= threshold
	default:
		return false
	}
}

// --- PRODUCTS_VIEWED -------------------------------------------------------

func evalProductsViewed(c *Condition, get commerceAttributeResolver) bool {
	// Dataset-backed membership cannot be resolved server-side: dataset
	// windows live in the browser. Both operators evaluate false, because
	// degrading to false beats an accidental full-traffic match.
	if c.DatasetSlug != "" {
		return false
	}

	attributeName := "viewedSkus"
	if c.Match == "category" {
		attributeName = "viewedCategories"
	}
	raw := get(attributeName)
	if _, ok := jsonArray(raw); !ok {
		return false
	}
	viewed := make(map[string]struct{})
	for _, v := range jsonStringElements(raw) {
		viewed[v] = struct{}{}
	}

	var candidates []json.RawMessage
	if arr, ok := jsonArray(c.Value); ok {
		candidates = arr
	} else {
		candidates = []json.RawMessage{c.Value}
	}
	targets := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if s, ok := jsonString(candidate); ok && s != "" {
			targets = append(targets, s)
		}
	}
	// No membership source means both operators are false.
	if len(targets) == 0 {
		return false
	}

	anyViewed := false
	for _, target := range targets {
		if _, ok := viewed[target]; ok {
			anyViewed = true
			break
		}
	}
	switch c.Operator {
	case "viewed_any":
		return anyViewed
	case "not_viewed_any":
		return !anyViewed
	default:
		return false
	}
}

// --- PURCHASE_HISTORY ------------------------------------------------------

func evalPurchaseHistory(
	c *Condition,
	get commerceAttributeResolver,
	exponent int,
) bool {
	// A completely absent purchaseHistory attribute makes ALL operators
	// false, INCLUDING has_not_purchased. Server callers own their data:
	// absence means "not provided", not "never purchased". This is a
	// deliberate divergence from the snippet, where a key miss in the
	// commerce store does mean "no purchases".
	profile, ok := jsonObject(get("purchaseHistory"))
	if !ok {
		return false
	}
	hasPurchased, ok := jsonBool(profile["hasPurchased"])
	if !ok {
		return false
	}

	switch c.Operator {
	case "has_purchased":
		return hasPurchased
	case "has_not_purchased":
		return !hasPurchased
	case "last_purchase_older_than":
		days, ok := numberFromJSString(singleConditionValue(c))
		if !ok {
			return false
		}
		if lastPurchaseAt, ok := jsonNumber(profile["lastPurchaseAt"]); ok &&
			!math.IsNaN(lastPurchaseAt) && !math.IsInf(lastPurchaseAt, 0) {
			cutoff := float64(nowMillis()) - days*float64(msPerDay)
			return lastPurchaseAt < cutoff
		}
		// No usable purchase timestamp: vacuously older only when the
		// visitor has never purchased. A purchaser with an unknown
		// timestamp is a "cannot know".
		return !hasPurchased
	case "lifetime_spend_gt":
		threshold, ok := minorToMajor(singleConditionValue(c), exponent)
		if !ok {
			return false
		}
		spend, ok := jsonNumber(profile["lifetimeSpend"])
		if !ok || math.IsNaN(spend) || math.IsInf(spend, 0) {
			return false
		}
		return spend > threshold
	case "order_count_gt":
		minimum, ok := numberFromJSString(singleConditionValue(c))
		if !ok {
			return false
		}
		count, ok := jsonNumber(profile["orderCount"])
		if !ok || math.IsNaN(count) || math.IsInf(count, 0) {
			return false
		}
		return count > minimum
	default:
		return false
	}
}
