// Package avsb is the official Go SDK for the A vs B feature-flag and
// experimentation platform.
//
// Flags are evaluated locally, in microseconds, against a datafile the SDK
// fetches from a global CDN. There is no relay to run and no per-evaluation
// network call.
//
// # Quick start
//
//	server, err := avsb.NewServer(avsb.ServerOptions{
//	    SDKKey: os.Getenv("AVSB_SDK_KEY"),
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer server.Close()
//
//	ctx := context.Background()
//	if err := server.OnReady(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	evalCtx := avsb.UserContext("u_123", map[string]any{"plan": "pro"})
//	flag := server.GetBoolFlag(ctx, "checkout_v2", false, avsb.WithContext(evalCtx))
//	if flag.IsEnabled() {
//	    // serve the new checkout
//	}
//
// Create ONE [Server] per process and share it across goroutines. Close it
// before the process exits, or the last batch of exposures never leaves.
//
// # What arrives without configuration
//
//   - The datafile is fetched from https://cdn.avsb.cloud/{sdkKey}/datafile.json
//     and refreshed every 30 seconds with jitter.
//   - Every HTTP call has a 10 second timeout.
//   - Exposures are recorded automatically on ab_test, bandit, and holdout
//     decisions, batched, and flushed on [Server.Close].
//   - A heartbeat reports liveness so the dashboard's Environments page can
//     show this service as connected.
//   - Failures are loud: a malformed SDK key, a 404 datafile, an empty
//     evaluation context, and a dropped event batch each log a message naming
//     what to fix.
//
// # Layers
//
//   - This package is the public API: [Server], [Flag], the context factories,
//     and the event methods.
//   - The core sub-package is the pure evaluation engine (hash, audience,
//     evaluate, holdouts, bandits). It mirrors the TypeScript reference and
//     passes the shared conformance corpus, which compares values, variation
//     keys, sources, rule ids, and the ordered reason strings exactly.
//
// # Not ready versus not found
//
// Evaluating before [Server.OnReady] finishes, or after [Server.Close], returns
// the caller's default with source "not_ready". That is a different answer from
// "not_found", which means the datafile loaded and does not contain the key.
// Conflating them made a startup race look like a deleted flag.
//
// # Middleware
//
// HTTP middleware lives under middleware/{httpmw,ginmw,echomw,fibermw}. Each
// resolves the identity once per request and attaches it to the request
// context, so a handler calls server.GetBoolFlag(r.Context(), ...) with no
// plumbing. net/http and go-chi both use httpmw, which is part of this module;
// gin, echo, and fiber are separate modules so their frameworks stay out of the
// build graph of anyone who does not use them.
//
// # Sticky bucketing
//
// Pass a [StickyBucketService] to keep a visitor in the variation they were
// first bucketed into. The in-memory implementation needs no dependencies;
// Redis, DynamoDB, and Postgres live behind build tags in the sticky
// sub-package.
//
// # Bandits
//
// A bandit-typed rule dispatches through the bandit engine. With no trained
// model it runs a deterministic cold-start policy rather than being skipped, so
// a configured bandit always serves something and always says what it did in
// [Flag.Reasons].
//
// # Streaming
//
// Streaming is off by default. Set ServerOptions.StreamingEnabled to open an
// SSE connection so flag changes arrive in about a second instead of on the
// polling interval. The endpoint is server controlled: the datafile publishes
// it, and when it publishes none the SDK says so once and stays on polling. It
// never guesses a host.
//
// # Testing
//
// [NewTestServer] loads a datafile you supply with no network of any kind and
// reasons switched on.
package avsb
