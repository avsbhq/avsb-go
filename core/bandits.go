package core

import (
	"math"
	"sort"
	"strings"
)

// BanditModelStore is the interface that bandit model backends must satisfy.
type BanditModelStore interface {
	// GetModel returns the current model for the named bandit, or nil when
	// no model has been loaded yet (cold start).
	GetModel(banditKey string) *BanditModel
	// UpdateModel persists or updates the model. Fire-and-forget; callers
	// are free to batch or persist asynchronously.
	UpdateModel(banditKey string, model *BanditModel)
}

// InMemoryBanditModelStore is a thread-unsafe in-memory BanditModelStore
// suitable for tests and single-goroutine usage.
type InMemoryBanditModelStore struct {
	models map[string]*BanditModel
}

// NewInMemoryBanditModelStore creates a new store optionally seeded with
// initial models.
func NewInMemoryBanditModelStore(initial map[string]*BanditModel) *InMemoryBanditModelStore {
	s := &InMemoryBanditModelStore{models: make(map[string]*BanditModel)}
	for k, m := range initial {
		s.models[k] = m
	}
	return s
}

// GetModel returns the current model or nil.
func (s *InMemoryBanditModelStore) GetModel(banditKey string) *BanditModel {
	return s.models[banditKey]
}

// UpdateModel stores a model.
func (s *InMemoryBanditModelStore) UpdateModel(banditKey string, model *BanditModel) {
	s.models[banditKey] = model
}

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

// statsFor returns the stats for the given actionId, defaulting to cold-start
// (zero mean, zero samples, variance=1 so Thompson sampling has a defined
// scale on unseen actions).
func statsFor(perAction map[string]BanditPerActionStats, actionID string) BanditPerActionStats {
	if s, ok := perAction[actionID]; ok {
		return s
	}
	return BanditPerActionStats{Mean: 0, Variance: 1, Samples: 0}
}

// uniformFromSeed derives a uniform-in-(0,1] sample from a deterministic seed.
// Mirrors the TypeScript uniformFromSeed implementation.
func uniformFromSeed(seed uint32, salt int) float64 {
	// hashStr is the combined string feed into MurmurHash3.
	hashStr := strings.Join([]string{uint32ToStr(seed), ":", intToStr(salt)}, "")
	hashed := MurmurHash3(hashStr, 0)
	// Add 1 so result is in (0,1] — Box-Muller requires u1 > 0 (log(0) is -∞).
	return (float64(hashed) + 1) / (hashRange + 1)
}

// SeededGaussian produces a single standard-normal sample deterministically
// from seed. Uses the Box-Muller transform with two seeded uniform samples.
// Exported so tests can assert the seeded-random pipeline directly.
func SeededGaussian(seed uint32) float64 {
	u1 := uniformFromSeed(seed, 1)
	u2 := uniformFromSeed(seed, 2)
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// intToStr converts an int to its string representation without fmt.
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
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}

// uint32ToStr converts a uint32 to its string representation.
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

// EpsilonGreedyResult holds the result of an epsilon-greedy selection.
type EpsilonGreedyResult struct {
	Action            BanditAction
	ActionProbability float64
	ExploreFired      bool
}

// EpsilonGreedy selects an action using the epsilon-greedy algorithm.
// hashBucket is the pre-computed MurmurHash3 bucket (0..9999).
func EpsilonGreedy(
	perAction map[string]BanditPerActionStats,
	actions []BanditAction,
	hashBucket uint32,
	explorationRate *float64,
) (EpsilonGreedyResult, error) {
	if len(actions) == 0 {
		return EpsilonGreedyResult{}, errEmptyActions("epsilonGreedy")
	}

	sorted := sortedActions(actions)
	epsilon := 0.1
	if explorationRate != nil {
		epsilon = *explorationRate
	}
	n := len(sorted)
	bucketFraction := float64(hashBucket) / maxBucket
	exploreFired := bucketFraction < epsilon

	if exploreFired {
		idx := int(hashBucket) % n
		action := sorted[idx]
		return EpsilonGreedyResult{
			Action:            action,
			ActionProbability: epsilon / float64(n),
			ExploreFired:      true,
		}, nil
	}

	// Exploit branch — pick the highest mean, alphabetical tie-break on id.
	var bestID string
	bestMean := math.Inf(-1)
	bestCount := 0
	for _, action := range sorted {
		stats := statsFor(perAction, action.ID)
		if stats.Mean > bestMean {
			bestMean = stats.Mean
			bestID = action.ID
			bestCount = 1
		} else if stats.Mean == bestMean {
			bestCount++
		}
	}

	var bestAction BanditAction
	for _, a := range sorted {
		if a.ID == bestID {
			bestAction = a
			break
		}
	}

	exploitMass := (1 - epsilon) / float64(bestCount)
	return EpsilonGreedyResult{
		Action:            bestAction,
		ActionProbability: exploitMass + epsilon/float64(n),
		ExploreFired:      false,
	}, nil
}

// ThompsonSampleResult holds the result of a Thompson sampling selection.
type ThompsonSampleResult struct {
	Action            BanditAction
	ActionProbability float64
}

