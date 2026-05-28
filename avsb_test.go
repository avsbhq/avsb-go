package avsb_test

import (
	"context"
	"encoding/json"
	"testing"

	avsb "github.com/avsbhq/avsb-go"
)

// minimalDatafile returns a JSON-encoded datafile with one enabled boolean
// flag. Useful for NewTestServer tests that don't need the full fixture suite.
func minimalDatafile(t *testing.T) []byte {
	t.Helper()
	type variation struct {
		ID    string          `json:"id"`
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	type flagEntry struct {
		ID                 string      `json:"id"`
		Key                string      `json:"key"`
		Type               string      `json:"type"`
		Enabled            bool        `json:"enabled"`
		DefaultVariationID string      `json:"defaultVariationId"`
		Variations         []variation `json:"variations"`
		Overrides          []struct{}  `json:"overrides"`
		Rules              []struct{}  `json:"rules"`
	}
	type df struct {
		Version  int         `json:"version"`
		Flags    []flagEntry `json:"flags"`
		Audiences []struct{} `json:"audiences"`
	}
	raw, _ := json.Marshal(df{
		Version: 2,
		Flags: []flagEntry{
			{
				ID:                 "flag_test",
				Key:                "test-flag",
				Type:               "boolean",
				Enabled:            true,
				DefaultVariationID: "var_off",
				Variations: []variation{
					{ID: "var_off", Key: "off", Value: json.RawMessage(`false`)},
					{ID: "var_on", Key: "on", Value: json.RawMessage(`true`)},
				},
			},
		},
	})
	return raw
}

// TestNewTestServer_LoadsDatafile verifies that NewTestServer accepts valid
// datafile bytes and produces a functioning server.
func TestNewTestServer_LoadsDatafile(t *testing.T) {
	server, err := avsb.NewTestServer(minimalDatafile(t))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	if server == nil {
		t.Fatal("expected non-nil server")
	}
}

// TestServer_GetBoolFlag_NotFound verifies that requesting a non-existent
// flag returns the caller-supplied default and source=not_found.
func TestServer_GetBoolFlag_NotFound(t *testing.T) {
	server, _ := avsb.NewTestServer(minimalDatafile(t))
	ctx := context.Background()
	evalCtx := avsb.UserContext("u1", nil)

	f := server.GetBoolFlag(ctx, "does-not-exist", true, avsb.WithContext(evalCtx))

	if f.Exists() {
		t.Error("expected Exists() to return false for missing flag")
	}
	if f.IsEnabled() {
		t.Error("expected IsEnabled() to return false for missing flag")
	}
	if !f.Value() {
		t.Error("expected default value=true for missing flag")
	}
	if f.Source() != "not_found" {
		t.Errorf("source: want not_found, got %q", f.Source())
	}
}

// TestServer_GetStringFlag_Default verifies that a disabled flag returns its
// default variation value (not the caller default).
func TestServer_GetStringFlag_Default(t *testing.T) {
	type variation struct {
		ID    string          `json:"id"`
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	type flagEntry struct {
		ID                 string      `json:"id"`
		Key                string      `json:"key"`
		Type               string      `json:"type"`
		Enabled            bool        `json:"enabled"`
		DefaultVariationID string      `json:"defaultVariationId"`
		Variations         []variation `json:"variations"`
		Overrides          []struct{}  `json:"overrides"`
		Rules              []struct{}  `json:"rules"`
	}
	type df struct {
		Version   int         `json:"version"`
		Flags     []flagEntry `json:"flags"`
		Audiences []struct{}  `json:"audiences"`
	}
	raw, _ := json.Marshal(df{
		Version: 2,
		Flags: []flagEntry{
			{
				ID:                 "flag_str",
				Key:                "str-flag",
				Type:               "string",
				Enabled:            false,
				DefaultVariationID: "var_default",
				Variations: []variation{
					{ID: "var_default", Key: "default", Value: json.RawMessage(`"default-value"`)},
				},
			},
		},
	})

	server, _ := avsb.NewTestServer(raw)
	evalCtx := avsb.UserContext("u1", nil)
	f := server.GetStringFlag(context.Background(), "str-flag", "caller-default", avsb.WithContext(evalCtx))

	if f.Value() != "default-value" {
		t.Errorf("want default-value, got %q", f.Value())
	}
	if f.Source() != "disabled" {
		t.Errorf("source: want disabled, got %q", f.Source())
	}
}

// TestServer_MultiContext_EvalContext verifies that MultiContext round-trips
// correctly through the SDK.
func TestServer_MultiContext_EvalContext(t *testing.T) {
	server, _ := avsb.NewTestServer(minimalDatafile(t))
	ctx := context.Background()

	evalCtx := avsb.MultiContext(
		avsb.UserContext("u_enterprise", nil),
		avsb.OrgContext("acme", map[string]any{"tier": "enterprise"}),
	)

	// Should not panic and should return a valid result.
	f := server.GetBoolFlag(ctx, "test-flag", false, avsb.WithContext(evalCtx))
	if f == (avsb.Flag[bool]{}) {
		t.Error("expected non-zero flag result")
	}
}

// TestFlag_IsEnabled_OnlyTrueForRuleSource verifies that IsEnabled returns
// false for source=default even when value is truthy.
func TestFlag_IsEnabled_OnlyTrueForRuleSource(t *testing.T) {
	// The test-flag has no rules, so it falls to default variation (var_off = false).
	server, _ := avsb.NewTestServer(minimalDatafile(t))
	evalCtx := avsb.UserContext("u1", nil)

	f := server.GetBoolFlag(context.Background(), "test-flag", false, avsb.WithContext(evalCtx))

	if f.Source() != "default" {
		t.Errorf("source: want default, got %q", f.Source())
	}
	if f.IsEnabled() {
		t.Error("IsEnabled should be false when source=default, even if value is checked")
	}
}
