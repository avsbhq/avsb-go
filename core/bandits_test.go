package core

import (
	"math"
	"testing"
)

func makeActions(ids ...string) []BanditAction {
	out := make([]BanditAction, len(ids))
	for i, id := range ids {
		out[i] = BanditAction{ID: id, VariationID: id + "_var"}
	}
	return out
}

// TestEpsilonGreedy_ExploitsBestArm verifies that when one arm has a clearly
// higher mean and the hash bucket is above the exploration threshold, the best
// arm is selected.
func TestEpsilonGreedy_ExploitsBestArm(t *testing.T) {
	perAction := map[string]BanditPerActionStats{
		"action_a": {Mean: 0.9, Variance: 0.1, Samples: 100},
		"action_b": {Mean: 0.3, Variance: 0.1, Samples: 100},
		"action_c": {Mean: 0.1, Variance: 0.1, Samples: 100},
	}
	actions := makeActions("action_a", "action_b", "action_c")

	// Use a bucket above epsilon (0.1 * 10000 = 1000) → exploit.
	exploitBucket := uint32(5000)
	result, err := EpsilonGreedy(perAction, actions, exploitBucket, nil)
	if err != nil {
		t.Fatalf("EpsilonGreedy returned error: %v", err)
	}
	if result.Action.ID != "action_a" {
		t.Errorf("expected action_a (best mean), got %q", result.Action.ID)
	}
	if result.ExploreFired {
		t.Error("expected exploit branch, not explore")
	}
}

// TestEpsilonGreedy_EmptyActions returns an error.
func TestEpsilonGreedy_EmptyActions(t *testing.T) {
	_, err := EpsilonGreedy(nil, nil, 0, nil)
	if err == nil {
		t.Error("expected error for empty actions list")
	}
}

// TestEpsilonGreedy_ExploreBranch verifies the explore branch fires when
// the bucket falls below epsilon.
func TestEpsilonGreedy_ExploreBranch(t *testing.T) {
	actions := makeActions("z_action", "a_action")
	// bucket 0 < epsilon*10000=1000 → explore
	result, err := EpsilonGreedy(nil, actions, 0, nil)
	if err != nil {
		t.Fatalf("EpsilonGreedy error: %v", err)
	}
	if !result.ExploreFired {
		t.Error("expected explore branch for bucket=0")
	}
}

// TestThompsonSample_SelectsBestArm verifies that with a very large samples
// count and low variance, the arm with the highest mean wins.
func TestThompsonSample_SelectsBestArm(t *testing.T) {
	perAction := map[string]BanditPerActionStats{
		"a": {Mean: 0.95, Variance: 0.0001, Samples: 10000},
		"b": {Mean: 0.10, Variance: 0.0001, Samples: 10000},
	}
	actions := makeActions("a", "b")

	// Run multiple buckets and assert "a" wins most of the time.
	wins := 0
	for bucket := uint32(0); bucket < 100; bucket++ {
		result, _ := ThompsonSample(perAction, actions, bucket)
		if result.Action.ID == "a" {
			wins++
		}
	}
	if wins < 80 {
		t.Errorf("expected action_a to win most buckets, won %d/100", wins)
	}
}

// TestUCB1_ColdStart verifies that the first unseen action is selected during
// cold start.
func TestUCB1_ColdStart(t *testing.T) {
	actions := makeActions("alpha", "beta", "gamma")
	// All actions have zero samples → UCB1 picks the first alphabetically.
	result, err := UCB1(nil, actions)
	if err != nil {
		t.Fatalf("UCB1 error: %v", err)
	}
	if result.Action.ID != "alpha" {
		t.Errorf("expected 'alpha' on cold start, got %q", result.Action.ID)
	}
}

// TestSeededGaussian_Deterministic verifies the seeded normal sampler is
// deterministic and produces a finite value.
func TestSeededGaussian_Deterministic(t *testing.T) {
	g1 := SeededGaussian(42)
	g2 := SeededGaussian(42)
	if g1 != g2 {
		t.Errorf("SeededGaussian is not deterministic: %f != %f", g1, g2)
	}
	if math.IsNaN(g1) || math.IsInf(g1, 0) {
		t.Errorf("SeededGaussian(42) = %f, expected finite value", g1)
	}
}

// TestSeededGaussian_DifferentSeeds verifies that different seeds produce
// different values.
func TestSeededGaussian_DifferentSeeds(t *testing.T) {
	g1 := SeededGaussian(1)
	g2 := SeededGaussian(2)
	if g1 == g2 {
		t.Error("expected different values for different seeds")
	}
}
