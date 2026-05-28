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
package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/avsbhq/avsb-go/core"
)

// manifestCase mirrors one entry in manifest.json.
type manifestCase struct {
	Case        string `json:"case"`
	Description string `json:"description"`
	Expected    string `json:"expected"`
}

type manifest struct {
	Cases []manifestCase `json:"cases"`
}

// expectedFile is the full structure stored in each expected/*.json file.
type expectedFile struct {
	Case        string        `json:"case"`
	Datafile    string        `json:"datafile"`
	Context     string        `json:"context"`
	FlagKey     string        `json:"flagKey"`
	DefaultValue json.RawMessage `json:"defaultValue"`
	Expected    expectedShape `json:"expected"`
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

	// Load manifest.
	manifestBytes, err := os.ReadFile(filepath.Join(fixturesDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	for _, tc := range m.Cases {
		tc := tc // capture loop variable
		t.Run(tc.Case, func(t *testing.T) {
			// Load expected file.
			expectedPath := filepath.Join(fixturesDir, tc.Expected)
			expectedBytes, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("read expected: %v", err)
			}
			var ef expectedFile
			if err := json.Unmarshal(expectedBytes, &ef); err != nil {
				t.Fatalf("parse expected file: %v", err)
			}

			// Load datafile.
			dfPath := filepath.Join(fixturesDir, ef.Datafile)
			dfBytes, err := os.ReadFile(dfPath)
			if err != nil {
				t.Fatalf("read datafile: %v", err)
			}
			var df core.Datafile
			if err := json.Unmarshal(dfBytes, &df); err != nil {
				t.Fatalf("parse datafile: %v", err)
			}

			// Load context.
			ctxPath := filepath.Join(fixturesDir, ef.Context)
			ctxBytes, err := os.ReadFile(ctxPath)
			if err != nil {
				t.Fatalf("read context: %v", err)
			}
			var evalCtx core.EvalContext
			if err := json.Unmarshal(ctxBytes, &evalCtx); err != nil {
				t.Fatalf("parse context: %v", err)
			}

			// Evaluate.
			result := core.EvaluateFlag(
				ef.FlagKey,
				ef.DefaultValue,
				evalCtx,
				&df,
				core.EvaluateOptions{IncludeReasons: true},
			)

			// Assert value.
			assertJSONEqual(t, "value", ef.Expected.Value, result.Value)

			// Assert variationKey.
			assertStringPtrEqual(t, "variationKey", ef.Expected.VariationKey, result.VariationKey)

			// Assert source.
			if ef.Expected.Source != nil {
				if result.Source != *ef.Expected.Source {
					t.Errorf("source: want %q, got %q", *ef.Expected.Source, result.Source)
				}
			}

			// Assert ruleId.
			assertStringPtrEqual(t, "ruleId", ef.Expected.RuleID, result.RuleID)

			// Assert ruleType.
			assertStringPtrEqual(t, "ruleType", ef.Expected.RuleType, result.RuleType)

			// Assert reasons (each expected reason must appear in result.Reasons).
			for _, want := range ef.Expected.Reasons {
				found := false
				for _, got := range result.Reasons {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("reasons: expected %q to be present in %v", want, result.Reasons)
				}
			}
		})
	}
}

// resolveFixturesDir walks up from the test file until it finds
// packages/avsb-conformance/fixtures/.
func resolveFixturesDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// filename = .../packages/avsb-go/conformance/conformance_test.go
	// go up to packages/avsb-go then ../avsb-conformance/fixtures
	dir := filepath.Dir(filename)
	fixturesDir := filepath.Join(dir, "..", "..", "avsb-conformance", "fixtures")
	if _, err := os.Stat(fixturesDir); err != nil {
		t.Fatalf("fixtures directory not found at %s: %v", fixturesDir, err)
	}
	return fixturesDir
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
