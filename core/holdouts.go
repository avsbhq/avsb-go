package core

// HoldoutEvaluation is the result of checking the datafile's holdouts for one
// flag.
type HoldoutEvaluation struct {
	// HeldOut is true when the context lands inside a holdout's traffic
	// window for this flag.
	HeldOut bool
	// Holdout is the matched holdout when HeldOut is true.
	Holdout *Holdout
	// VariationID is the variation served to held-out contexts for the
	// requested flag, empty when the holdout names none (the flag's own
	// default variation is served then).
	VariationID string
	// BucketingKey is the identity the holdout bucketed on, resolved through
	// the holdout's own hashAttribute. Exposure events use it so an org-keyed
	// holdout attributes its exposure to the organization rather than to
	// whichever member happened to make the request.
	BucketingKey string
}

// EvaluateHoldout reports whether the context is held out from the named flag.
//
// The first holdout that participates in flagID and whose bucket lands inside
// its traffic-allocation window classifies the context as held out. Bucketing
// is murmurhash3(holdout.key + ":" + bucketingKey) % 10000, so several flags
// sharing one holdout classify a context identically: that is the
// mutual-exclusion guarantee.
func EvaluateHoldout(ctx EvalContext, flagID string, df *Datafile) HoldoutEvaluation {
	if df == nil || len(df.Holdouts) == 0 {
		return HoldoutEvaluation{}
	}

	for i := range df.Holdouts {
		ho := &df.Holdouts[i]

		participates := false
		for _, fid := range ho.FlagIDs {
			if fid == flagID {
				participates = true
				break
			}
		}
		if !participates {
			continue
		}
		// A zero or negative allocation is a holdout that is switched off,
		// not one that holds nobody out by luck. Skipping it here is what
		// lets the flag's own rules run.
		if ho.TrafficAllocation <= 0 {
			continue
		}

		hashAttr := ho.HashAttribute
		if hashAttr == "" {
			hashAttr = "user.key"
		}
		bucketKey := LegacyAttribute(ctx, hashAttr)

		hash := MurmurHash3(ho.Key+":"+bucketKey, 0) % 10000
		// Float threshold, for the same reason the ab_test allocation uses
		// one: truncating to a uint32 moves the boundary by up to one bucket
		// and puts this SDK one context out of step with every other language
		// on a fractional allocation such as 0.999.
		if float64(hash) >= ho.TrafficAllocation*10000 {
			continue
		}

		return HoldoutEvaluation{
			HeldOut:      true,
			Holdout:      ho,
			VariationID:  ho.DefaultVariationByFlag[flagID],
			BucketingKey: bucketKey,
		}
	}

	return HoldoutEvaluation{}
}
