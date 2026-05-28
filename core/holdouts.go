package core

// HoldoutEvaluation is the result of evaluating one holdout for a flag.
type HoldoutEvaluation struct {
	// HeldOut is true when the context lands inside this holdout's traffic window.
	HeldOut bool
	// Holdout is the matched holdout when HeldOut is true.
	Holdout *Holdout
	// VariationID is the variation id served to held-out contexts for the
	// requested flag, when HeldOut is true and a mapping exists.
	VariationID string
}

// EvaluateHoldout checks whether the supplied context is held out from the
// named flag by any active holdout in the datafile.
//
// Spec §8.2 — holdouts + mutual exclusion.
//
// The first holdout that participates in flagID and whose bucket lands inside
// its traffic-allocation window classifies the context as held out. Bucketing
// is murmurhash3(holdout.key + ":" + bucketingKey) % 10_000 so multiple flags
// sharing a holdout get a consistent classification per context — this is the
// mutual-exclusion guarantee.
func EvaluateHoldout(ctx EvalContext, flagID string, df *Datafile) HoldoutEvaluation {
	if len(df.Holdouts) == 0 {
		return HoldoutEvaluation{}
	}

	for i := range df.Holdouts {
		ho := &df.Holdouts[i]

		// Check if this holdout participates in the flag.
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
		if ho.TrafficAllocation <= 0 {
			continue
		}

		// Resolve the bucketing key via the holdout's hashAttribute.
		hashAttr := ho.HashAttribute
		if hashAttr == "" {
			hashAttr = "user.key"
		}
		bucketKey := LegacyAttribute(ctx, hashAttr)

		// murmurhash3(holdout.key + ":" + bucketKey) % 10_000
		hash := MurmurHash3(ho.Key+":"+bucketKey, 0) % 10000
		threshold := uint32(ho.TrafficAllocation * 10000)
		if hash >= threshold {
			continue
		}

		variationID := ho.DefaultVariationByFlag[flagID]
		return HoldoutEvaluation{
			HeldOut:     true,
			Holdout:     ho,
			VariationID: variationID,
		}
	}

	return HoldoutEvaluation{}
}
