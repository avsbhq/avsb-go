package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// EvaluateOptions controls evaluator behaviour for one decision.
type EvaluateOptions struct {
	// IncludeReasons turns the ordered decision trail in
	// FlagEvaluation.Reasons on. The conformance harness always runs with it
	// on, because the reasons are part of the cross-language contract.
	IncludeReasons bool
	// OnExposure receives an ExposureEvent when an ab_test, bandit, or
	// holdout decision allocates a context to a variation. Optional.
	OnExposure func(ExposureEvent)
	// BanditModelStore is consulted for bandit-typed rules. Models are
	// client-layer state (they are learned, not published in the datafile),
	// so when no store is supplied the evaluator falls back to a
	// deterministic cold-start model built from the datafile's bandit config
	// alone. That fallback is what the conformance corpus locks: every port
	// produces the same action for the same datafile and context with no
	// model present.
	BanditModelStore BanditModelStore
}

// EvaluateFlag computes the flag decision for one (flag, context) pair.
//
// Decision pipeline, mirroring packages/avsb-core/src/evaluate.ts:
//
//  1. flag exists?               -> not_found
//  2. flag enabled?              -> disabled (serves the default variation)
//  3. datafile-declared override -> datafileOverride
//  4. holdout participation?     -> holdout
//  5. rules in declared order    -> rule (targeted_delivery, ab_test) | bandit
//  6. fallback                   -> default
//
// Sticky bucketing and runtime overrides are NOT here: they are client-layer
// concerns, so this function stays a pure function of datafile plus context.
// The SDK layer runs them before calling in (see Server.evaluate).
func EvaluateFlag(
	flagKey string,
	defaultValue json.RawMessage,
	ctx EvalContext,
	df *Datafile,
	opts EvaluateOptions,
) FlagEvaluation {
	var reasons []string
	addReason := func(format string, args ...interface{}) {
		if !opts.IncludeReasons {
			return
		}
		reasons = append(reasons, fmt.Sprintf(format, args...))
	}

	out := func(f FlagEvaluation) FlagEvaluation {
		f.FlagKey = flagKey
		// Reasons is never nil, whether collection is on or off: a caller
		// ranging over it should not have to nil-check.
		if opts.IncludeReasons && reasons != nil {
			f.Reasons = reasons
		} else {
			f.Reasons = []string{}
		}
		if len(f.Value) == 0 {
			f.Value = defaultValue
		}
		return f
	}

	if df == nil {
		// The SDK layer answers not_ready before it gets here; this guard
		// exists so a direct core caller cannot dereference nil.
		addReason("no datafile loaded, returning the caller's default")
		return out(FlagEvaluation{Value: defaultValue, Source: SourceNotReady})
	}

	// 1. FIND FLAG.
	flag := findFlag(df, flagKey)
	if flag == nil {
		addReason("flag %q not found in datafile", flagKey)
		return out(FlagEvaluation{Value: defaultValue, Source: SourceNotFound})
	}

	// 2. CHECK ENABLED.
	if !flag.Enabled {
		addReason("flag %q is disabled, returning default variation", flagKey)
		dv := variationByID(flag, flag.DefaultVariationID)
		return out(buildEval(dv, defaultValue, SourceDisabled, nil, nil))
	}

	// 3. OVERRIDES. Datafile overrides match on the primary context's key,
	// falling back to the first declared kind when there is no user kind.
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
			return out(buildEval(v, defaultValue, SourceDatafileOverride, nil, nil))
		}
	}

	// 4. HOLDOUT CHECK. Fires between overrides and the rules loop, so an
	// explicit override still wins (that is operator intent) while every
	// regular rule honours the holdout.
	holdout := EvaluateHoldout(ctx, flag.ID, df)
	if holdout.HeldOut && holdout.Holdout != nil {
		targetVarID := holdout.VariationID
		if targetVarID == "" {
			targetVarID = flag.DefaultVariationID
		}
		holdoutVar := variationByID(flag, targetVarID)

		if opts.OnExposure != nil {
			opts.OnExposure(ExposureEvent{
				// The identity the HOLDOUT bucketed on, which is not always
				// the user key: an org-keyed holdout buckets by organization,
				// and attributing its exposure to a user id would split one
				// org's traffic across every member.
				VisitorID:    holdout.BucketingKey,
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
				Attributes: BuildExposureAttributes(ctx, df),
			})
		}

		varKey := targetVarID
		if holdoutVar != nil {
			varKey = holdoutVar.Key
		}
		addReason("held out by holdout %q, serving default variation %q", holdout.Holdout.Key, varKey)
		hoID := holdout.Holdout.ID
		hoType := RuleTypeHoldout
		return out(buildEval(holdoutVar, defaultValue, SourceHoldout, &hoID, &hoType))
	}

	// Build the audience lookup once for the whole rule loop.
	audienceMap := make(map[string]*Audience, len(df.Audiences))
	for i := range df.Audiences {
		audienceMap[df.Audiences[i].ID] = &df.Audiences[i]
	}
	audienceOpts := AudienceOptions{CurrencyExponent: df.CurrencyExponent}

	// 5. EVALUATE RULES, in declared order. First match wins.
	for ri := range flag.Rules {
		rule := &flag.Rules[ri]
		if !rule.Enabled {
			addReason("rule %q skipped (disabled)", rule.ID)
			continue
		}

		// Resolve this rule's bucketing key. v1 datafiles persist
		// hashAttribute "userId" and mean the primary context's key; v2
		// datafiles persist dotted paths like "user.key".
		hashPath := rule.HashAttribute
		if hashPath == "" {
			hashPath = "userId"
		}
		hashValue := ""
		if raw := ResolveDottedAttribute(ctx, hashPath); raw != nil {
			hashValue = RawToString(raw)
		}

		// a. CHECK AUDIENCE. Any listed audience matching is enough.
		if len(rule.AudienceIDs) > 0 {
			matched := false
			for _, aID := range rule.AudienceIDs {
				if a, ok := audienceMap[aID]; ok && EvaluateAudience(a, ctx, audienceOpts) {
					matched = true
					break
				}
			}
			if !matched {
				addReason("rule %q skipped (audience not matched)", rule.ID)
				continue
			}
		}

		// b. EVALUATE BY TYPE.
		switch rule.Type {
		case RuleTypeTargetedDelivery:
			if len(rule.Variations) == 0 {
				continue
			}
			// A targeted delivery rule below 100 percent is a ROLLOUT: serve
			// the variation to that share of the audience and let everyone
			// else fall through to the next rule, exactly as an ab_test
			// rule's allocation does (N1-1). The gate sits after the
			// empty-variations skip, so a rule with nothing to serve stays a
			// silent skip.
			if bucket, threshold, excluded := allocationGate(rule, hashValue, flagKey); excluded {
				addReason(allocationExclusionReason, rule.ID, bucket, formatNumberLikeJS(threshold))
				continue
			}
			targetVar := variationByID(flag, rule.Variations[0].VariationID)
			addReason(
				"rule %q matched (targeted_delivery), serving variation %q",
				rule.ID, reasonVariationKey(targetVar),
			)
			rt := RuleTypeTargetedDelivery
			return out(buildEval(targetVar, defaultValue, SourceRule, &rule.ID, &rt))

		case RuleTypeABTest:
			// Traffic allocation: see allocationGate for the float-threshold
			// rule every port must copy.
			if bucket, threshold, excluded := allocationGate(rule, hashValue, flagKey); excluded {
				addReason(allocationExclusionReason, rule.ID, bucket, formatNumberLikeJS(threshold))
				continue
			}

			// Assign a variation from the cumulative split.
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
							Attributes: BuildExposureAttributes(ctx, df),
						})
					}
					addReason(
						"rule %q matched (ab_test), bucketed into variation %q (hash %d/%s)",
						rule.ID, reasonVariationKey(expVar), h, formatNumberLikeJS(cumulative),
					)
					rt := RuleTypeABTest
					return out(buildEval(expVar, defaultValue, SourceRule, &rule.ID, &rt))
				}
			}
			continue

		case RuleTypeBandit:
			// Bandit rules dispatch through the shared bandit engine. Every
			// exit below appends a reason: a bandit rule must never
			// disappear from the decision trail, which is what it used to do
			// (Go had no case for it at all, so a configured bandit behaved
			// as "no rules matched" with nothing logged).
			bandit := resolveRuleBandit(rule, df)
			if bandit == nil {
				if rule.BanditID != "" {
					addReason(
						"rule %q skipped (banditId %q is not in datafile.bandits)",
						rule.ID, rule.BanditID,
					)
				} else {
					addReason("rule %q skipped (bandit rule carries no bandit config)", rule.ID)
				}
				continue
			}
			if len(bandit.Config.Actions) == 0 {
				addReason("rule %q skipped (bandit %q declares no actions)", rule.ID, bandit.Key)
				continue
			}

			store := opts.BanditModelStore
			if store == nil {
				store = coldStartBanditStore(bandit.Config)
			}
			decision, err := EvaluateBandit(bandit.Key, bandit.Config, ctx, store)
			if err != nil {
				addReason(
					"rule %q skipped (bandit %q could not select an action: %s)",
					rule.ID, bandit.Key, err.Error(),
				)
				continue
			}
			if decision == nil {
				addReason(
					"rule %q skipped (bandit %q has no model in the supplied store)",
					rule.ID, bandit.Key,
				)
				continue
			}

			banditVar := variationByID(flag, decision.Action.VariationID)
			if banditVar == nil {
				addReason(
					"rule %q skipped (bandit %q action %q targets unknown variation %q)",
					rule.ID, bandit.Key, decision.Action.ID, decision.Action.VariationID,
				)
				continue
			}

			if opts.OnExposure != nil {
				opts.OnExposure(ExposureEvent{
					VisitorID:    hashValue,
					ExperimentID: rule.ID,
					VariationID:  decision.Action.VariationID,
					EventName:    "flag_exposure",
					Timestamp:    time.Now().UnixMilli(),
					Properties: map[string]interface{}{
						"flagKey":   flagKey,
						"flagId":    flag.ID,
						"banditKey": bandit.Key,
						"actionId":  decision.Action.ID,
					},
					Attributes: BuildExposureAttributes(ctx, df),
				})
			}

			addReason(
				"rule %q matched (bandit %q), served action %q into variation %q",
				rule.ID, bandit.Key, decision.Action.ID, banditVar.Key,
			)
			rt := RuleTypeBandit
			return out(buildEval(banditVar, defaultValue, SourceBandit, &rule.ID, &rt))

		default:
			// An unknown rule type on the wire gets named rather than
			// dropped: a newer dashboard can publish a type this SDK version
			// has never heard of, and the reason is the only clue.
			addReason("rule %q skipped (unsupported rule type %q)", rule.ID, rule.Type)
			continue
		}
	}

	// 6. DEFAULT.
	addReason("no rules matched, returning default variation")
	dv := variationByID(flag, flag.DefaultVariationID)
	return out(buildEval(dv, defaultValue, SourceDefault, nil, nil))
}