// ThompsonSample selects an action using Thompson sampling with seeded
// pseudo-randomness derived from hashBucket.
func ThompsonSample(
	perAction map[string]BanditPerActionStats,
	actions []BanditAction,
	hashBucket uint32,
) (ThompsonSampleResult, error) {
	if len(actions) == 0 {
		return ThompsonSampleResult{}, errEmptyActions("thompsonSample")
	}

	sorted := sortedActions(actions)

	bestID := sorted[0].ID
	bestSample := math.Inf(-1)

	for _, action := range sorted {
		stats := statsFor(perAction, action.ID)
		// Seed each action independently from the request bucket.
		seedStr := uint32ToStr(hashBucket) + ":" + action.ID
		seed := MurmurHash3(seedStr, 0)
		z := SeededGaussian(seed)
		samples := stats.Samples
		if samples < 1 {
			samples = 1
		}
		stddev := math.Sqrt(stats.Variance / float64(samples))
		sample := stats.Mean + z*stddev
		if sample > bestSample {
			bestSample = sample
			bestID = action.ID
		}
	}

	var bestAction BanditAction
	for _, a := range sorted {
		if a.ID == bestID {
			bestAction = a
			break
		}
	}

	return ThompsonSampleResult{
		Action: bestAction,
		// 1/N placeholder — true posterior selection probability requires
		// integration over the joint posterior (computed offline).
		ActionProbability: 1.0 / float64(len(sorted)),
	}, nil
}

// UCB1Result holds the result of a UCB1 selection.
type UCB1Result struct {
	Action            BanditAction
	ActionProbability float64
}

// UCB1 selects an action using the Upper Confidence Bound 1 algorithm.
func UCB1(
	perAction map[string]BanditPerActionStats,
	actions []BanditAction,
) (UCB1Result, error) {
	if len(actions) == 0 {
		return UCB1Result{}, errEmptyActions("ucb1")
	}

	sorted := sortedActions(actions)

	// Cold-start policy — sample every action at least once.
	for _, action := range sorted {
		stats := statsFor(perAction, action.ID)
		if stats.Samples == 0 {
			return UCB1Result{Action: action, ActionProbability: 1.0}, nil
		}
	}

	totalSamples := 0
	for _, action := range sorted {
		totalSamples += statsFor(perAction, action.ID).Samples
	}
	lnTotal := math.Log(math.Max(float64(totalSamples), 1))

	bestID := sorted[0].ID
	bestScore := math.Inf(-1)
	for _, action := range sorted {
		stats := statsFor(perAction, action.ID)
		score := stats.Mean + math.Sqrt(2*lnTotal/math.Max(float64(stats.Samples), 1))
		if score > bestScore {
			bestScore = score
			bestID = action.ID
		}
	}

	var bestAction BanditAction
	for _, a := range sorted {
		if a.ID == bestID {
			bestAction = a
			break
		}
	}

	return UCB1Result{Action: bestAction, ActionProbability: 1.0}, nil
}

// BanditEvaluationResult is the output of EvaluateBandit.
type BanditEvaluationResult struct {
	Action BanditAction
	Entry  BanditDecisionEntry
}

// EvaluateBandit is the top-level entry point that selects a bandit action
// for the given context and bandit key.
// Returns nil when no model is available (cold start — no model loaded yet).
func EvaluateBandit(
	banditKey string,
	config BanditConfig,
	ctx EvalContext,
	store BanditModelStore,
) *BanditEvaluationResult {
	model := store.GetModel(banditKey)
	if model == nil {
		return nil
	}

	// Contextual lookup — when the model declares per-bucket maps and the
	// request's bucket exists, use that. Otherwise fall back to top-level.
	bucketKey := deriveContextBucket(config, ctx)
	perActionMap := model.PerAction
	if bucketKey != "" && model.ContextualModels != nil {
		if cm, ok := model.ContextualModels[bucketKey]; ok {
			perActionMap = cm
		}
	}

	hashBucket := MurmurHash3(banditKey+":"+ContextSummary(ctx), 0) % maxBucket

	var action BanditAction
	var actionProbability float64
	var optimalityGap *float64

	switch config.Algorithm {
	case "epsilon-greedy":
		result, err := EpsilonGreedy(perActionMap, config.Actions, hashBucket, config.ExplorationRate)
		if err != nil {
			return nil
		}
		action = result.Action
		actionProbability = result.ActionProbability
		if result.ExploreFired {
			epsilon := 0.1
			if config.ExplorationRate != nil {
				epsilon = *config.ExplorationRate
			}
			g := epsilon
			optimalityGap = &g
		}

	case "thompson-sampling":
		result, err := ThompsonSample(perActionMap, config.Actions, hashBucket)
		if err != nil {
			return nil
		}
		action = result.Action
		actionProbability = result.ActionProbability

	case "ucb1":
		result, err := UCB1(perActionMap, config.Actions)
		if err != nil {
			return nil
		}
		action = result.Action
		actionProbability = result.ActionProbability

	default:
		return nil
	}

	entry := BanditDecisionEntry{
		FlagKey:           banditKey,
		BanditKey:         banditKey,
		ActionID:          action.ID,
		ModelVersion:      model.Version,
		ActionProbability: actionProbability,
		OptimalityGap:     optimalityGap,
		Source:            "bandit",
		RuleType:          "bandit",
		Reasons:           []string{"bandit \"" + banditKey + "\" served action \"" + action.ID + "\""},
	}

	return &BanditEvaluationResult{Action: action, Entry: entry}
}

// deriveContextBucket computes the context-bucket key for contextual model
// lookup. Returns an empty string when no contextAttributes are declared.
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
		val := resolveContextAttr(ctx, key)
		parts = append(parts, key+"="+val)
	}
	return strings.Join(parts, ";")
}

// resolveContextAttr resolves a dotted attribute path from the context for
// contextual-bucket lookup.
func resolveContextAttr(ctx EvalContext, key string) string {
	raw := ResolveDottedAttribute(ctx, key)
	if raw == nil {
		return ""
	}
	return RawToString(raw)
}

// banditError is a simple error type for this package to avoid importing fmt
// in hot-path code. Non-critical path can still use fmt.Errorf.
type banditError string

func (e banditError) Error() string { return string(e) }

func errEmptyActions(fn string) error {
	return banditError(fn + ": actions list is empty")
}
