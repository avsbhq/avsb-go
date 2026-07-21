// Package avsb is the official Go SDK for the A vs B feature-flag and
// experimentation platform.
//
// # Quick start
//
//	server, err := avsb.NewServer(avsb.ServerOptions{SDKKey: os.Getenv("AVSB_SDK_KEY")})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	ctx := context.Background()
//	if err := server.OnReady(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	evalCtx := avsb.UserContext("u_1", map[string]any{"plan": "pro"})
//	flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))
//	if flag.IsEnabled() {
//	    // serve new checkout
//	}
//
// # Architecture
//
// The SDK is split into two layers:
//
//   - The top-level avsb package exposes the [Server] type, the public API
//     callers interact with.
//   - The core sub-package contains pure evaluation logic (hash, audience,
//     evaluate, holdouts, bandits) that mirrors the canonical TypeScript
//     implementation and passes the shared conformance suite.
//
// # Middleware
//
// HTTP middleware for net/http, chi, gin, echo and fiber live under
// middleware/{httpmw,chimw,ginmw,echomw,fibermw}. Each middleware injects an
// evaluated [EvalContext] into the request context so handlers can call
// server.GetBoolFlag(r.Context(), ...) without plumbing the context manually.
//
// # Sticky bucketing
//
// Optional persistent bucketing adapters live under the sticky sub-package.
// The in-memory adapter ([sticky.InMemoryService]) requires no external dep.
// Redis, DynamoDB and Postgres adapters are build-tag gated.
//
// # Conformance
//
// The conformance sub-package runs the shared fixture suite from
// packages/avsb-conformance/fixtures/ and verifies that the Go evaluator
// produces byte-identical decisions to the TypeScript reference implementation.
package avsb
