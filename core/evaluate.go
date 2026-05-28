package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// EvaluateOptions controls evaluator behaviour for one decision.
type EvaluateOptions struct {
	// IncludeReasons turns the audit trail in FlagEvaluation.Reasons on/off.
	// Off matches the per-call DISABLE behaviour from spec §2.11.
	IncludeReasons bool
	// StickyBucketService is an optional persistent bucketing backend. When
	// provided, the evaluator reads sticky assignments before evaluating rules
	// and writes new assignments on ab_test matches.
	StickyBucketService StickyBucketService
	// OnExposure receives an ExposureEvent when an ab_test or holdout rule
	// allocates a user to a variation. Optional.
	OnExposure func(ExposureEvent)
}

// EvaluateFlag computes the flag decision for one (flag, context) pair.
// Mirrors the TypeScript evaluator at packages/avsb-core/src/evaluate.ts.
//
// Decision pipeline:
//  1. flag exists?               → not_found
//  2. flag enabled?              → disabled (serve default variation)
//  3. datafile-declared override → override
//  4. holdout participation?     → holdout
//  5. rule iteration             → targeted_delivery | ab_test
//  6. fallback default variation → default
func EvaluateFlag(
	flagKey string,
	defaultValue json.RawMessage,
	ctx EvalContext,
	df *Datafile,
	opts EvaluateOptions,
) FlagEvaluation {
	start := time.Now()
	var reasons []string
	addReason := func(format string, args ...interface{}) {
		if !opts.IncludeReasons {
			return
		}
		reasons = append(reasons, fmt.Sprintf(format, args...))
	}

	out := func(f FlagEvaluation) FlagEvaluation {
		f.FlagKey = flagKey
		if opts.IncludeReasons {
			f.Reasons = reasons
		} else {
			f.Reasons = []string{}
		}
		_ = start // DurationMicros omitted at SDK layer; agent layer adds it
		if len(f.Value) == 0 {
			f.Value = defaultValue
		}
		return f
	}

	// 1. FIND FLAG.
	flag := findFlag(df, flagKey)
	if flag == nil {
		addReason("flag %q not found in datafile", flagKey)
		return out(FlagEvaluation{Value: defaultValue, Source: "not_found"})
	}

	// 2. CHECK ENABLED.
	if !flag.Enabled {
		addReason("flag %q is disabled, returning default variation", flagKey)
		dv := variationByID(flag, flag.DefaultVariationID)
		return out(buildEval(dv, defaultValue, "disabled", nil, nil))
	}

	// 3. OVERRIDES — overrides match on the primary context's key.
	primaryKey := BucketingKey(ctx, "user")
	if primaryKey == "" {
		if pc := PrimaryContext(ctx); pc != nil {
			primaryKey = pc.Key
		}
	}

	for _, o := range flag.Overrides {
		if o.UserID != "" && o.UserID == primaryKey {
			addReason("matched user override for userId=%q", primaryKey)
			v := variationByID(flag, o.VariationID)
			return out(buildEval(v, defaultValue, "datafileOverride", nil, nil))
		}
	}

	// 4. HOLDOUT CHECK — fires before per-rule iteration so an entire flag
	// can be carved out for the held-out cohort.
	holdout := EvaluateHoldout(ctx, flag.ID, df)
	if holdout.HeldOut && holdout.Holdout != nil {
		targetVarID := holdout.VariationID
		if targetVarID == "" {
			targetVarID = flag.DefaultVariationID
		}
		holdoutVar := variationByID(flag, targetVarID)

		if opts.OnExposure != nil {
			opts.OnExposure(ExposureEvent{
				VisitorID:    primaryKey,
				ExperimentID: holdout.Holdout.ID,
				VariationID:  targetVarID,
				EventName:    "flag_holdout_exposure",
				Timestamp:    time.Now().UnixMilli(),
				Properties: map[string]interface{}{
					"flagKey":    flagKey,
					"flagId":     flag.ID,
					"holdoutId":  holdout.Holdout.ID,
					"holdoutKey": holdout.Holdout.Key,
				},
			})
		}

		varKey := ""
		if holdoutVar != nil {
			varKey = holdoutVar.Key
		} else {
			varKey = targetVarID
		}
		addReason("held out by holdout %q, serving default variation %q", holdout.Holdout.Key, varKey)
		hoID := holdout.Holdout.ID
		hoType := "holdout"
		return out(buildEval(holdoutVar, defaultValue, "holdout", &hoID, &hoType))
	}

	// Build audience lookup.
	audienceMap := make(map[string]*Audience, len(df.Audiences))
	for i := range df.Audiences {
		audienceMap[df.Audiences[i].ID] = &df.Audiences[i]
	}

	// 5. EVALUATE RULES.
	for ri := range flag.Rules {
		rule := &flag.Rules[ri]
		if !rule.Enabled {
			addReason("rule %q skipped (disabled)", rule.ID)
			continue
		}

		hashPath := rule.HashAttribute
		if hashPath == "" {
			hashPath = "userId"
		}
		raw := ResolveDottedAttribute(ctx, hashPath)
		hashValue := ""
		if raw != nil {
			hashValue = RawToString(raw)
		}

		// Audience check.
		if len(rule.AudienceIDs) > 0 {
			matched := false
			for _, aID := range rule.AudienceIDs {
				if a, ok := audienceMap[aID]; ok && EvaluateAudience(a, ctx) {
					matched = true
					break
				}
			}
			if !matched {
				addReason("rule %q skipped (audience not matched)", rule.ID)
				continue
			}
		}

		switch rule.Type {
		case "targeted_delivery":
			if len(rule.Variations) == 0 {
				continue
			}
			targetVar := variationByID(flag, rule.Variations[0].VariationID)
			varKey := ""
			if targetVar != nil {
				varKey = targetVar.Key
			}
			addReason("rule %q matched (targeted_delivery), serving variation %q", rule.ID, varKey)
			rt := "targeted_delivery"
			return out(buildEval(targetVar, defaultValue, "rule", &rule.ID, &rt))

		case "ab_test":
			// Check traffic allocation.
			allocHash := MurmurHash3(hashValue+flagKey+"__"+rule.ID, 0) % 10000
			if rule.TrafficAllocation != nil {
				threshold := uint32(*rule.TrafficAllocation * 10000)
				if allocHash >= threshold {
					addReason(
						"rule %q excluded by traffic allocation (hash %d >= threshold %d)",
						rule.ID, allocHash, threshold,
					)
					continue
				}
			}

			// Assign variation.
			h := MurmurHash3(hashValue+flagKey, 0) % 10000
			cumulative := 0.0
			for _, rv := range rule.Variations {
				cumulative += rv.Percentage * 10000
				if float64(h) < cumulative {
					expVar := variationByID(flag, rv.VariationID)
					if opts.OnExposure != nil && expVar != nil {
						opts.OnExposure(ExposureEvent{
							VisitorID:    hashValue,
							ExperimentID: rule.ID,
							VariationID:  rv.VariationID,
							EventName:    "flag_exposure",
							Timestamp:    time.Now().UnixMilli(),
							Properties: map[string]interface{}{
								"flagKey": flagKey,
								"flagId":  flag.ID,
							},
						})
					}
					expVarKey := ""
					if expVar != nil {
						expVarKey = expVar.Key
					}
					addReason(
						"rule %q matched (ab_test), bucketed into variation %q (hash %d/%.0f)",
						rule.ID, expVarKey, h, cumulative,
					)
					rt := "ab_test"
					return out(buildEval(expVar, defaultValue, "rule", &rule.ID, &rt))
				}
			}
			continue
		}
	}

	// 6. DEFAULT.
	addReason("no rules matched, returning default variation")
	dv := variationByID(flag, flag.DefaultVariationID)
	return out(buildEval(dv, defaultValue, "default", nil, nil))
}

// --- helpers ---

func findFlag(df *Datafile, key string) *FlagEntry {
	for i := range df.Flags {
		if df.Flags[i].Key == key {
			return &df.Flags[i]
		}
	}
	return nil
}

func variationByID(flag *FlagEntry, id string) *Variation {
	for i := range flag.Variations {
		if flag.Variations[i].ID == id {
			return &flag.Variations[i]
		}
	}
	return nil
}

func buildEval(
	v *Variation,
	defaultValue json.RawMessage,
	source string,
	ruleID *string,
	ruleType *string,
) FlagEvaluation {
	f := FlagEvaluation{Source: source, RuleID: ruleID, RuleType: ruleType}
	if v == nil {
		f.Value = defaultValue
		return f
	}
	f.Value = v.Value
	k := v.Key
	f.VariationKey = &k
	return f
}
