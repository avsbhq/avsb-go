package core

// allocationExclusionReason is the reason an ab_test or targeted_delivery rule
// records when a context falls outside its traffic allocation. Reasons are
// compared byte for byte against the TypeScript reference, so both rule types
// share this one format.
const allocationExclusionReason = "rule %q excluded by traffic allocation (hash %d >= threshold %s)"

// allocationGate is the traffic-allocation gate shared by ab_test and
// targeted_delivery rules. It mirrors allocationExclusion in
// packages/avsb-core/src/evaluate.ts and returns the context's allocation
// bucket, the threshold it was compared against, and whether the context is
// EXCLUDED from the rule.
//
// A nil TrafficAllocation means no gate, and so does a field missing from an
// old datafile, which decodes to nil.
//
// One hash input serves both rule types, hashValue + flagKey + "__" + ruleID,
// so a visitor's rollout membership is the same in every SDK and does not move
// when a rule changes type.
//
// The threshold is a FLOAT comparison against the raw product, never a
// truncated or rounded integer. A 0.12345 rollout puts the threshold at
// 1234.5: bucket 1234 is inside it, and a port that truncates the threshold to
// 1234 excludes that context and diverges from every other language on one
// bucket in ten thousand. Boundary fixtures lock this at fractional and at
// integer thresholds, for both rule types.
func allocationGate(rule *Rule, hashValue, flagKey string) (bucket uint32, threshold float64, excluded bool) {
	if rule.TrafficAllocation == nil {
		return 0, 0, false
	}
	bucket = MurmurHash3(hashValue+flagKey+"__"+rule.ID, 0) % 10000
	threshold = *rule.TrafficAllocation * 10000
	return bucket, threshold, float64(bucket) >= threshold
}
