// Package conformance_test verifies that the Go evaluator produces decisions
// that are semantically identical to the canonical TypeScript reference
// implementation for every fixture in packages/avsb-conformance/fixtures/.
//
// Each fixture consists of:
//   - A datafile JSON (the flag configuration)
//   - A context JSON (the EvalContext passed to the evaluator)
//   - An expected JSON with the fields the evaluator must produce
//
// The test loads the manifest, iterates every case, calls EvaluateFlag with
// reasons enabled, and asserts that value/variationKey/source/ruleId/
// ruleType/reasons all match the expected output.
//
// FAIL-HARD RULE (conformance README): a missing manifest, an empty
// manifest, or a missing per-case file fails the run loudly. Nothing here
// skips. This matters most in the published avsb-go repo, where the
// monorepo-relative fixture path does not exist and only the vendored copy
// under conformance/testdata does.
package conformance_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/avsbhq/avsb-go/core"
)

// manifestCase mirrors one entry in manifest.json.
type manifestCase struct {
	Case        string `json:"case"`
	Family      string `json:"family"`
	Description string `json:"description"`
	Expected    string `json:"expected"`
}

type manifest struct {
	CaseCount int            `json:"caseCount"`
	Cases     []manifestCase `json:"cases"`
}

// expectedFile is the full structure stored in each expected/*.json file.
type expectedFile struct {
	Case         string          `json:"case"`
	Family       string          `json:"family"`
	Datafile     string          `json:"datafile"`
	Context      string          `json:"context"`
	FlagKey      string          `json:"flagKey"`
	DefaultValue json.RawMessage `json:"defaultValue"`
	Expected     expectedShape   `json:"expected"`
}

// expectedShape contains the fields we assert against.
type expectedShape struct {
	Value        json.RawMessage `json:"value"`
	VariationKey *string         `json:"variationKey"`
	Source       *string         `json:"source"`
	RuleID       *string         `json:"ruleId"`
	RuleType     *string         `json:"ruleType"`
	Reasons      []string        `json:"reasons"`
}

func TestConformance(t *testing.T) {
	fixturesDir := resolveFixturesDir(t)

	manifestBytes, err := os.ReadFile(filepath.Join(fixturesDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest at %s: %v", fixturesDir, err)
	}
	var m manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	// An empty corpus must never look like a passing run.
	if len(m.Cases) == 0 {
		t.Fatalf("conformance manifest at %s lists no cases; an empty corpus guarantees nothing", fixturesDir)
	}
	// caseCount is optional so an older vendored copy still loads, but when
	// it is present it must match: a mismatch means a hand-edited manifest.
	if m.CaseCount != 0 && m.CaseCount != len(m.Cases) {
		t.Fatalf("conformance manifest says caseCount=%d but carries %d cases", m.CaseCount, len(m.Cases))
	}

	ran := 0
	for _, tc := range m.Cases {
		tc := tc // capture loop variable
		t.Run(tc.Case, func(t *testing.T) {
			// Load expected file. A missing file is a failure, not a skip.
			expectedPath := filepath.Join(fixturesDir, tc.Expected)
			expectedBytes, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("read expected %s: %v", expectedPath, err)
			}
			var ef expectedFile
			if err := json.Unmarshal(expectedBytes, &ef); err != nil {
				t.Fatalf("parse expected file %s: %v", expectedPath, err)
			}
			if ef.Case != tc.Case {
				t.Fatalf("fixture %s declares case %q but the manifest names it %q", tc.Expected, ef.Case, tc.Case)
			}

			// Load datafile.
			dfPath := filepath.Join(fixturesDir, ef.Datafile)
			dfBytes, err := os.ReadFile(dfPath)
			if err != nil {
				t.Fatalf("read datafile %s: %v", dfPath, err)
			}
			var df core.Datafile
			if err := json.Unmarshal(dfBytes, &df); err != nil {
				t.Fatalf("parse datafile %s: %v", dfPath, err)
			}

			// Load context.
			ctxPath := filepath.Join(fixturesDir, ef.Context)
			ctxBytes, err := os.ReadFile(ctxPath)
			if err != nil {
				t.Fatalf("read context %s: %v", ctxPath, err)
			}
			var evalCtx core.EvalContext
			if err := json.Unmarshal(ctxBytes, &evalCtx); err != nil {
				t.Fatalf("parse context %s: %v", ctxPath, err)
			}

			// Evaluate. Reasons are part of the corpus, so they are always on
			// in the harness whatever the SDK default is.
			result := core.EvaluateFlag(
				ef.FlagKey,
				ef.DefaultValue,
				evalCtx,
				&df,
				core.EvaluateOptions{IncludeReasons: true},
			)

			assertJSONEqual(t, "value", ef.Expected.Value, result.Value)
			assertStringPtrEqual(t, "variationKey", ef.Expected.VariationKey, result.VariationKey)

			if ef.Expected.Source != nil {
				if result.Source != *ef.Expected.Source {
					t.Errorf("source: want %q, got %q", *ef.Expected.Source, result.Source)
				}
			}

			assertStringPtrEqual(t, "ruleId", ef.Expected.RuleID, result.RuleID)
			assertStringPtrEqual(t, "ruleType", ef.Expected.RuleType, result.RuleType)

			// Reasons are compared EXACTLY and in order: the decision trail is
			// part of the contract, not a debug nicety (spec section 1.4).
			assertReasonsEqual(t, ef.Expected.Reasons, result.Reasons)
		})
		ran++
	}

	if ran != len(m.Cases) {
		t.Fatalf("ran %d of %d manifest cases", ran, len(m.Cases))
	}
}

