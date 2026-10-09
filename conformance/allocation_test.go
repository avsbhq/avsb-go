package conformance_test

// The conformance allocation family: targeted delivery rollouts (N1-1).
//
// Six generated fixtures, one rollout rule each (5, 30 and 50 percent, a
// fractional 0.12345 boundary, 100 percent and no allocation), each with 200
// contexts whose outcomes the TypeScript reference recorded. Every one of the
// 1,200 evaluations is replayed, reasons included, so a visitor is inside or
// outside a rollout identically here and in every other SDK.
//
// FAIL-HARD RULE (conformance README, "The allocation family"): a missing
// manifest, an empty or miscounted fixture list, a missing file, a case that
// differs from its manifest entry, a fixture with the wrong number of
// evaluations, or a JSON file the manifest does not name all fail the run.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/avsbhq/avsb-go/core"
)

type allocationManifest struct {
	FixtureCount       int                       `json:"fixtureCount"`
	ContextsPerFixture int                       `json:"contextsPerFixture"`
	Fixtures           []allocationManifestEntry `json:"fixtures"`
}

type allocationManifestEntry struct {
	Case string `json:"case"`
	File string `json:"file"`
}

type allocationProbe struct {
	Bucket      int    `json:"bucket"`
	ResolvedKey string `json:"resolvedKey"`
}

type allocationEvaluation struct {
	Context  json.RawMessage `json:"context"`
	Expected expectedShape   `json:"expected"`
}

type allocationFixture struct {
	Case         string                 `json:"case"`
	FlagKey      string                 `json:"flagKey"`
	DefaultValue json.RawMessage        `json:"defaultValue"`
	Probes       []allocationProbe      `json:"probes"`
	Datafile     json.RawMessage        `json:"datafile"`
	Evaluations  []allocationEvaluation `json:"evaluations"`
}

const allocationRegenerateHint = "run `npm run generate` then `npm run vendor` in packages/avsb-conformance"

// loadAllocationFamily resolves fixtures/allocation the same way the
// single-context corpus is resolved (monorepo first, vendored copy second),
// and enforces every fail-hard rule before a single evaluation runs.
func loadAllocationFamily(t *testing.T) (string, allocationManifest, []allocationFixture) {
	t.Helper()
	dir := filepath.Join(resolveFixturesDir(t), "allocation")

	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read allocation manifest in %s: %v (%s)", dir, err, allocationRegenerateHint)
	}
	var m allocationManifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("parse allocation manifest: %v", err)
	}
	if len(m.Fixtures) == 0 {
		t.Fatalf("allocation manifest in %s lists no fixtures; an empty family guarantees nothing", dir)
	}
	if m.FixtureCount != len(m.Fixtures) {
		t.Fatalf("allocation manifest says fixtureCount=%d but lists %d fixtures", m.FixtureCount, len(m.Fixtures))
	}
	if m.ContextsPerFixture <= 0 {
		t.Fatalf("allocation manifest declares no contextsPerFixture")
	}

	fixtures := make([]allocationFixture, 0, len(m.Fixtures))
	for _, entry := range m.Fixtures {
		raw, err := os.ReadFile(filepath.Join(dir, entry.File))
		if err != nil {
			t.Fatalf("read allocation fixture %s: %v (%s)", entry.File, err, allocationRegenerateHint)
		}
		var f allocationFixture
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("parse allocation fixture %s: %v", entry.File, err)
		}
		if f.Case != entry.Case {
			t.Fatalf("allocation fixture %s declares case %q but the manifest names it %q", entry.File, f.Case, entry.Case)
		}
		if len(f.Evaluations) != m.ContextsPerFixture {
			t.Fatalf("allocation fixture %s carries %d evaluations, want %d", entry.File, len(f.Evaluations), m.ContextsPerFixture)
		}
		fixtures = append(fixtures, f)
	}
	return dir, m, fixtures
}

// replayAllocationFixture evaluates every recorded context and returns how
// many ran and a description of each one that drifted from the reference.
func replayAllocationFixture(t *testing.T, f allocationFixture) (int, []string) {
	t.Helper()
	var df core.Datafile
	if err := json.Unmarshal(f.Datafile, &df); err != nil {
		t.Fatalf("%s: parse inline datafile: %v", f.Case, err)
	}

	ran := 0
	var drift []string
	for _, e := range f.Evaluations {
		var ctx core.EvalContext
		if err := json.Unmarshal(e.Context, &ctx); err != nil {
			t.Fatalf("%s: parse context %s: %v", f.Case, e.Context, err)
		}
		// Reasons are part of the contract, so the harness always collects them.
		got := core.EvaluateFlag(f.FlagKey, f.DefaultValue, ctx, &df, core.EvaluateOptions{IncludeReasons: true})
		ran++
		if problem := allocationMismatch(e.Expected, got); problem != "" {
			drift = append(drift, fmt.Sprintf("%s: %s", e.Context, problem))
		}
	}
	return ran, drift
}

