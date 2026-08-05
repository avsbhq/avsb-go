package core

import (
	"math"
	"sort"
	"strings"
)

// Bandit evaluation: epsilon-greedy, Thompson sampling, UCB1.
//
// Port of packages/avsb-core/src/bandits.ts. Design invariants, do not regress:
//
//   - The algorithms are pure functions of (perAction, actions, hashBucket).
//   - No real randomness. Every stochastic step is seeded from MurmurHash3, so
//     one (banditKey, context) pair always produces the same decision. That is
//     what makes concrete expected actions testable and lets an incident be
//     replayed.
//   - Ties break alphabetically on action id, also for determinism. It is why
//     cold start serves the lowest action id rather than the first declared.

// BanditModelStore is the interface bandit model backends satisfy.
type BanditModelStore interface {
	// GetModel returns the current model for the named bandit, or nil when no
	// model has been loaded yet.
	GetModel(banditKey string) *BanditModel
	// UpdateModel persists or updates the model. Fire and forget; callers may
	// batch or persist asynchronously.
	UpdateModel(banditKey string, model *BanditModel)
}

// InMemoryBanditModelStore is an in-memory BanditModelStore for tests and
// single-goroutine usage. It is NOT safe for concurrent use.
type InMemoryBanditModelStore struct {
	models map[string]*BanditModel
}

// NewInMemoryBanditModelStore creates a store, optionally seeded with models.
func NewInMemoryBanditModelStore(initial map[string]*BanditModel) *InMemoryBanditModelStore {
	s := &InMemoryBanditModelStore{models: make(map[string]*BanditModel)}
	for k, m := range initial {
		s.models[k] = m
	}
	return s
}

// GetModel returns the current model, or nil.
func (s *InMemoryBanditModelStore) GetModel(banditKey string) *BanditModel {
	return s.models[banditKey]
}

// UpdateModel stores a model.
func (s *InMemoryBanditModelStore) UpdateModel(banditKey string, model *BanditModel) {
	s.models[banditKey] = model
}

// coldStartModelVersion is the version reported for the derived model the
// evaluator falls back to when the caller supplied no store.
const coldStartModelVersion = "cold-start"

// coldStartBanditStore hands back a zero-observation model for any bandit key,
// so the algorithms run their documented cold-start policies instead of the
// rule being silently skipped. A bandit that has never been trained still has
// to serve something, and "the lowest action id, deterministically" is an
// answer every language agrees on.
func coldStartBanditStore(config BanditConfig) BanditModelStore {
	model := &BanditModel{
		Version:   coldStartModelVersion,
		Algorithm: config.Algorithm,
		PerAction: map[string]BanditPerActionStats{},
	}
	return coldStartStore{model: model}
}

type coldStartStore struct {
	model *BanditModel
}

func (s coldStartStore) GetModel(string) *BanditModel { return s.model }

// UpdateModel is a no-op: the cold-start model is derived, not stored.
func (s coldStartStore) UpdateModel(string, *BanditModel) {}

const maxBucket = 10000
const hashRange = float64(0x100000000) // 2^32

// sortedActions returns a copy of actions sorted alphabetically by id.
func sortedActions(actions []BanditAction) []BanditAction {
	cp := make([]BanditAction, len(actions))
	copy(cp, actions)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].ID < cp[j].ID
	})
	return cp
}

// statsFor returns the stats for actionID, defaulting to cold start: zero mean,
// zero samples, and variance 1 so Thompson sampling still has a defined scale
// on an unseen action.
func statsFor(perAction map[string]BanditPerActionStats, actionID string) BanditPerActionStats {
	if s, ok := perAction[actionID]; ok {
		return s
	}
	return BanditPerActionStats{Mean: 0, Variance: 1, Samples: 0}
}

// uniformFromSeed derives a uniform sample in (0,1] from a deterministic seed.
// The 1 in the numerator keeps it strictly above zero, because Box-Muller
// takes log(u1) and log(0) is negative infinity.
func uniformFromSeed(seed uint32, salt int) float64 {
	hashed := MurmurHash3(uint32ToStr(seed)+":"+intToStr(salt), 0)
	return (float64(hashed) + 1) / (hashRange + 1)
}

