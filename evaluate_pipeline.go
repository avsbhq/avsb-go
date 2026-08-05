package avsb

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// ---------------------------------------------------------------------------
// Evaluation
// ---------------------------------------------------------------------------

// evalMeta carries the timing the Flag surfaces.
type evalMeta struct {
	evaluatedAt    time.Time
	durationMicros int64
}

// evaluate is the shared path for every Get*Flag call. It runs the client-layer
// pipeline (runtime override, then sticky) before the pure evaluator, exactly
// as @avsbhq/node does, so a Node service and a Go service agree on a visitor.
func (s *Server) evaluate(
	ctx context.Context,
	key string,
	defaultValue json.RawMessage,
	opts ...Option,
) (core.FlagEvaluation, evalMeta) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	co := applyCallOpts(opts)

	includeReasons := s.opts.IncludeReasons
	if co.includeReasons != nil {
		includeReasons = *co.includeReasons
	}

	// The evaluation context comes from WithContext, or from the Go context
	// the HTTP middleware put it on. Reading the second one is what makes
	// `server.GetBoolFlag(r.Context(), ...)` work after a middleware ran,
	// instead of the middleware being decorative.
	evalCtx := co.ctx
	if evalCtx == nil {
		evalCtx = EvalContextFrom(ctx)
	}
	if evalCtx == nil {
		evalCtx = core.EvalContext{}
	}
	s.warnIfEmptyContext(evalCtx, key)

	s.mu.RLock()
	df := s.df
	closed := s.closed
	tracker := s.tracker
	decisionLog := s.log
	overrideEntry := s.overrides[key]
	s.mu.RUnlock()

	finish := func(eval core.FlagEvaluation) (core.FlagEvaluation, evalMeta) {
		meta := evalMeta{
			evaluatedAt:    time.Now(),
			durationMicros: time.Since(start).Microseconds(),
		}
		decisionLog.LogDecision(DecisionLogEntry{
			FlagKey:        eval.FlagKey,
			VariationKey:   derefString(eval.VariationKey),
			Source:         eval.Source,
			RuleID:         derefString(eval.RuleID),
			RuleType:       derefString(eval.RuleType),
			Reasons:        eval.Reasons,
			EvaluatedAt:    meta.evaluatedAt,
			DurationMicros: meta.durationMicros,
			ContextSummary: core.ContextRefs(evalCtx),
		})
		return eval, meta
	}

	// Not ready: no datafile, or closed. This is NOT not_found. Reporting a
	// missing datafile as "the flag does not exist" is what made a startup
	// race look like a deleted flag.
	if df == nil || closed {
		reasons := []string{}
		if includeReasons {
			if closed {
				reasons = []string{
					"the SDK is closed, so no datafile is loaded. Returning the caller's default",
				}
			} else {
				reasons = []string{
					"the SDK is not ready: no datafile is loaded yet. Await OnReady(ctx) " +
						"(or pass ServerOptions.Bootstrap) before evaluating. Returning the caller's default",
				}
			}
		}
		return finish(core.FlagEvaluation{
			Value:   defaultValue,
			Source:  core.SourceNotReady,
			FlagKey: key,
			Reasons: reasons,
		})
	}

	flagEntry := core.FindFlag(df, key)
	bucketingID := visitorIDFromContext(evalCtx)

	// 1. Runtime override (client layer).
	if flagEntry != nil && overrideEntry != nil {
		if variation := resolveRuntimeOverride(flagEntry, overrideEntry, evalCtx); variation != nil {
			reasons := []string{}
			if includeReasons {
				reasons = []string{fmt.Sprintf("matched runtime override for flagKey=%q", key)}
			}
			variationKey := variation.Key
			return finish(core.FlagEvaluation{
				Value:        variation.Value,
				VariationKey: &variationKey,
				Source:       core.SourceRuntimeOverride,
				FlagKey:      key,
				Reasons:      reasons,
			})
		}
	}

	// 2. Sticky lookup (client layer). It runs BEFORE the evaluator, so a
	// visitor who was already bucketed keeps their variation even when the
	// datafile now says something else. That ordering is deliberate: sticky
	// beats a rule change, because moving a visitor mid-experiment destroys
	// the measurement.
	if s.opts.StickyBucketService != nil && flagEntry != nil && bucketingID != "" {
		if assignment := s.opts.StickyBucketService.Lookup(bucketingID, key); assignment != nil {
			variation := core.VariationByID(flagEntry, assignment.VariationID)
			// A stored variation that no longer exists on the flag is a MISS,
			// not an error: the variation was deleted, so serving it is
			// impossible and the evaluator gets to decide afresh.
			if variation != nil {
				reasons := []string{}
				if includeReasons {
					reasons = []string{
						fmt.Sprintf("matched sticky bucket assignment for bucketingId=%q", bucketingID),
					}
				}
				if tracker != nil && !co.suppressExposure {
					s.fireStickyExposure(tracker, df, flagEntry, variation, assignment, bucketingID, evalCtx)
				}
				variationKey := variation.Key
				ruleID := assignment.RuleID
				ruleType := assignment.RuleType
				return finish(core.FlagEvaluation{
					Value:        variation.Value,
					VariationKey: &variationKey,
					Source:       core.SourceSticky,
					RuleID:       &ruleID,
					RuleType:     &ruleType,
					FlagKey:      key,
					Reasons:      reasons,
				})
			}
		}
	}

	// 3-6. The pure evaluator.
	var onExposure func(core.ExposureEvent)
	if tracker != nil && !co.suppressExposure {
		onExposure = tracker.queueExposure
	}

	result := core.EvaluateFlag(key, defaultValue, evalCtx, df, core.EvaluateOptions{
		IncludeReasons:   includeReasons,
		OnExposure:       onExposure,
		BanditModelStore: s.opts.BanditModelStore,
	})

	// Save the assignment for ab_test rules ONLY. A targeted_delivery is a
	// rollout, not an assignment; a holdout, an override, or a default is not
	// the visitor's to keep.
	if s.opts.StickyBucketService != nil &&
		bucketingID != "" &&
		flagEntry != nil &&
		result.Source == core.SourceRule &&
		result.VariationKey != nil &&
		result.RuleID != nil &&
		result.RuleType != nil &&
		*result.RuleType == core.RuleTypeABTest {
		if variation := core.VariationByKey(flagEntry, *result.VariationKey); variation != nil {
			s.opts.StickyBucketService.Save(bucketingID, key, core.StickyAssignment{
				// The variation ID, never the key. Lookup resolves by id, so
				// saving the key writes a record that can never match again.
				VariationID: variation.ID,
				RuleID:      *result.RuleID,
				RuleType:    core.RuleTypeABTest,
				AssignedAt:  time.Now().UnixMilli(),
			})
		}
	}

	return finish(result)
}

