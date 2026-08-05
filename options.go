package avsb

import (
	"log/slog"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// ServerOptions configures a Server. Only SDKKey is required.
type ServerOptions struct {
	// SDKKey is the environment SDK key from Settings, then Environments, in
	// the A vs B dashboard. It looks like sdk_production_xxxxxxxx. Required
	// unless Bootstrap or DatafileURL is set.
	//
	// It is a PUBLIC identifier: it names an environment and grants read
	// access to that environment's published flag configuration, nothing
	// more. It is still worth keeping out of a public repository, because
	// rotating it means republishing every deployment.
	SDKKey string

	// Bootstrap is a datafile supplied directly, as raw JSON. Evaluation works
	// immediately with no network call, which is what makes tests, air-gapped
	// deployments, and CI runs possible. When SDKKey is also set, the
	// bootstrap serves traffic while the first fetch runs.
	Bootstrap []byte

	// DatafileURL overrides the datafile address entirely. Leave it empty: the
	// canonical URL is built from SDKKey against DefaultCDNHost. Set it only
	// for a proxy that mirrors the CDN.
	DatafileURL string

	// CDNHost overrides the CDN origin, keeping the /{sdkKey}/datafile.json
	// path. Use it for a self-hosted mirror or for local development
	// ("http://localhost:3000").
	CDNHost string

	// CollectEndpoint overrides the event ingestion origin. Leave it empty:
	// the datafile publishes the right origin, and this exists only for a
	// proxy in front of it.
	CollectEndpoint string

	// StreamingEndpoint overrides the SSE ticket endpoint. An explicit value
	// beats the datafile's own streamEndpoint field.
	StreamingEndpoint string

	// StreamingEnabled opens a persistent SSE connection so flag changes
	// arrive in about a second instead of on the polling interval.
	//
	// It is OFF by default. Streaming is server controlled: the datafile
	// publishes the endpoint, and when neither the datafile nor
	// StreamingEndpoint names one, the SDK says so once and stays on polling.
	// It never guesses a host. An earlier version of this SDK defaulted
	// streaming ON against a host that does not resolve, so every Go process
	// ran an infinite reconnect loop and logged a warning each cycle.
	StreamingEnabled bool

	// PollingInterval is how often the datafile is refetched. Default 30s,
	// minimum 5s, with plus or minus 20% jitter so a fleet does not hit the
	// CDN in lockstep. Set PollingDisabled to turn it off.
	PollingInterval time.Duration

	// PollingDisabled stops the background refresh. The SDK then serves
	// whatever datafile it loaded at startup until the process restarts, so
	// use it only with Bootstrap or with streaming.
	PollingDisabled bool

	// HTTPTimeout bounds every datafile fetch and heartbeat. Default 10s.
	// There is no "no timeout" setting on purpose: an earlier version used
	// http.DefaultClient with none, so a hung connection hung OnReady forever.
	HTTPTimeout time.Duration

	// Logger is the structured logger for SDK messages. Defaults to
	// slog.Default(), so real problems (a bad key, a 404 datafile, dropped
	// events) are visible with no configuration at all.
	Logger *slog.Logger

	// StickyBucketService persists variation assignments so a visitor keeps
	// the variation they were first bucketed into. See StickyBucketService for
	// the pipeline and the save rule.
	StickyBucketService StickyBucketService

	// BanditModelStore supplies trained bandit models. Without one, bandit
	// rules still dispatch: they run against a deterministic cold-start model
	// derived from the datafile's bandit config, which serves the lowest
	// action id. They are never silently skipped.
	BanditModelStore core.BanditModelStore

	// IncludeReasons turns on the ordered decision trail on every evaluation
	// (Flag.Reasons). It is off by default because the strings are built
	// eagerly; turn it on in staging, or per call with WithReasons(true).
	IncludeReasons bool

	// Events tunes the batched event tracker: flush cadence, batch size, queue
	// bound, retries. The zero value is a sensible default.
	Events EventOptions
}

// Option is a functional option applied to one evaluation.
type Option func(*callOpts)

type callOpts struct {
	ctx              core.EvalContext
	includeReasons   *bool
	suppressExposure bool
}

// WithContext supplies the evaluation context for this call.
//
// When it is omitted, the SDK looks for a context on the Go context.Context
// argument instead, which is how the HTTP middleware passes one down. When
// neither is present the evaluation runs against an empty context, which
// buckets every request identically, and the SDK warns once naming the call to
// add.
func WithContext(evalCtx core.EvalContext) Option {
	return func(o *callOpts) {
		o.ctx = evalCtx
	}
}

// WithReasons turns the decision trail on or off for this call, overriding
// ServerOptions.IncludeReasons.
func WithReasons(enabled bool) Option {
	return func(o *callOpts) {
		o.includeReasons = &enabled
	}
}

// WithoutExposure evaluates without recording an exposure event.
//
// Use it for a read that is not a real impression: rendering an internal
// dashboard, warming a cache, or reading the same flag twice in one request. An
// exposure the visitor never saw dilutes the experiment.
func WithoutExposure() Option {
	return func(o *callOpts) {
		o.suppressExposure = true
	}
}

func applyCallOpts(opts []Option) callOpts {
	co := callOpts{}
	for _, o := range opts {
		o(&co)
	}
	return co
}