// resolveFixturesDir returns the conformance fixtures directory, preferring
// the monorepo copy and falling back to the copy vendored into this package
// for the published repo. When neither exists it fails, naming both paths
// and the command that produces the vendored copy.
func resolveFixturesDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate conformance fixtures")
	}
	// filename = .../packages/avsb-go/conformance/conformance_test.go
	dir := filepath.Dir(filename)

	candidates := []string{
		// Monorepo: packages/avsb-conformance/fixtures
		filepath.Join(dir, "..", "..", "avsb-conformance", "fixtures"),
		// Published repo: vendored copy under conformance/testdata
		filepath.Join(dir, "testdata", "avsb-conformance", "fixtures"),
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(candidate, "manifest.json")); err == nil && !info.IsDir() {
			return candidate
		}
	}

	t.Fatalf(
		"conformance fixtures not found. Looked for manifest.json in:\n  %s\n  %s\n"+
			"In the monorepo, run `npm run generate` in packages/avsb-conformance. "+
			"For this repo standalone, run `npm run vendor` there and commit conformance/testdata.",
		candidates[0], candidates[1],
	)
	return ""
}

// assertJSONEqual compares two json.RawMessage values by value equality
// after normalising through json.Unmarshal.
func assertJSONEqual(t *testing.T, field string, want, got json.RawMessage) {
	t.Helper()
	var wantVal, gotVal interface{}
	if err := json.Unmarshal(want, &wantVal); err != nil {
		t.Fatalf("%s: unmarshal want: %v", field, err)
	}
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatalf("%s: unmarshal got: %v", field, err)
	}
	wantJSON, _ := json.Marshal(wantVal)
	gotJSON, _ := json.Marshal(gotVal)
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("%s: want %s, got %s", field, wantJSON, gotJSON)
	}
}

// assertStringPtrEqual compares two *string fields.
func assertStringPtrEqual(t *testing.T, field string, want, got *string) {
	t.Helper()
	if want == nil && got == nil {
		return
	}
	if want == nil {
		t.Errorf("%s: want nil, got %q", field, *got)
		return
	}
	if got == nil {
		t.Errorf("%s: want %q, got nil", field, *want)
		return
	}
	if *want != *got {
		t.Errorf("%s: want %q, got %q", field, *want, *got)
	}
}

// assertReasonsEqual compares the reason trail exactly and in order.
func assertReasonsEqual(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("reasons: want %d entries %v, got %d entries %v", len(want), want, len(got), got)
		return
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("reasons[%d]: want %q, got %q", i, want[i], got[i])
		}
	}
}

// TestConformanceFixturesHaveNoOrphans fails when the fixtures directory
// carries an expected/*.json file the manifest does not name. Those are
// renamed or deleted cases whose output lingers: they replay green against
// the reference that produced them while locking semantics nobody declares.
func TestConformanceFixturesHaveNoOrphans(t *testing.T) {
	fixturesDir := resolveFixturesDir(t)

	manifestBytes, err := os.ReadFile(filepath.Join(fixturesDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	named := make(map[string]bool, len(m.Cases))
	for _, c := range m.Cases {
		named[fmt.Sprintf("%s.json", c.Case)] = true
	}

	entries, err := os.ReadDir(filepath.Join(fixturesDir, "expected"))
	if err != nil {
		t.Fatalf("read expected dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if !named[entry.Name()] {
			t.Errorf("fixture expected/%s is not named by the manifest", entry.Name())
		}
	}
}