// fireStickyExposure records the exposure for a sticky hit.
//
// When the stored rule no longer matches the datafile the exposure is
// SUPPRESSED and a warning is logged instead: the value is still served (the
// visitor keeps their variation) but there is no rule left to attribute the
// impression to, and attributing it to a rule that changed would corrupt the
// results rather than enrich them.
func (s *Server) fireStickyExposure(
	tracker *eventTracker,
	df *core.Datafile,
	flagEntry *core.FlagEntry,
	variation *core.Variation,
	assignment *core.StickyAssignment,
	bucketingID string,
	evalCtx core.EvalContext,
) {
	var matchingRule *core.Rule
	for i := range flagEntry.Rules {
		if flagEntry.Rules[i].ID != assignment.RuleID {
			continue
		}
		for _, rv := range flagEntry.Rules[i].Variations {
			if rv.VariationID == variation.ID {
				matchingRule = &flagEntry.Rules[i]
				break
			}
		}
		break
	}

	if matchingRule == nil {
		s.logger.Warn("avsb: a sticky assignment points at a rule that no longer matches the datafile. "+
			"Serving the stored variation, but recording no exposure for it",
			"flagKey", flagEntry.Key,
			"bucketingId", bucketingID,
			"assignedRuleId", assignment.RuleID,
			"assignedVariationId", variation.ID,
			"hint", "the rule was edited or deleted after this visitor was bucketed; "+
				"clear the sticky store for this flag to re-bucket, or leave it to keep the split intact")
		return
	}

	tracker.queueExposure(core.ExposureEvent{
		VisitorID:    bucketingID,
		ExperimentID: matchingRule.ID,
		VariationID:  variation.ID,
		EventName:    "flag_exposure",
		Timestamp:    time.Now().UnixMilli(),
		Properties: map[string]interface{}{
			"flagKey": flagEntry.Key,
			"flagId":  flagEntry.ID,
		},
		Attributes: core.BuildExposureAttributes(evalCtx, df),
	})
}

