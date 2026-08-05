package avsb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// datafileJSON builds a datafile with one always-on flag, plus whatever
// server-controlled endpoints the test needs.
func datafileJSON(collectEndpoint, heartbeatEndpoint, streamEndpoint string) string {
	return fmt.Sprintf(`{
	  "version": 2,
	  "projectType": "FEATURE_FLAG",
	  "sdkKey": "sdk_production_abc123",
	  "environmentKey": "production",
	  "publishedAt": "2026-05-29T00:00:00.000Z",
	  "collectEndpoint": %q,
	  "heartbeatEndpoint": %q,
	  "streamEndpoint": %q,
	  "flags": [{
	    "id": "flag_ab_1",
	    "key": "checkout-v2",
	    "type": "boolean",
	    "enabled": true,
	    "defaultVariationId": "var_off",
	    "variations": [
	      {"id": "var_off", "key": "off", "value": false},
	      {"id": "var_on", "key": "on", "value": true}
	    ],
	    "overrides": [],
	    "rules": [{
	      "id": "rule_full",
	      "type": "targeted_delivery",
	      "enabled": true,
	      "audienceIds": [],
	      "hashAttribute": "user.key",
	      "trafficAllocation": null,
	      "variations": [{"variationId": "var_on", "percentage": 1}]
	    }]
	  }],
	  "audiences": []
	}`, collectEndpoint, heartbeatEndpoint, streamEndpoint)
}

// dataPlane is a fake CDN plus ingestion host in one, so a test can assert what
// the SDK asked for and what it sent.
type dataPlane struct {
	mu sync.Mutex
	// datafileRequests records the paths and headers of datafile GETs.
	datafileRequests []*http.Request
	heartbeats       []map[string]any
	datafile         string
	server           *httptest.Server
}

func newDataPlane(t *testing.T) *dataPlane {
	t.Helper()
	dp := &dataPlane{}
	dp.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/datafile.json"):
			dp.mu.Lock()
			dp.datafileRequests = append(dp.datafileRequests, r.Clone(r.Context()))
			body := dp.datafile
			dp.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		case r.URL.Path == "/api/sdk/heartbeat":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			dp.mu.Lock()
			dp.heartbeats = append(dp.heartbeats, payload)
			dp.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(dp.server.Close)
	dp.datafile = datafileJSON(dp.server.URL, dp.server.URL+"/api/sdk/heartbeat", "")
	return dp
}

func (dp *dataPlane) datafileCalls() []*http.Request {
	dp.mu.Lock()
	defer dp.mu.Unlock()
	return append([]*http.Request(nil), dp.datafileRequests...)
}

func (dp *dataPlane) heartbeatPayloads() []map[string]any {
	dp.mu.Lock()
	defer dp.mu.Unlock()
	return append([]map[string]any(nil), dp.heartbeats...)
}