// SeededGaussian produces one standard-normal sample deterministically from
// seed, via Box-Muller over two seeded uniforms. Exported so tests can assert
// the seeded-random pipeline directly instead of inferring it from decisions.
func SeededGaussian(seed uint32) float64 {
	u1 := uniformFromSeed(seed, 1)
	u2 := uniformFromSeed(seed, 2)
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// intToStr renders an int without pulling fmt into the hot path.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}

// uint32ToStr renders a uint32 without pulling fmt into the hot path.
func uint32ToStr(n uint32) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// BanditEvaluationResult is the output of EvaluateBandit.
type BanditEvaluationResult struct {
	// Action is the selected arm.
	Action BanditAction
	// Entry is the structured decision record for the decision log.
	Entry BanditDecisionEntry
}

// EvaluateBandit selects a bandit action for the given context and bandit key.
//
// Returns (nil, nil) when the store has no model for this bandit, which the
// evaluator reports as a named skip. Returns an error when the config itself
// cannot be served (empty action set, unknown algorithm) so the evaluator can
// say which of the two happened instead of blaming a missing model.
func EvaluateBandit(
	banditKey string,
	config BanditConfig,
	ctx EvalContext,
	store BanditModelStore,
) (*BanditEvaluationResult, error) {
	model := store.GetModel(banditKey)
	if model == nil {
		return nil, nil
	}

	// Contextual lookup: when the model declares per-bucket maps and the
	// request's bucket is one of them, use it. Otherwise the top-level map.
	bucketKey := deriveContextBucket(config, ctx)
	perActionMap := model.PerAction
	if bucketKey != "" && model.ContextualModels != nil {
		if cm, ok := model.ContextualModels[bucketKey]; ok {
			perActionMap = cm
		}
	}

	// The request bucket is seeded from the bandit key and the context's
	// bucketing key, matching the reference exactly. It used to be seeded
	// from a context SUMMARY string ("user:u_1"), which made every Go bandit
	// decision differ from every other language's for the same visitor.
	hashBucket := MurmurHash3(banditKey+":"+BucketingKey(ctx, "user"), 0) % maxBucket

	var action BanditAction
	var actionProbability float64
	var optimalityGap *float64

	switch config.Algorithm {
	case "epsilon-greedy":
		result, err := EpsilonGreedy(perActionMap, config.Actions, hashBucket, config.ExplorationRate)
		if err != nil {
			return nil, err
		}
		action = result.Action
		actionProbability = result.ActionProbability
		if result.ExploreFired {
			epsilon := 0.1
			if config.ExplorationRate != nil {
				epsilon = *config.ExplorationRate
			}
			gap := epsilon
			optimalityGap = &gap
		}

	case "thompson-sampling":
		result, err := ThompsonSample(perActionMap, config.Actions, hashBucket)
		if err != nil {
			return nil, err
		}
		action = result.Action
		actionProbability = result.ActionProbability

	case "ucb1":
		result, err := UCB1(perActionMap, config.Actions)
		if err != nil {
			return nil, err
		}
		action = result.Action
		actionProbability = result.ActionProbability

	default:
		return nil, banditError("unknown bandit algorithm " + quoteForError(config.Algorithm) +
			". Known algorithms: epsilon-greedy, thompson-sampling, ucb1")
	}

	return &BanditEvaluationResult{
		Action: action,
		Entry: BanditDecisionEntry{
			FlagKey:           banditKey,
			BanditKey:         banditKey,
			ActionID:          action.ID,
			ModelVersion:      model.Version,
			ActionProbability: actionProbability,
			OptimalityGap:     optimalityGap,
			Source:            SourceBandit,
			RuleType:          RuleTypeBandit,
			Reasons: []string{
				"bandit " + quoteForError(banditKey) + " served action " + quoteForError(action.ID),
			},
		},
	}, nil
}

// deriveContextBucket computes the contextual-model lookup key by joining the
// declared contextAttributes as key=value pairs, keys sorted alphabetically.
// Returns "" when no action declares any, meaning "no contextual lookup".
func deriveContextBucket(config BanditConfig, ctx EvalContext) string {
	keys := map[string]struct{}{}
	for _, action := range config.Actions {
		for k := range action.ContextAttributes {
			keys[k] = struct{}{}
		}
	}
	if len(keys) == 0 {
		return ""
	}

	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	parts := make([]string, 0, len(sorted))
	for _, key := range sorted {
		parts = append(parts, key+"="+resolveContextAttr(ctx, key))
	}
	return strings.Join(parts, ";")
}

// resolveContextAttr resolves a dotted attribute path for contextual-bucket
// lookup, returning "" when it cannot be resolved.
func resolveContextAttr(ctx EvalContext, key string) string {
	raw := ResolveDottedAttribute(ctx, key)
	if raw == nil {
		return ""
	}
	return RawToString(raw)
}

// banditError keeps fmt out of the hot path.
type banditError string

func (e banditError) Error() string { return string(e) }

func errEmptyActions(fn string) error {
	return banditError(fn + ": actions list is empty")
}

// quoteForError wraps a value in double quotes for a message, without pulling
// in fmt or strconv on this path.
func quoteForError(s string) string {
	return `"` + s + `"`
}
