package core

import (
	"encoding/json"
	"testing"
)

// intPtr is a local helper for the currency-exponent option.
func intPtr(v int) *int { return &v }

// TestCommerceOperators locks the six commerce operators plus their fail-closed
// paths. Before they existed, an e-commerce audience built in the dashboard
// evaluated false on every server SDK with no warning, so it looked configured
// and targeted nobody.
//
// Money on a condition is in MINOR units and converts with the project's
// currency exponent, which is why "5000" means 50.00 at exponent 2 and 5000 at
// exponent 0.
func TestCommerceOperators(t *testing.T) {
	cases := []struct {
		name      string
		condition Condition
		attrs     map[string]interface{}
		exponent  *int
		want      bool
	}{
		{
			name: "cart value between is inclusive at the upper bound",
			condition: Condition{
				Type: "CART_VALUE", Operator: "between",
				Value: mustRaw([]string{"5000", "10000"}), ContextKind: "user",
			},
			attrs: map[string]interface{}{"cartValue": 100},
			want:  true,
		},
		{
			name: "cart value just above the upper bound misses",
			condition: Condition{
				Type: "CART_VALUE", Operator: "between",
				Value: mustRaw([]string{"5000", "10000"}), ContextKind: "user",
			},
			attrs: map[string]interface{}{"cartValue": 100.01},
			want:  false,
		},
		{
			name: "cart value between is inclusive at the lower bound",
			condition: Condition{
				Type: "CART_VALUE", Operator: "between",
				Value: mustRaw([]string{"5000", "10000"}), ContextKind: "user",
			},
			attrs: map[string]interface{}{"cartValue": 50},
			want:  true,
		},
		{
			name: "cart value greater_than converts the threshold from minor units",
			condition: Condition{
				Type: "CART_VALUE", Operator: "greater_than",
				Value: mustRaw("5000"), ContextKind: "user",
			},
			attrs: map[string]interface{}{"cartValue": 50.01},
			want:  true,
		},
		{
			name: "a cart value sent as a string is mistyped and fails closed",
			condition: Condition{
				Type: "CART_VALUE", Operator: "greater_than",
				Value: mustRaw("5000"), ContextKind: "user",
			},
			attrs: map[string]interface{}{"cartValue": "100"},
			want:  false,
		},
		{
			name: "a blank threshold fails closed rather than matching everyone",
			condition: Condition{
				Type: "CART_VALUE", Operator: "greater_than",
				Value: mustRaw(""), ContextKind: "user",
			},
			attrs: map[string]interface{}{"cartValue": 100},
			want:  false,
		},
		{
			name: "viewed_any matches when one listed sku was viewed",
			condition: Condition{
				Type: "PRODUCTS_VIEWED", Operator: "viewed_any",
				Value: mustRaw([]string{"SKU-RED-01", "SKU-BLUE-02"}), ContextKind: "user",
			},
			attrs: map[string]interface{}{"viewedSkus": []string{"SKU-GREEN-09", "SKU-BLUE-02"}},
			want:  true,
		},
		{
			name: "viewed_any misses when none was viewed",
			condition: Condition{
				Type: "PRODUCTS_VIEWED", Operator: "viewed_any",
				Value: mustRaw([]string{"SKU-RED-01", "SKU-BLUE-02"}), ContextKind: "user",
			},
			attrs: map[string]interface{}{"viewedSkus": []string{"SKU-GREEN-09"}},
			want:  false,
		},
		{
			name: "not_viewed_any is the negation, not a separate answer",
			condition: Condition{
				Type: "PRODUCTS_VIEWED", Operator: "not_viewed_any",
				Value: mustRaw([]string{"SKU-RED-01"}), ContextKind: "user",
			},
			attrs: map[string]interface{}{"viewedSkus": []string{"SKU-GREEN-09"}},
			want:  true,
		},
		{
			name: "match category reads viewedCategories instead of viewedSkus",
			condition: Condition{
				Type: "PRODUCTS_VIEWED", Operator: "viewed_any",
				Value: mustRaw([]string{"outerwear"}), ContextKind: "user", Match: "category",
			},
			attrs: map[string]interface{}{
				"viewedSkus":       []string{"outerwear"},
				"viewedCategories": []string{"footwear"},
			},
			want: false,
		},
		{
			name: "a dataset-backed condition cannot be resolved server-side and is false",
			condition: Condition{
				Type: "PRODUCTS_VIEWED", Operator: "viewed_any",
				Value: mustRaw([]string{"SKU-RED-01"}), ContextKind: "user",
				DatasetSlug: "recently-viewed",
			},
			attrs: map[string]interface{}{"viewedSkus": []string{"SKU-RED-01"}},
			want:  false,
		},
		{
			name: "a dataset-backed condition is false for the negated operator too",
			condition: Condition{
				Type: "PRODUCTS_VIEWED", Operator: "not_viewed_any",
				Value: mustRaw([]string{"SKU-RED-01"}), ContextKind: "user",
				DatasetSlug: "recently-viewed",
			},
			attrs: map[string]interface{}{"viewedSkus": []string{"SKU-GREEN-09"}},
			want:  false,
		},
		{
			name: "has_purchased matches a purchaser",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "has_purchased",
				Value: mustRaw(""), ContextKind: "user",
			},
			attrs: map[string]interface{}{"purchaseHistory": map[string]interface{}{"hasPurchased": true}},
			want:  true,
		},
		{
			name: "has_purchased misses a non-purchaser",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "has_purchased",
				Value: mustRaw(""), ContextKind: "user",
			},
			attrs: map[string]interface{}{"purchaseHistory": map[string]interface{}{"hasPurchased": false}},
			want:  false,
		},
		{
			name: "an absent purchase history makes has_not_purchased false too",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "has_not_purchased",
				Value: mustRaw(""), ContextKind: "user",
			},
			attrs: nil,
			want:  false,
		},
		{
			name: "has_not_purchased matches a supplied non-purchaser",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "has_not_purchased",
				Value: mustRaw(""), ContextKind: "user",
			},
			attrs: map[string]interface{}{"purchaseHistory": map[string]interface{}{"hasPurchased": false}},
			want:  true,
		},
		{
			name: "lifetime_spend_gt converts from minor units at exponent 2",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "lifetime_spend_gt",
				Value: mustRaw("25000"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true, "lifetimeSpend": 250.01},
			},
			want: true,
		},
		{
			name: "lifetime_spend_gt is strictly greater, so an equal spend misses",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "lifetime_spend_gt",
				Value: mustRaw("25000"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true, "lifetimeSpend": 250},
			},
			want: false,
		},
		{
			name: "at exponent 0 the minor-unit value IS the major amount",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "lifetime_spend_gt",
				Value: mustRaw("25000"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true, "lifetimeSpend": 25001},
			},
			exponent: intPtr(0),
			want:     true,
		},
		{
			name: "at exponent 0 a 250 spend no longer clears a 25000 threshold",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "lifetime_spend_gt",
				Value: mustRaw("25000"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true, "lifetimeSpend": 250},
			},
			exponent: intPtr(0),
			want:     false,
		},
		{
			name: "order_count_gt is strictly greater",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "order_count_gt",
				Value: mustRaw("3"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true, "orderCount": 3},
			},
			want: false,
		},
		{
			name: "order_count_gt matches above the threshold",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "order_count_gt",
				Value: mustRaw("3"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true, "orderCount": 4},
			},
			want: true,
		},
		{
			name: "a missing orderCount fails closed",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "order_count_gt",
				Value: mustRaw("3"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true},
			},
			want: false,
		},
		{
			name: "last_purchase_older_than matches a 1970 timestamp",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "last_purchase_older_than",
				Value: mustRaw("30"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true, "lastPurchaseAt": 0},
			},
			want: true,
		},
		{
			name: "last_purchase_older_than misses a 2100 timestamp",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "last_purchase_older_than",
				Value: mustRaw("30"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{
					"hasPurchased": true, "lastPurchaseAt": 4102444800000,
				},
			},
			want: false,
		},
		{
			name: "a purchaser with no timestamp is a cannot-know, so false",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "last_purchase_older_than",
				Value: mustRaw("30"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true},
			},
			want: false,
		},
		{
			name: "a supplied non-purchaser is vacuously older than any window",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "last_purchase_older_than",
				Value: mustRaw("30"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": false},
			},
			want: true,
		},
		{
			name: "a purchase history missing hasPurchased is mistyped and fails closed",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "has_purchased",
				Value: mustRaw(""), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"lifetimeSpend": 500},
			},
			want: false,
		},
		{
			name: "an unknown commerce operator is false, never a match",
			condition: Condition{
				Type: "PURCHASE_HISTORY", Operator: "spent_a_lot_probably",
				Value: mustRaw("1"), ContextKind: "user",
			},
			attrs: map[string]interface{}{
				"purchaseHistory": map[string]interface{}{"hasPurchased": true},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			condition := tc.condition
			condition.ID = "c_commerce"
			ctx := makeUserCtx("u_commerce", tc.attrs)
			got := EvaluateCondition(&condition, ctx, AudienceOptions{CurrencyExponent: tc.exponent})
			if got != tc.want {
				t.Errorf("%s %s: want %v, got %v", condition.Type, condition.Operator, tc.want, got)
			}
		})
	}
}