// TestDatafileURLComesFromTheEndpointContract covers the single largest bug the
// review found: every language SDK fetched its datafile from a URL that does not
// exist, so nothing initialised against production. The canonical shape is
// {cdnHost}/{sdkKey}/datafile.json and nothing may hand-build it.
func TestDatafileURLComesFromTheEndpointContract(t *testing.T) {
	dp := newDataPlane(t)

	server, err := NewServer(ServerOptions{
		SDKKey:          "sdk_production_abc123",
		CDNHost:         dp.server.URL,
		PollingDisabled: true,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	if err := server.OnReady(context.Background()); err != nil {
		t.Fatalf("OnReady: %v", err)
	}

	calls := dp.datafileCalls()
	if len(calls) != 1 {
		t.Fatalf("want one datafile fetch, got %d", len(calls))
	}
	if got := calls[0].URL.Path; got != "/sdk_production_abc123/datafile.json" {
		t.Errorf("datafile path: want /sdk_production_abc123/datafile.json, got %q", got)
	}

	// Keyless by path. The SDK key is the path segment; there is no header,
	// no query string, and no Authorization. The X-SDK-Key header this SDK
	// used to send was read by nothing on the way in.
	if got := calls[0].Header.Get("X-SDK-Key"); got != "" {
		t.Errorf("datafile fetches are keyless by path; found X-SDK-Key: %q", got)
	}
	if got := calls[0].Header.Get("Authorization"); got != "" {
		t.Errorf("there is no Authorization header on the data plane; found %q", got)
	}
	if got := calls[0].URL.RawQuery; got != "" {
		t.Errorf("the datafile URL carries no query string; found %q", got)
	}
	if got := calls[0].Header.Get("User-Agent"); !strings.HasPrefix(got, SDKType+"/") {
		t.Errorf("User-Agent: want it to start with %q, got %q", SDKType+"/", got)
	}
}

// TestForbiddenHostsAreNotDefaults locks the contract's forbidden list. Two of
// these were shipped as defaults, and one of them (stream.avsb.cloud) gave every
// Go process an infinite reconnect loop against DNS that does not resolve.
func TestForbiddenHostsAreNotDefaults(t *testing.T) {
	forbidden := []string{
		"datafile.avsb.cloud",
		"stream.avsb.cloud",
		"sdk.avsb.cloud",
		"collect.avsb.cloud",
	}

	url, err := DatafileURL("sdk_production_abc123", "")
	if err != nil {
		t.Fatalf("DatafileURL: %v", err)
	}
	batchURL, err := CollectURL("batch", "")
	if err != nil {
		t.Fatalf("CollectURL: %v", err)
	}
	defaults := []string{url, batchURL, StreamEndpointURL(""), HeartbeatURL("")}

	for _, host := range forbidden {
		for _, candidate := range defaults {
			if strings.Contains(candidate, host) {
				t.Errorf("%q must never appear in a default URL, found it in %q", host, candidate)
			}
		}
	}
	if url != "https://cdn.avsb.cloud/sdk_production_abc123/datafile.json" {
		t.Errorf("datafile URL: got %q", url)
	}
	if batchURL != "https://ingest.avsb.cloud/v1/collect/batch" {
		t.Errorf("batch URL: got %q", batchURL)
	}
}

// TestHTTPTimeoutIsAlwaysSet covers the hang. This SDK used http.DefaultClient
// with no timeout, so a stalled connection blocked OnReady forever, which is
// exactly what the README's quickstart invited by passing context.Background().
func TestHTTPTimeoutIsAlwaysSet(t *testing.T) {
	t.Run("the default is ten seconds", func(t *testing.T) {
		server, err := NewServer(ServerOptions{
			SDKKey: "sdk_production_abc123", Logger: silentLogger(),
		})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		if server.httpClient.Timeout != 10*time.Second {
			t.Errorf("datafile client timeout: want 10s, got %v", server.httpClient.Timeout)
		}
	})

	t.Run("an explicit timeout is honoured", func(t *testing.T) {
		server, err := NewServer(ServerOptions{
			SDKKey: "sdk_production_abc123", HTTPTimeout: 3 * time.Second, Logger: silentLogger(),
		})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		if server.httpClient.Timeout != 3*time.Second {
			t.Errorf("want 3s, got %v", server.httpClient.Timeout)
		}
	})

	t.Run("the streaming client bounds the handshake, not the stream", func(t *testing.T) {
		server, err := NewServer(ServerOptions{
			SDKKey: "sdk_production_abc123", Logger: silentLogger(),
		})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		// An SSE body stays open, so an overall deadline would kill every
		// healthy stream on a timer.
		if server.streamHTTPClient.Timeout != 0 {
			t.Errorf("streaming client must have no overall timeout, got %v",
				server.streamHTTPClient.Timeout)
		}
		transport, ok := server.streamHTTPClient.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("want a *http.Transport, got %T", server.streamHTTPClient.Transport)
		}
		if transport.ResponseHeaderTimeout == 0 {
			t.Error("the streaming handshake must still be bounded by ResponseHeaderTimeout")
		}
	})
}

// TestHeartbeatMatchesTheLockedPayload covers SDK liveness. Without it a
// perfectly working service shows as Not connected on the Environments page.
func TestHeartbeatMatchesTheLockedPayload(t *testing.T) {
	dp := newDataPlane(t)

	server, err := NewServer(ServerOptions{
		SDKKey:          "sdk_production_abc123",
		CDNHost:         dp.server.URL,
		PollingDisabled: true,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()
	if err := server.OnReady(context.Background()); err != nil {
		t.Fatalf("OnReady: %v", err)
	}

	beats := dp.heartbeatPayloads()
	if len(beats) != 1 {
		t.Fatalf("want one heartbeat after a successful fetch, got %d", len(beats))
	}
	beat := beats[0]
	if beat["sdkKey"] != "sdk_production_abc123" {
		t.Errorf("sdkKey: got %v", beat["sdkKey"])
	}
	if beat["sdkType"] != "avsb-go" {
		t.Errorf("sdkType: want the published package identifier avsb-go, got %v", beat["sdkType"])
	}
	if version, _ := beat["sdkVersion"].(string); version == "" {
		t.Error("sdkVersion must be present, read from build metadata")
	}
	if len(beat) != 3 {
		t.Errorf("the heartbeat carries exactly sdkKey, sdkType, sdkVersion; got %v", beat)
	}
}

// TestNoHeartbeatEndpointMeansNoHeartbeat covers the deliberate non-default. A
// proxied deployment publishes its own origin, so posting to the public app host
// would report liveness nowhere while looking healthy.
func TestNoHeartbeatEndpointMeansNoHeartbeat(t *testing.T) {
	dp := newDataPlane(t)
	dp.mu.Lock()
	dp.datafile = datafileJSON(dp.server.URL, "", "")
	dp.mu.Unlock()

	server, err := NewServer(ServerOptions{
		SDKKey:          "sdk_production_abc123",
		CDNHost:         dp.server.URL,
		PollingDisabled: true,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()
	if err := server.OnReady(context.Background()); err != nil {
		t.Fatalf("OnReady: %v", err)
	}

	if beats := dp.heartbeatPayloads(); len(beats) != 0 {
		t.Errorf("want no heartbeat when the datafile publishes no endpoint, got %v", beats)
	}
}

// TestNotReadyBeforeInitAndAfterClose covers B15 at the SDK boundary. Before,
// both states reported not_found, so a startup race was indistinguishable from a
// deleted flag.
func TestNotReadyBeforeInitAndAfterClose(t *testing.T) {
	dp := newDataPlane(t)

	server, err := NewServer(ServerOptions{
		SDKKey:          "sdk_production_abc123",
		CDNHost:         dp.server.URL,
		PollingDisabled: true,
		IncludeReasons:  true,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	evalCtx := UserContext("u_1", nil)

	before := server.GetBoolFlag(ctx, "checkout-v2", true, WithContext(evalCtx))
	if before.Source() != "not_ready" {
		t.Errorf("before OnReady: want source not_ready, got %q", before.Source())
	}
	if !before.Value() {
		t.Error("before OnReady: want the caller's default back, unchanged")
	}
	if before.Exists() {
		t.Error("before OnReady: Exists must be false, because nothing is knowable")
	}
	if before.IsEnabled() {
		t.Error("before OnReady: IsEnabled must be false even with a truthy default")
	}
	if len(before.Reasons()) == 0 || !strings.Contains(before.Reasons()[0], "not ready") {
		t.Errorf("the reason must say the SDK is not ready, got %v", before.Reasons())
	}

	if err := server.OnReady(ctx); err != nil {
		t.Fatalf("OnReady: %v", err)
	}

	during := server.GetBoolFlag(ctx, "checkout-v2", false, WithContext(evalCtx))
	if during.Source() != "rule" {
		t.Errorf("after OnReady: want source rule, got %q (reasons %v)", during.Source(), during.Reasons())
	}
	if !during.IsEnabled() {
		t.Error("after OnReady: the full-rollout rule serves on, so the flag is enabled")
	}

	missing := server.GetBoolFlag(ctx, "no-such-flag", false, WithContext(evalCtx))
	if missing.Source() != "not_found" {
		t.Errorf("a key absent from a LOADED datafile is not_found, got %q", missing.Source())
	}

	server.Close()

	after := server.GetBoolFlag(ctx, "checkout-v2", true, WithContext(evalCtx))
	if after.Source() != "not_ready" {
		t.Errorf("after Close: want source not_ready, got %q", after.Source())
	}
	if len(after.Reasons()) == 0 || !strings.Contains(after.Reasons()[0], "closed") {
		t.Errorf("the reason must say the SDK is closed, got %v", after.Reasons())
	}
}

// TestOnReadyFailureLeavesTheServerUsable covers the resilience half of the same
// story: a CDN blip at boot must not brick flags, and the polling loop keeps
// trying.
func TestOnReadyFailureLeavesTheServerUsable(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer dead.Close()

	server, err := NewServer(ServerOptions{
		SDKKey:          "sdk_production_abc123",
		CDNHost:         dead.URL,
		PollingDisabled: true,
		IncludeReasons:  true,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	readyErr := server.OnReady(context.Background())
	if readyErr == nil {
		t.Fatal("a 404 datafile must be reported, not swallowed")
	}
	// The message has to be actionable: what failed, the exact URL, and a
	// command to reproduce it.
	message := readyErr.Error()
	for _, want := range []string{"404", "/sdk_production_abc123/datafile.json", "curl"} {
		if !strings.Contains(message, want) {
			t.Errorf("the failure message must mention %q; got %q", want, message)
		}
	}

	flag := server.GetBoolFlag(context.Background(), "checkout-v2", true, WithContext(UserContext("u", nil)))
	if flag.Source() != "not_ready" {
		t.Errorf("want not_ready after a failed init, got %q", flag.Source())
	}
	if !flag.Value() {
		t.Error("a failed init still returns the caller's default")
	}
}

// TestBootstrapServesImmediately covers the offline path: with a datafile in
// hand, evaluation works with no network at all.
func TestBootstrapServesImmediately(t *testing.T) {
	server, err := NewServer(ServerOptions{
		Bootstrap:       []byte(datafileJSON("https://ingest.avsb.cloud", "", "")),
		PollingDisabled: true,
		IncludeReasons:  true,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	flag := server.GetBoolFlag(context.Background(), "checkout-v2", false, WithContext(UserContext("u", nil)))
	if flag.Source() != "rule" {
		t.Errorf("a bootstrap datafile serves immediately; got source %q", flag.Source())
	}
}

// TestMiddlewareContextIsHonoured is the fix for the discarded context.Context.
// The middleware attaches identity to the request context, and evaluation reads
// it, so a handler needs no plumbing. Before this, the context argument was
// literally discarded and every middleware-served request bucketed as anonymous.
func TestMiddlewareContextIsHonoured(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	ctx := ContextWithEvalContext(context.Background(), UserContext("u_from_middleware", nil))

	flag := server.GetBoolFlag(ctx, "checkout-v2", false)
	if flag.Source() != "rule" {
		t.Fatalf("want the rule to serve; got %q with reasons %v", flag.Source(), flag.Reasons())
	}

	// The decision log proves WHICH identity was used, not just that one was.
	log := NewMemoryDecisionLog(10)
	server.SetDecisionLog(log)
	_ = server.GetBoolFlag(ctx, "checkout-v2", false)

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("want one logged decision, got %d", len(entries))
	}
	if len(entries[0].ContextSummary) != 1 || entries[0].ContextSummary[0].Key != "u_from_middleware" {
		t.Errorf("want the middleware identity in the decision log, got %+v", entries[0].ContextSummary)
	}
}

// TestExplicitContextBeatsTheGoContext covers precedence, so a handler can
// override the middleware's identity for one call.
func TestExplicitContextBeatsTheGoContext(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	log := NewMemoryDecisionLog(10)
	server.SetDecisionLog(log)

	ctx := ContextWithEvalContext(context.Background(), UserContext("u_from_middleware", nil))
	_ = server.GetBoolFlag(ctx, "checkout-v2", false, WithContext(UserContext("u_explicit", nil)))

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("want one logged decision, got %d", len(entries))
	}
	if entries[0].ContextSummary[0].Key != "u_explicit" {
		t.Errorf("WithContext must win over the Go context, got %q",
			entries[0].ContextSummary[0].Key)
	}
}

// TestGetAllFlagsEvaluatesEveryKey covers the bulk read a server-rendered page
// needs. Without it a page pays one call per flag and the code reads like a list
// of magic strings.
func TestGetAllFlagsEvaluatesEveryKey(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	all := server.GetAllFlags(context.Background(), WithContext(UserContext("u", nil)))
	if len(all) != 1 {
		t.Fatalf("want one flag, got %d", len(all))
	}
	flag, present := all["checkout-v2"]
	if !present {
		t.Fatalf("want the flag keyed by its key, got %v", all)
	}
	if string(flag.Value()) != "true" {
		t.Errorf("value: want true, got %s", flag.Value())
	}
	if flag.Source() != "rule" {
		t.Errorf("source: want rule, got %q", flag.Source())
	}
}

// TestGetAllFlagsOnANotReadyServerIsEmptyNotNil covers the boundary a template
// would otherwise panic on.
func TestGetAllFlagsOnANotReadyServerIsEmptyNotNil(t *testing.T) {
	server, err := NewServer(ServerOptions{
		SDKKey: "sdk_production_abc123", PollingDisabled: true,
		Events: EventOptions{Disabled: true}, Logger: silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	all := server.GetAllFlags(context.Background())
	if all == nil {
		t.Fatal("want an empty map, got nil")
	}
	if len(all) != 0 {
		t.Errorf("want no flags before a datafile loads, got %v", all)
	}
}

// TestTypedGetterFallsBackOnATypeMismatch covers the case Eppo turned into a
// headline: reading a string flag with the boolean getter must return the
// caller's default rather than a coerced value.
func TestTypedGetterFallsBackOnATypeMismatch(t *testing.T) {
	datafile := `{
	  "version": 2, "sdkKey": "sdk_production_abc123", "publishedAt": "x",
	  "collectEndpoint": "https://ingest.avsb.cloud",
	  "flags": [{
	    "id": "flag_s", "key": "copy-variant", "type": "string", "enabled": true,
	    "defaultVariationId": "var_a",
	    "variations": [{"id": "var_a", "key": "a", "value": "bold"}],
	    "overrides": [], "rules": []
	  }],
	  "audiences": []
	}`
	server, err := NewTestServer([]byte(datafile))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	asBool := server.GetBoolFlag(context.Background(), "copy-variant", true, WithContext(UserContext("u", nil)))
	if !asBool.Value() {
		t.Error("a string value read as a bool returns the caller's default, never a coercion")
	}

	asString := server.GetStringFlag(context.Background(), "copy-variant", "fallback", WithContext(UserContext("u", nil)))
	if asString.Value() != "bold" {
		t.Errorf("the matching getter returns the real value, got %q", asString.Value())
	}
}

// TestRuntimeOverridesWinAndAreDeterministic covers the client-layer step the
// semantics contract puts first, including the ordering guard: ranging over an
// override map would make a multi-context resolve differently between two
// identical calls, because Go randomises map iteration.
func TestRuntimeOverridesWinAndAreDeterministic(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	ctx := context.Background()
	multi := MultiContext(
		UserContext("u_1", nil),
		OrgContext("acme", nil),
	)

	// A user override and an organization override both match this context.
	// The user identity is checked first, always.
	server.SetOverrideForUser("checkout-v2", "u_1", "off")
	server.SetOverrideForUser("checkout-v2", "acme", "on", "organization")

	for i := 0; i < 20; i++ {
		flag := server.GetBoolFlag(ctx, "checkout-v2", false, WithContext(multi))
		if flag.Source() != "runtimeOverride" {
			t.Fatalf("want source runtimeOverride, got %q", flag.Source())
		}
		if flag.Value() {
			t.Fatalf("iteration %d: the user override (off) must win over the organization one", i)
		}
	}

	server.ClearOverrideForUser("checkout-v2", "u_1")
	orgWins := server.GetBoolFlag(ctx, "checkout-v2", false, WithContext(multi))
	if !orgWins.Value() {
		t.Error("with the user override cleared, the organization override serves on")
	}

	server.ClearAllOverrides()
	back := server.GetBoolFlag(ctx, "checkout-v2", false, WithContext(multi))
	if back.Source() != "rule" {
		t.Errorf("with overrides cleared the rule serves again, got %q", back.Source())
	}
}

// TestGlobalOverrideAppliesToEveryone covers the other override shape, plus the
// guard that an override naming a variation the flag does not have resolves to
// nothing rather than to a broken value.
func TestGlobalOverrideAppliesToEveryone(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	ctx := context.Background()
	server.SetGlobalOverride("checkout-v2", "off")
	for _, key := range []string{"u_1", "u_2", "u_3"} {
		flag := server.GetBoolFlag(ctx, "checkout-v2", true, WithContext(UserContext(key, nil)))
		if flag.Source() != "runtimeOverride" || flag.Value() {
			t.Errorf("%s: want the global override to serve off, got source %q value %v",
				key, flag.Source(), flag.Value())
		}
	}

	server.SetGlobalOverride("checkout-v2", "no-such-variation")
	flag := server.GetBoolFlag(ctx, "checkout-v2", false, WithContext(UserContext("u_1", nil)))
	if flag.Source() != "rule" {
		t.Errorf("an override naming a missing variation resolves to nothing, got source %q", flag.Source())
	}
}

// TestNewServerRefusesToStartWithNothing covers the construction guard, and that
// its message says where the key lives.
func TestNewServerRefusesToStartWithNothing(t *testing.T) {
	_, err := NewServer(ServerOptions{Logger: silentLogger()})
	if err == nil {
		t.Fatal("a Server with no key, no bootstrap, and no URL cannot do anything")
	}
	for _, want := range []string{"SDKKey", "Environments", "Bootstrap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message must mention %q; got %q", want, err.Error())
		}
	}
}

// TestOnReadyAfterCloseIsRefused covers the lifecycle edge, so a restart is an
// explicit new Server rather than a half-revived one.
func TestOnReadyAfterCloseIsRefused(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	server.Close()

	if err := server.OnReady(context.Background()); err == nil {
		t.Error("OnReady after Close must be refused")
	}
}

// TestPollingRefetchesTheDatafile covers the refresh loop. Without one, a flag
// change needs a process restart, which the review called out as intolerable and
// which the README claimed already worked.
func TestPollingRefetchesTheDatafile(t *testing.T) {
	dp := newDataPlane(t)

	// Drive the loop in milliseconds instead of waiting out the real 5s floor.
	originalFloor := minPollingInterval
	minPollingInterval = 20 * time.Millisecond
	defer func() { minPollingInterval = originalFloor }()

	server, err := NewServer(ServerOptions{
		SDKKey:          "sdk_production_abc123",
		CDNHost:         dp.server.URL,
		PollingInterval: 20 * time.Millisecond,
		Events:          EventOptions{Disabled: true},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()
	if err := server.OnReady(context.Background()); err != nil {
		t.Fatalf("OnReady: %v", err)
	}

	// Flip the served datafile so the refetch is observable in a decision.
	dp.mu.Lock()
	dp.datafile = strings.Replace(dp.datafile, `"enabled": true`, `"enabled": false`, 1)
	dp.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		flag := server.GetBoolFlag(context.Background(), "checkout-v2", false,
			WithContext(UserContext("u", nil)))
		if flag.Source() == "disabled" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the polling loop never picked up the republished datafile")
}

// TestPollingIntervalIsClamped covers the guard against a hot loop against the
// CDN.
func TestPollingIntervalIsClamped(t *testing.T) {
	if minPollingInterval != 5*time.Second {
		t.Errorf("the documented minimum is 5s, got %v", minPollingInterval)
	}
	if defaultPollingInterval != 30*time.Second {
		t.Errorf("the documented default is 30s, got %v", defaultPollingInterval)
	}
}

// TestTrackRequiresAVisitor covers the drop-with-a-reason path. An event with no
// visitor cannot be joined to an exposure, so accepting it silently would show a
// metric with data that attributes to nobody.
func TestTrackRequiresAVisitor(t *testing.T) {
	dp := newDataPlane(t)

	server, err := NewServer(ServerOptions{
		SDKKey:          "sdk_production_abc123",
		CDNHost:         dp.server.URL,
		PollingDisabled: true,
		Events:          EventOptions{FlushInterval: time.Hour, BatchSize: 100},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()
	if err := server.OnReady(context.Background()); err != nil {
		t.Fatalf("OnReady: %v", err)
	}

	server.mu.RLock()
	tracker := server.tracker
	server.mu.RUnlock()
	if tracker == nil {
		t.Fatal("a loaded datafile must start the event tracker")
	}

	server.Track(context.Background(), "signup_completed", TrackEvent{})
	if _, metrics := tracker.queued(); metrics != 0 {
		t.Errorf("an event with no visitor is dropped, queued %d", metrics)
	}

	server.Track(context.Background(), "", TrackEvent{VisitorID: "u_1"})
	if _, metrics := tracker.queued(); metrics != 0 {
		t.Errorf("an event with no name is dropped, queued %d", metrics)
	}

	server.Track(context.Background(), "signup_completed", TrackEvent{Context: UserContext("u_1", nil)})
	if _, metrics := tracker.queued(); metrics != 1 {
		t.Errorf("an event with a context is queued, got %d", metrics)
	}
}

// TestTrackPurchaseValidatesItsInput covers the three refusals, each naming what
// is wrong. Ingestion rejects a zero or negative total, so catching it here turns
// a silent server-side drop into an error the caller can log.
func TestTrackPurchaseValidatesItsInput(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	ctx := context.Background()
	if err := server.TrackPurchase(ctx, "", PurchaseOrder{OrderID: "O", Total: 1}); err == nil {
		t.Error("a purchase with no visitor must be refused")
	}
	if err := server.TrackPurchase(ctx, "u", PurchaseOrder{Total: 1}); err == nil {
		t.Error("a purchase with no order id must be refused")
	}
	err = server.TrackPurchase(ctx, "u", PurchaseOrder{OrderID: "O", Total: 0})
	if err == nil {
		t.Fatal("a zero total must be refused")
	}
	if !strings.Contains(err.Error(), "major units") {
		t.Errorf("the message should explain the units; got %q", err.Error())
	}
}

// TestExposuresReachTheTrackerOnEvaluation is the end-to-end wiring check: an
// ab_test decision has to produce an exposure, which is the entire reason a
// server SDK exists for an experimentation platform.
func TestExposuresReachTheTrackerOnEvaluation(t *testing.T) {
	datafile := `{
	  "version": 2, "sdkKey": "sdk_production_abc123", "publishedAt": "x",
	  "collectEndpoint": "https://ingest.avsb.cloud",
	  "flags": [{
	    "id": "flag_ab", "key": "split", "type": "string", "enabled": true,
	    "defaultVariationId": "var_a",
	    "variations": [
	      {"id": "var_a", "key": "a", "value": "A"},
	      {"id": "var_b", "key": "b", "value": "B"}
	    ],
	    "overrides": [],
	    "rules": [{
	      "id": "rule_split", "type": "ab_test", "enabled": true, "audienceIds": [],
	      "hashAttribute": "user.key", "trafficAllocation": 1,
	      "variations": [
	        {"variationId": "var_a", "percentage": 0.5},
	        {"variationId": "var_b", "percentage": 0.5}
	      ]
	    }]
	  }],
	  "audiences": []
	}`

	rec := &captured{}
	ingest := httptest.NewServer(rec.handler())
	defer ingest.Close()

	server, err := NewServer(ServerOptions{
		Bootstrap:       []byte(datafile),
		CollectEndpoint: ingest.URL,
		PollingDisabled: true,
		Events:          EventOptions{FlushInterval: time.Hour, BatchSize: 100},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	ctx := context.Background()
	_ = server.GetStringFlag(ctx, "split", "fallback", WithContext(UserContext("u_exposed", nil)))
	server.Flush(ctx)

	paths, bodies, requests := rec.snapshot()
	if requests != 1 {
		t.Fatalf("want one exposure batch, got %d requests", requests)
	}
	if paths[0] != "/v1/collect/batch" {
		t.Errorf("path: want /v1/collect/batch, got %q", paths[0])
	}
	events := bodies[0]["events"].([]any)
	event := events[0].(map[string]any)
	if event["visitorId"] != "u_exposed" {
		t.Errorf("visitorId: want the bucketing identity u_exposed, got %v", event["visitorId"])
	}
	if event["experimentId"] != "rule_split" {
		t.Errorf("experimentId: want the rule id, got %v", event["experimentId"])
	}
	if event["eventName"] != "flag_exposure" {
		t.Errorf("eventName: want flag_exposure, got %v", event["eventName"])
	}
}

// TestWithoutExposureSuppressesTheEvent covers the read that is not an
// impression. An exposure the visitor never saw dilutes the experiment.
func TestWithoutExposureSuppressesTheEvent(t *testing.T) {
	rec := &captured{}
	ingest := httptest.NewServer(rec.handler())
	defer ingest.Close()

	server, err := NewServer(ServerOptions{
		Bootstrap:       []byte(datafileJSON("https://ingest.avsb.cloud", "", "")),
		CollectEndpoint: ingest.URL,
		PollingDisabled: true,
		Events:          EventOptions{FlushInterval: time.Hour, BatchSize: 100},
		Logger:          silentLogger(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	ctx := context.Background()
	_ = server.GetBoolFlag(ctx, "checkout-v2", false,
		WithContext(UserContext("u_quiet", nil)), WithoutExposure())
	server.Flush(ctx)

	if _, _, requests := rec.snapshot(); requests != 0 {
		t.Errorf("WithoutExposure must send nothing, got %d requests", requests)
	}
}

// TestDecisionLogRecordsEveryEvaluation covers the promise the type makes, and
// the bound that keeps it from leaking memory in a long-running server.
func TestDecisionLogRecordsEveryEvaluation(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	log := NewMemoryDecisionLog(3)
	server.SetDecisionLog(log)

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = server.GetBoolFlag(ctx, "checkout-v2", false,
			WithContext(UserContext(fmt.Sprintf("u_%d", i), nil)))
	}

	entries := log.Entries()
	if len(entries) != 3 {
		t.Fatalf("the collector is bounded at 3, got %d entries", len(entries))
	}
	// The three newest survive.
	if entries[0].ContextSummary[0].Key != "u_7" {
		t.Errorf("want the oldest dropped, so the first kept is u_7; got %q",
			entries[0].ContextSummary[0].Key)
	}
	last := entries[2]
	if last.FlagKey != "checkout-v2" {
		t.Errorf("flagKey: got %q", last.FlagKey)
	}
	if last.Source != "rule" {
		t.Errorf("source: got %q", last.Source)
	}
	if last.VariationKey != "on" {
		t.Errorf("variationKey: got %q", last.VariationKey)
	}
	if last.RuleType != "targeted_delivery" {
		t.Errorf("ruleType: got %q", last.RuleType)
	}
	if last.EvaluatedAt.IsZero() {
		t.Error("evaluatedAt must be set")
	}

	if drained := log.Drain(); len(drained) != 3 {
		t.Errorf("Drain returns what was held, got %d", len(drained))
	}
	if len(log.Entries()) != 0 {
		t.Error("Drain must empty the buffer")
	}
}

// TestStreamingStaysOffWithNowhereToConnect covers B12. Streaming on with no
// published endpoint used to mean an infinite reconnect loop against a host that
// does not resolve; now it says so once and polling carries on.
func TestStreamingStaysOffWithNowhereToConnect(t *testing.T) {
	resolution := ResolveStreamEndpoint("", "")
	if resolution.Available {
		t.Fatal("with neither an option nor a datafile field, streaming must be unavailable")
	}
	for _, want := range []string{"streamEndpoint", "Polling stays"} {
		if !strings.Contains(resolution.Reason, want) {
			t.Errorf("the reason must mention %q; got %q", want, resolution.Reason)
		}
	}
	if strings.Contains(resolution.Reason, "stream.avsb.cloud") {
		t.Error("the reason must not point at a host that does not exist")
	}

	fromDatafile := ResolveStreamEndpoint("", "https://app.avsb.cloud/api/sdk/stream")
	if !fromDatafile.Available || fromDatafile.Source != "datafile" {
		t.Errorf("the datafile field enables streaming, got %+v", fromDatafile)
	}
	fromOption := ResolveStreamEndpoint("https://stream.example.test/ticket",
		"https://app.avsb.cloud/api/sdk/stream")
	if fromOption.Source != "option" {
		t.Errorf("an explicit option wins, got %+v", fromOption)
	}
}

// TestServerIsSafeForConcurrentUse covers the promise the docs make about
// sharing one Server across goroutines, including while the override table and
// the decision log are being written.
func TestServerIsSafeForConcurrentUse(t *testing.T) {
	server, err := NewTestServer([]byte(datafileJSON("https://ingest.avsb.cloud", "", "")))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()
	server.SetDecisionLog(NewMemoryDecisionLog(100))

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("u_%d", i)
			switch i % 3 {
			case 0:
				server.SetOverrideForUser("checkout-v2", key, "off")
			case 1:
				server.ClearOverrideForUser("checkout-v2", key)
			}
			for j := 0; j < 20; j++ {
				_ = server.GetBoolFlag(ctx, "checkout-v2", false, WithContext(UserContext(key, nil)))
				_ = server.GetAllFlags(ctx, WithContext(UserContext(key, nil)))
			}
		}(i)
	}
	wg.Wait()
}

// TestSDKVersionIsResolvedNotHardcoded covers the heartbeat's version field.
// Inside this module's own tests there is no dependency entry to read, so the
// honest answer is the unknown sentinel rather than a stale literal.
func TestSDKVersionIsResolvedNotHardcoded(t *testing.T) {
	got := SDKVersion()
	if got == "" {
		t.Fatal("SDKVersion must never be empty: it is sent on every heartbeat")
	}
	if got != unknownVersion && !strings.HasPrefix(got, "v") {
		t.Errorf("a resolved module version starts with v, got %q", got)
	}
	if SDKType != "avsb-go" {
		t.Errorf("SDKType is the published package identifier; got %q", SDKType)
	}
}

// TestEvalContextRoundTrip covers the context factories, which are the first
// thing every caller touches.
func TestEvalContextRoundTrip(t *testing.T) {
	t.Run("a user context carries its key and attributes", func(t *testing.T) {
		ctx := UserContext("u_1", map[string]any{"plan": "pro", "seats": 12})
		if BucketingKey(ctx, "user") != "u_1" {
			t.Errorf("user key: got %q", BucketingKey(ctx, "user"))
		}
		encoded, err := json.Marshal(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"kind":"user"`, `"key":"u_1"`, `"plan":"pro"`, `"seats":12`} {
			if !strings.Contains(string(encoded), want) {
				t.Errorf("want %s in %s", want, encoded)
			}
		}
	})

	t.Run("a multi context exposes every kind", func(t *testing.T) {
		ctx := MultiContext(
			UserContext("u_1", nil),
			OrgContext("acme", map[string]any{"tier": "enterprise"}),
			SingleContext("device", "d_1", nil),
		)
		if BucketingKey(ctx, "user") != "u_1" {
			t.Errorf("user: got %q", BucketingKey(ctx, "user"))
		}
		if BucketingKey(ctx, "organization") != "acme" {
			t.Errorf("organization: got %q", BucketingKey(ctx, "organization"))
		}
		if BucketingKey(ctx, "device") != "d_1" {
			t.Errorf("device: got %q", BucketingKey(ctx, "device"))
		}
	})

	t.Run("a nested multi context is ignored rather than corrupting the shape", func(t *testing.T) {
		inner := MultiContext(UserContext("u_1", nil))
		ctx := MultiContext(inner, OrgContext("acme", nil))
		if BucketingKey(ctx, "organization") != "acme" {
			t.Errorf("the valid sub-context still lands, got %q", BucketingKey(ctx, "organization"))
		}
	})

	t.Run("an unserialisable attribute is skipped, not fatal", func(t *testing.T) {
		ctx := UserContext("u_1", map[string]any{
			"plan":    "pro",
			"channel": make(chan int),
		})
		if BucketingKey(ctx, "user") != "u_1" {
			t.Error("the context must still be usable")
		}
		if _, present := ctx["channel"]; present {
			t.Error("an attribute that cannot be serialised must not be carried")
		}
	})
}