// resolveRuntimeOverride resolves a runtime override for this context.
//
// Precedence is fixed and deterministic: the "user" identity first, then every
// other kind in alphabetical order, then the global override. Ranging over the
// map directly would let a multi-context that matches both a user override and
// an organization override resolve differently between two identical calls,
// because Go randomises map iteration.
//
// An override naming a variation the flag does not have resolves to nothing,
// rather than to a broken value.
func resolveRuntimeOverride(
	flagEntry *core.FlagEntry,
	entry *flagOverrides,
	evalCtx core.EvalContext,
) *core.Variation {
	kinds := make([]string, 0, len(entry.perKindKey))
	for kindKey := range entry.perKindKey {
		kind, _ := splitKindKey(kindKey)
		if kind != "" && kind != "user" {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	kinds = append([]string{"user"}, kinds...)

	variationKey := ""
	for _, kind := range kinds {
		sub := core.ContextByKind(evalCtx, kind)
		if sub == nil {
			continue
		}
		if vk, ok := entry.perKindKey[kind+":"+sub.Key]; ok && vk != "" {
			variationKey = vk
			break
		}
	}
	if variationKey == "" {
		variationKey = entry.global
	}
	if variationKey == "" {
		return nil
	}
	return core.VariationByKey(flagEntry, variationKey)
}

func splitKindKey(kindKey string) (kind, key string) {
	for i := 0; i < len(kindKey); i++ {
		if kindKey[i] == ':' {
			return kindKey[:i], kindKey[i+1:]
		}
	}
	return "", ""
}

// visitorIDFromContext returns the identity an event or a sticky record is
// keyed on: the user key when there is one, otherwise the primary context's.
func visitorIDFromContext(evalCtx core.EvalContext) string {
	if evalCtx == nil {
		return ""
	}
	if key := core.BucketingKey(evalCtx, "user"); key != "" {
		return key
	}
	if primary := core.PrimaryContext(evalCtx); primary != nil {
		return primary.Key
	}
	return ""
}

// warnIfEmptyContext warns once per process when an evaluation runs with no
// identity. Every such call buckets into the same variation, so an experiment
// looks like it is running while measuring one visitor, and nothing else in the
// system can tell.
func (s *Server) warnIfEmptyContext(evalCtx core.EvalContext, flagKey string) {
	if visitorIDFromContext(evalCtx) != "" {
		return
	}
	s.warnedEmptyContext.Do(func() {
		s.logger.Warn("avsb: evaluating with an empty context, so every request buckets identically "+
			"and experiment results will be meaningless",
			"flagKey", flagKey,
			"fix", "pass avsb.WithContext(avsb.UserContext(userID, nil)) on the call, "+
				"or attach one per request with middleware/httpmw")
	})
}

// warnTypeMismatch names a flag whose served value does not fit the getter that
// asked for it, which is otherwise invisible: the caller just gets their
// default and assumes the flag is off.
func (s *Server) warnTypeMismatch(flagKey, wanted string, raw json.RawMessage) {
	s.logger.Warn("avsb: flag value does not fit the getter that asked for it, returning the default",
		"flagKey", flagKey,
		"getter", wanted,
		"servedValue", string(raw),
		"fix", "either change the flag's type in the dashboard or read it with the matching getter "+
			"(GetBoolFlag, GetStringFlag, GetNumberFlag, GetJSONFlag)")
}
