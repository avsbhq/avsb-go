package avsb

import (
	"github.com/avsbhq/avsb-go/core"
	"log/slog"
)

// ServerOptions configures a new [Server] instance.
type ServerOptions struct {
	// SDKKey is the server-side SDK key from the A vs B dashboard.
	// Required unless DatafileURL or InlineDatafile is provided.
	SDKKey string

	// DatafileURL overrides the URL from which the datafile is fetched.
	// Defaults to https://datafile.avsb.cloud/v2/{SDKKey}.
	DatafileURL string

	// InlineDatafile provides the datafile bytes directly, bypassing HTTP
	// fetching. Useful for tests and edge runtimes with no outbound access.
	InlineDatafile []byte

	// Logger is the structured logger the server will use for internal
	// messages. Defaults to slog.Default().
	Logger *slog.Logger

	// StickyBucketService is an optional persistent bucketing backend. When
	// set, users that have already been bucketed into an ab_test variation
	// stay in that variation across calls.
	StickyBucketService core.StickyBucketService

	// StreamingEnabled controls whether the server opens a persistent SSE
	// connection for real-time datafile updates. Defaults to true when
	// SDKKey is non-empty and DatafileURL is empty.
	StreamingEnabled *bool

	// IncludeReasons controls whether flag evaluations include the human-
	// readable audit trail in FlagEvaluation.Reasons. Defaults to false
	// for production and true when running tests (via NewTestServer).
	IncludeReasons bool
}

// Option is a functional option applied to a per-call evaluation.
type Option func(*callOpts)

type callOpts struct {
	ctx            core.EvalContext
	includeReasons *bool
}

// WithContext supplies the evaluation context for this call.
func WithContext(evalCtx core.EvalContext) Option {
	return func(o *callOpts) {
		o.ctx = evalCtx
	}
}

// WithReasons enables or disables the reasons audit trail for this call,
// overriding the server-level IncludeReasons setting.
func WithReasons(enabled bool) Option {
	return func(o *callOpts) {
		o.includeReasons = &enabled
	}
}

func applyCallOpts(opts []Option) callOpts {
	co := callOpts{}
	for _, o := range opts {
		o(&co)
	}
	return co
}