// TestCurrencyExponentFrom covers the defaulting rule: an absent or nonsensical
// exponent means two decimal places, because that is what most currencies use
// and silently treating money as 100x its value is the worst outcome.
func TestCurrencyExponentFrom(t *testing.T) {
	cases := []struct {
		name     string
		declared *int
		want     int
	}{
		{name: "absent defaults to two", declared: nil, want: 2},
		{name: "zero is a real value for JPY", declared: intPtr(0), want: 0},
		{name: "three is honoured", declared: intPtr(3), want: 3},
		{name: "negative is nonsense and defaults", declared: intPtr(-1), want: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := currencyExponentFrom(tc.declared); got != tc.want {
				t.Errorf("want %d, got %d", tc.want, got)
			}
		})
	}
}

// TestJSONTypeGuards covers the raw-JSON type questions the commerce operators
// ask. They exist because the reference branches on the JavaScript runtime type
// of an attribute, and a Go port that coerced instead would match traffic the
// reference does not.
func TestJSONTypeGuards(t *testing.T) {
	t.Run("jsonNumber accepts only JSON numbers", func(t *testing.T) {
		if _, ok := jsonNumber(json.RawMessage(`42.5`)); !ok {
			t.Error("42.5 should be a number")
		}
		if _, ok := jsonNumber(json.RawMessage(`"42.5"`)); ok {
			t.Error("a quoted number is a string, not a number")
		}
		if _, ok := jsonNumber(json.RawMessage(`null`)); ok {
			t.Error("null is not a number")
		}
	})

	t.Run("jsonObject rejects arrays", func(t *testing.T) {
		if _, ok := jsonObject(json.RawMessage(`{"a":1}`)); !ok {
			t.Error("an object should parse")
		}
		if _, ok := jsonObject(json.RawMessage(`[1,2]`)); ok {
			t.Error("an array is not an object")
		}
	})

	t.Run("jsonStringElements skips non-string elements", func(t *testing.T) {
		got := jsonStringElements(json.RawMessage(`["a",1,null,"b"]`))
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("want [a b], got %v", got)
		}
	})

	t.Run("numberFromJSString rejects blank and non-numeric strings", func(t *testing.T) {
		if v, ok := numberFromJSString(json.RawMessage(`" 30 "`)); !ok || v != 30 {
			t.Errorf("a padded numeric string should parse to 30, got %v %v", v, ok)
		}
		if _, ok := numberFromJSString(json.RawMessage(`""`)); ok {
			t.Error("a blank string must not become zero")
		}
		if _, ok := numberFromJSString(json.RawMessage(`"lots"`)); ok {
			t.Error("a non-numeric string must not parse")
		}
		if _, ok := numberFromJSString(json.RawMessage(`30`)); ok {
			t.Error("condition values are strings on the wire; a bare number is out of contract")
		}
	})
}