// allocationMismatch compares all six contract fields and names the first
// that differs, or returns "" when the evaluation matches the reference.
func allocationMismatch(want expectedShape, got core.FlagEvaluation) string {
	if !sameJSONValue(want.Value, got.Value) {
		return fmt.Sprintf("value %s, want %s", got.Value, want.Value)
	}
	if !sameStringPtr(want.VariationKey, got.VariationKey) {
		return fmt.Sprintf("variationKey %s, want %s", describePtr(got.VariationKey), describePtr(want.VariationKey))
	}
	if want.Source == nil || *want.Source != got.Source {
		return fmt.Sprintf("source %q, want %s", got.Source, describePtr(want.Source))
	}
	if !sameStringPtr(want.RuleID, got.RuleID) {
		return fmt.Sprintf("ruleId %s, want %s", describePtr(got.RuleID), describePtr(want.RuleID))
	}
	if !sameStringPtr(want.RuleType, got.RuleType) {
		return fmt.Sprintf("ruleType %s, want %s", describePtr(got.RuleType), describePtr(want.RuleType))
	}
	if len(want.Reasons) != len(got.Reasons) {
		return fmt.Sprintf("reasons %q, want %q", got.Reasons, want.Reasons)
	}
	for i := range want.Reasons {
		if want.Reasons[i] != got.Reasons[i] {
			return fmt.Sprintf("reasons %q, want %q", got.Reasons, want.Reasons)
		}
	}
	return ""
}

func sameJSONValue(want, got json.RawMessage) bool {
	var wantVal, gotVal interface{}
	if json.Unmarshal(want, &wantVal) != nil || json.Unmarshal(got, &gotVal) != nil {
		return false
	}
	wantJSON, _ := json.Marshal(wantVal)
	gotJSON, _ := json.Marshal(gotVal)
	return string(wantJSON) == string(gotJSON)
}

func sameStringPtr(want, got *string) bool {
	if want == nil || got == nil {
		return want == nil && got == nil
	}
	return *want == *got
}

func describePtr(s *string) string {
	if s == nil {
		return "null"
	}
	return fmt.Sprintf("%q", *s)
}

func TestAllocationFamily(t *testing.T) {
	_, m, fixtures := loadAllocationFamily(t)

	total := 0
	for _, f := range fixtures {
		f := f
		t.Run(f.Case, func(t *testing.T) {
			ran, drift := replayAllocationFixture(t, f)
			total += ran
			if ran != m.ContextsPerFixture {
				t.Fatalf("ran %d evaluations, want %d", ran, m.ContextsPerFixture)
			}
			if len(drift) > 0 {
				t.Fatalf("%d of %d contexts drifted from the reference; first: %s", len(drift), ran, drift[0])
			}
		})
	}

	// Nothing can pass by running nothing.
	if want := m.FixtureCount * m.ContextsPerFixture; total != want || total != 1200 {
		t.Fatalf("ran %d allocation evaluations, want %d (1,200 today)", total, want)
	}
}

func TestAllocationFamilyCarriesTheSixRolloutLevels(t *testing.T) {
	_, m, _ := loadAllocationFamily(t)
	want := []string{
		"targeted_delivery_allocation_0.05",
		"targeted_delivery_allocation_0.3",
		"targeted_delivery_allocation_0.5",
		"targeted_delivery_allocation_fractional_boundary",
		"targeted_delivery_allocation_1.0",
		"targeted_delivery_allocation_null",
	}
	if len(m.Fixtures) != len(want) {
		t.Fatalf("allocation manifest lists %d fixtures, want %d", len(m.Fixtures), len(want))
	}
	for i, entry := range m.Fixtures {
		if entry.Case != want[i] {
			t.Errorf("fixture %d is %q, want %q", i, entry.Case, want[i])
		}
	}
}

func TestAllocationFamilyHasNoOrphans(t *testing.T) {
	dir, m, _ := loadAllocationFamily(t)
	named := make(map[string]bool, len(m.Fixtures))
	for _, entry := range m.Fixtures {
		named[entry.File] = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read allocation dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" || name == "manifest.json" {
			continue
		}
		if !named[name] {
			t.Errorf("allocation/%s is not named by the manifest", name)
		}
	}
}

func TestAllocationFractionalProbesComeFirst(t *testing.T) {
	_, _, fixtures := loadAllocationFamily(t)
	for _, f := range fixtures {
		if f.Case != "targeted_delivery_allocation_fractional_boundary" {
			continue
		}
		if len(f.Probes) != 2 || f.Probes[0].Bucket != 1234 || f.Probes[1].Bucket != 1235 {
			t.Fatalf("probes %+v, want buckets 1234 then 1235", f.Probes)
		}
		for i, probe := range f.Probes {
			var ctx struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(f.Evaluations[i].Context, &ctx); err != nil {
				t.Fatalf("parse probe context: %v", err)
			}
			if ctx.Key != probe.ResolvedKey {
				t.Errorf("evaluation %d is %q, want the probe key %q", i, ctx.Key, probe.ResolvedKey)
			}
		}
		// Bucket 1234 is inside the 1234.5 threshold; a port that floors it says no.
		if s := f.Evaluations[0].Expected.Source; s == nil || *s != "rule" {
			t.Errorf("bucket 1234 source %s, want rule", describePtr(s))
		}
		if s := f.Evaluations[1].Expected.Source; s == nil || *s != "default" {
			t.Errorf("bucket 1235 source %s, want default", describePtr(s))
		}
		return
	}
	t.Fatal("the fractional boundary fixture is missing")
}
