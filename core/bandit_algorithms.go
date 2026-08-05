package core

import "math"

// The three bandit policies, as pure functions of (perAction, actions,
// hashBucket). They are separated from the dispatch in bandits.go so the
// arithmetic can be read, and tested, without the store and context plumbing
// around it.
//
// Ties break alphabetically on action id. That is why an untrained bandit
// serves the lowest action id in every language rather than whichever arm was
// declared first.

// EpsilonGreedyResult holds one epsilon-greedy selection.
type EpsilonGreedyResult struct {
	// Action is the selected arm.
	Action BanditAction
	// ActionProbability is the probability the policy assigned this arm.
	ActionProbability float64
	// ExploreFired is true when the explore branch chose the arm.
	ExploreFired bool
}

// EpsilonGreedy selects an action with the epsilon-greedy policy. hashBucket is
// the pre-computed MurmurHash3 bucket in 0..9999.
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
	// A declared rate of 0 means exploration off, which is a real
	// configuration and not "unset": only a nil pointer falls back to 0.1.
	epsilon := 0.1
	if explorationRate != nil {
		epsilon = *explorationRate
	}
	n := len(sorted)
	bucketFraction := float64(hashBucket) / maxBucket
	exploreFired := bucketFraction < epsilon

	if exploreFired {
		idx := int(hashBucket) % n
		return EpsilonGreedyResult{
			Action:            sorted[idx],
			ActionProbability: epsilon / float64(n),
			ExploreFired:      true,
		}, nil
	}

	// Exploit: highest mean, alphabetical tie-break on id.
	bestID := sorted[0].ID
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

	// Among bestCount tied winners each takes an equal share of the exploit
	// mass, and every arm also takes an epsilon/N share from the explore
	// branch.
	exploitMass := (1 - epsilon) / float64(bestCount)
	return EpsilonGreedyResult{
		Action:            actionByID(sorted, bestID),
		ActionProbability: exploitMass + epsilon/float64(n),
		ExploreFired:      false,
	}, nil
}

// ThompsonSampleResult holds one Thompson-sampling selection.
type ThompsonSampleResult struct {
	// Action is the selected arm.
	Action BanditAction
	// ActionProbability is 1/N: see the note in ThompsonSample.
	ActionProbability float64
}

// ThompsonSample selects an action with Thompson sampling, using seeded
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
		// Seed each arm independently from the request bucket, so the same
		// (hashBucket, actionID) pair always draws the same sample.
		seed := MurmurHash3(uint32ToStr(hashBucket)+":"+action.ID, 0)
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

	return ThompsonSampleResult{
		Action: actionByID(sorted, bestID),
		// The true posterior selection probability needs integration over the
		// joint posterior, which is not hot-path work. 1/N is a placeholder;
		// analytics that need the real number recompute it offline from the
		// model and the seed.
		ActionProbability: 1.0 / float64(len(sorted)),
	}, nil
}

// UCB1Result holds one UCB1 selection.
type UCB1Result struct {
	// Action is the selected arm.
	Action BanditAction
	// ActionProbability is 1.0: UCB1 is deterministic given a model.
	ActionProbability float64
}

// UCB1 selects an action with the Upper Confidence Bound 1 policy.
func UCB1(
	perAction map[string]BanditPerActionStats,
	actions []BanditAction,
) (UCB1Result, error) {
	if len(actions) == 0 {
		return UCB1Result{}, errEmptyActions("ucb1")
	}

	sorted := sortedActions(actions)

	// Cold start: sample every arm at least once. With several unseen arms
	// the lowest id wins, which is what makes an untrained bandit
	// deterministic across languages.
	for _, action := range sorted {
		if statsFor(perAction, action.ID).Samples == 0 {
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

	return UCB1Result{Action: actionByID(sorted, bestID), ActionProbability: 1.0}, nil
}

// actionByID returns the action with the given id from an already-sorted slice.
// The id always comes from that same slice, so the zero value is unreachable.
func actionByID(sorted []BanditAction, id string) BanditAction {
	for _, a := range sorted {
		if a.ID == id {
			return a
		}
	}
	return BanditAction{}
}
