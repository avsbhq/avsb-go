package avsb

// Purchase wire types. Orders are the one event this SDK sends immediately
// rather than batching, so they get their own file next to the tracker that
// posts them.

// PurchaseOrder is a server-side purchase. Decimal values are forwarded as
// given; there is no currency conversion on the way out.
type PurchaseOrder struct {
	// OrderID is the unique order or transaction identifier. Required.
	OrderID string `json:"orderId"`
	// Total is the order total in decimal major units as displayed (99.95,
	// not 9995). Must be greater than zero. Required.
	Total float64 `json:"total"`
	// Currency is the ISO 4217 code.
	Currency string `json:"currency,omitempty"`
	// Subtotal is the pre-tax, pre-shipping total.
	Subtotal *float64 `json:"subtotal,omitempty"`
	// Shipping is the shipping charge.
	Shipping *float64 `json:"shipping,omitempty"`
	// Tax is the tax charged.
	Tax *float64 `json:"tax,omitempty"`
	// Discount is the discount applied.
	Discount *float64 `json:"discount,omitempty"`
	// Coupon is the coupon code used.
	Coupon string `json:"coupon,omitempty"`
	// Test marks a test purchase. Test orders are stored but excluded from
	// every revenue, profit, recommendation, and audience calculation. Only
	// set it when deliberately testing an integration.
	Test bool `json:"test,omitempty"`
	// Items is the line items.
	Items []PurchaseItem `json:"items,omitempty"`
}

// PurchaseItem is one line item on a PurchaseOrder.
type PurchaseItem struct {
	// SKU is the stock-keeping unit. Required.
	SKU string `json:"sku"`
	// ProductKey is the canonical product-grain key. Pass it when known so
	// order items join catalog analytics without alias resolution; the
	// ingestion worker derives it from SKU otherwise.
	ProductKey string `json:"productKey,omitempty"`
	// Name is the product name.
	Name string `json:"name,omitempty"`
	// Price is the unit price in decimal major units.
	Price *float64 `json:"price,omitempty"`
	// Quantity is the number of units.
	Quantity *int `json:"quantity,omitempty"`
	// Category is the product category.
	Category string `json:"category,omitempty"`
}

// purchasePayload is the wire shape for one order: the caller's order plus the
// fields the tracker stamps.
type purchasePayload struct {
	PurchaseOrder
	EventID   string `json:"eventId"`
	VisitorID string `json:"visitorId"`
	Timestamp int64  `json:"timestamp"`
	PageURL   string `json:"pageUrl"`
}
