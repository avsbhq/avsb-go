package core

// Data-plane wire types.
//
// The field names here are FROZEN vocabulary, locked by the conformance
// fixtures at packages/avsb-conformance/fixtures/wire/manifest.json. The
// ingestion worker validates these exact names and the dashboard reads the
// columns they land in, so renaming one is not a compile error anywhere: the
// data simply disappears.

// ExposureEvent is the analytics event emitted when a context is allocated to
// a variation by an ab_test, bandit, or holdout decision.
//
// The field names are frozen wire vocabulary (conformance fixtures
// fixtures/wire/manifest.json). Renaming one silently drops the data.
type ExposureEvent struct {
	// EventID is a UUID v4 stamped once at enqueue time so a retried batch
	// carries the same ids and ingestion can deduplicate.
	EventID string `json:"eventId"`
	// VisitorID is the identity the decision bucketed on.
	VisitorID string `json:"visitorId"`
	// ExperimentID is the rule id for a flag exposure, the holdout id for a
	// holdout exposure.
	ExperimentID string `json:"experimentId"`
	// VariationID is the served variation's id.
	VariationID string `json:"variationId"`
	// EventName is "flag_exposure" or "flag_holdout_exposure".
	EventName string `json:"eventName"`
	// Revenue is null on an exposure. Only conversions carry money.
	Revenue *float64 `json:"revenue"`
	// Value is an optional numeric metric value.
	Value *float64 `json:"value,omitempty"`
	// Timestamp is milliseconds since the Unix epoch.
	Timestamp int64 `json:"timestamp"`
	// PageURL is required by the ingestion schema; server SDKs send "".
	PageURL string `json:"pageUrl"`
	// Properties carries flagKey, flagId and, per decision type, holdout or
	// bandit identifiers.
	Properties map[string]interface{} `json:"properties,omitempty"`
	// Attributes carries registered attributes only. Unregistered keys are
	// dropped server-side.
	Attributes map[string]string `json:"attributes,omitempty"`
}

// MetricEvent is a conversion or metric event. There is no experiment or
// variation on it: attribution happens server-side from the visitor's
// exposures.
//
// The metric batch has NO properties field. Anything sent under that name is
// stripped before storage, so it would look delivered and be gone; put the
// number in Value instead.
type MetricEvent struct {
	// EventID is a UUID v4 stamped once at enqueue time.
	EventID string `json:"eventId"`
	// VisitorID is the identity the conversion is attributed to.
	VisitorID string `json:"visitorId"`
	// EventName is the metric key as configured in the dashboard. An unknown
	// key is accepted and stored, so a typo shows up as an empty metric.
	EventName string `json:"eventName"`
	// Revenue is decimal major units, or null for a non-revenue conversion.
	Revenue *float64 `json:"revenue"`
	// Value is the numeric metric value for average-value metrics, null when
	// the metric is a count.
	Value *float64 `json:"value,omitempty"`
	// Timestamp is milliseconds since the Unix epoch.
	Timestamp int64 `json:"timestamp"`
	// PageURL is required by the ingestion schema; server SDKs send "".
	PageURL string `json:"pageUrl"`
}
