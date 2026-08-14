# avsb-go

Official Go SDK for [A vs B](https://app.avsb.cloud), the feature-flag and experimentation platform.

Flags are evaluated locally, in microseconds, against a datafile the SDK fetches from a global CDN. There is no relay to run and no network call per evaluation.

Go 1.22+. **The root module has zero dependencies.** `context.Context` propagation throughout. Generics for `Flag[T]`.

---

## 1. Install

```bash
go get github.com/avsbhq/avsb-go
```

Framework glue lives in separate modules so nothing you do not use ends up in your build graph. Install one only if you want it:

```bash
go get github.com/avsbhq/avsb-go/middleware/ginmw
go get github.com/avsbhq/avsb-go/middleware/echomw
go get github.com/avsbhq/avsb-go/middleware/fibermw
go get github.com/avsbhq/avsb-go/openfeature
```

`net/http` and go-chi need nothing extra: both use `middleware/httpmw`, which ships with the SDK because it is standard library only.

---

## 2. Quickstart

```go
package main

import (
    "context"
    "log"
    "os"

    avsb "github.com/avsbhq/avsb-go"
)

func main() {
    server, err := avsb.NewServer(avsb.ServerOptions{
        SDKKey: os.Getenv("AVSB_SDK_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }
    defer server.Close()

    ctx := context.Background()
    if err := server.OnReady(ctx); err != nil {
        log.Fatal(err)
    }

    evalCtx := avsb.UserContext("u_123", map[string]any{"plan": "pro"})
    flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))

    if flag.IsEnabled() {
        // serve the new checkout
    }
    log.Printf("variation=%s source=%s", flag.VariationKey(), flag.Source())
}
```

Create **one** `Server` per process and share it across goroutines. A second instance means a second datafile fetch, a second polling loop, and two event queues each holding half your exposures.

Call `Close` before the process exits, or the last batch of exposures and conversions never leaves.

### What you get without configuring anything

- The datafile is fetched from `https://cdn.avsb.cloud/{sdkKey}/datafile.json` and refreshed every 30 seconds with jitter.
- Every HTTP call has a 10 second timeout.
- Exposures are recorded automatically on `ab_test`, `bandit`, and holdout decisions, batched, and flushed on `Close`.
- A heartbeat reports liveness, so the dashboard's Environments page shows this service as connected.
- Failures are loud: a malformed SDK key, a 404 datafile, an empty evaluation context, and a dropped event batch each log a line naming what to fix.

---

## 3. SDK keys

Copy the **environment SDK key** from the A vs B dashboard: Environments in the project sidebar. It looks like `sdk_production_ttqm0eaj4vth1krcb2xn`.

```bash
export AVSB_SDK_KEY=sdk_production_ttqm0eaj4vth1krcb2xn
```

```go
server, err := avsb.NewServer(avsb.ServerOptions{
    SDKKey: os.Getenv("AVSB_SDK_KEY"),
})
```

The SDK key is a **public identifier**. It names an environment and grants read access to that environment's published flag configuration, nothing more. It is still worth keeping out of a public repository, because rotating it means republishing every deployment.

The key's shape is checked at construction. A key that does not look like one is logged with what is wrong and used anyway, because the mint format can grow and the datafile fetch is the real arbiter:

```
avsb: SDK key "https://app.a...ents" does not look like an SDK key: it looks like a URL,
not a key. Copy the key itself, not the address of the page it is on. Keys look like
sdk_production_ttqm0eaj4vth1krcb2xn (sdk_<environment>_<id>). ...
```

---

## 4. Identity

Every evaluation takes an `EvalContext`, built with one of four factories:

```go
// A person.
evalCtx := avsb.UserContext("u_123", map[string]any{
    "plan":        "pro",
    "beta_tester": true,
})

// An account.
orgCtx := avsb.OrgContext("acme-corp", map[string]any{"tier": "enterprise"})

// Any other kind your project declares.
deviceCtx := avsb.SingleContext("device", "d_abc", map[string]any{"os": "linux"})
```

There is no persistent identity on `Server`. Each request builds its own context and passes it in, which is idiomatic Go: no global state, safe across goroutines.

Evaluating with no identity buckets every request identically, so the SDK warns once per process naming the call to add.

### Multi-context

```go
evalCtx := avsb.MultiContext(
    avsb.UserContext("u_123", nil),
    avsb.OrgContext("acme-corp", map[string]any{"tier": "enterprise"}),
)

flag := server.GetBoolFlag(ctx, "enterprise-feature", false, avsb.WithContext(evalCtx))
```

A rule with `hashAttribute: "organization.key"` then buckets by account, so every member of an account sees the same thing. A rule with `"user.key"` buckets by person. Both work from the same call.

---

## 5. Reading flags

### The typed getters

```go
func (s *Server) GetBoolFlag(ctx context.Context, key string, defaultValue bool, opts ...Option) Flag[bool]
func (s *Server) GetStringFlag(ctx context.Context, key string, defaultValue string, opts ...Option) Flag[string]
func (s *Server) GetNumberFlag(ctx context.Context, key string, defaultValue float64, opts ...Option) Flag[float64]
func (s *Server) GetJSONFlag(ctx context.Context, key string, defaultValue json.RawMessage, opts ...Option) Flag[json.RawMessage]
func (s *Server) GetAllFlags(ctx context.Context, opts ...Option) map[string]Flag[json.RawMessage]
```

A default is required on every call. It comes back untouched, with its type intact, when the flag is missing, the SDK is not ready, or the served value does not fit the getter that asked for it. **Type mismatches fall back to your default and log; they never coerce.** Reading a string flag with `GetBoolFlag` does not give you `true`.

Numbers are `float64` because that is what JSON carries: a flag holding `3` comes back as `3.0`.

`GetJSONFlag` hands you the raw JSON to unmarshal into your own type:

```go
type CheckoutConfig struct {
    Retries   int `json:"retries"`
    TimeoutMs int `json:"timeoutMs"`
}

flag := server.GetJSONFlag(ctx, "checkout-config", []byte(`{"retries":3,"timeoutMs":250}`),
    avsb.WithContext(evalCtx))

var config CheckoutConfig
if err := json.Unmarshal(flag.Value(), &config); err != nil {
    // the datafile value did not fit your type
}
```

### `Flag[T]`

```go
type Flag[T any] struct{ /* unexported */ }

func (f Flag[T]) Value() T                // the evaluated value, or your default
func (f Flag[T]) VariationKey() string    // the served variation, "" when none
func (f Flag[T]) Source() string          // why this value was served (see below)
func (f Flag[T]) RuleID() string          // the matched rule or holdout id, "" when none
func (f Flag[T]) RuleType() string        // "ab_test" | "targeted_delivery" | "holdout" | "bandit" | ""
func (f Flag[T]) Reasons() []string       // the ordered decision trail, never nil
func (f Flag[T]) FlagKey() string         // the key this result was evaluated for
func (f Flag[T]) EvaluatedAt() time.Time  // when the decision was made
func (f Flag[T]) DurationMicros() int64   // how long it took
func (f Flag[T]) IsEnabled() bool         // a real decision produced a truthy value
func (f Flag[T]) Exists() bool            // the key is in the datafile
```

### `Source()`: exactly ten values

| Source | Meaning |
|---|---|
| `datafileOverride` | a datafile-declared user override matched |
| `runtimeOverride` | an override set on this client matched |
| `sticky` | a stored sticky assignment was served |
| `rule` | a `targeted_delivery` or `ab_test` rule matched |
| `holdout` | the context is in a holdout cohort |
| `bandit` | a bandit rule selected an action |
| `default` | no rule matched, the flag's default variation was served |
| `not_found` | the datafile loaded and this key is not in it |
| `disabled` | the flag exists but is switched off |
| `not_ready` | there was no datafile to look in yet |

Each is exported as a constant on the `core` package (`core.SourceRule`, `core.SourceNotReady`, and so on).

**`not_found` and `not_ready` are different answers.** `not_found` means the key is absent from a datafile the SDK has. `not_ready` means it has no datafile: `OnReady` has not finished, the fetch failed with no bootstrap, or the client was closed. Nothing about the key is knowable, so `Exists()` is false and `IsEnabled()` is false even when your default is `true`.

### `IsEnabled()`

True when `Source()` is one of `rule`, `holdout`, `bandit`, `runtimeOverride`, `datafileOverride`, or `sticky`, **and** the value is truthy. False otherwise, with no exceptions. The definition is identical in every A vs B SDK and is locked by the shared conformance suite.

The trap worth knowing: **a flag serving its default variation is not enabled**, however truthy that default is. "Enabled" means something decided to serve it.

Truthiness follows the JavaScript rule, because the reference engine is TypeScript and a helper that answers differently per language is not portable. `false`, `0`, `NaN`, `""` and `null` are falsy; everything else is truthy. So the string `"false"` is truthy (it is not parsed), the string `"0"` is truthy, and an empty JSON array or object is truthy.

### Per-call options

```go
flag := server.GetBoolFlag(ctx, "checkout-v2", false,
    avsb.WithContext(evalCtx),      // who this evaluation is for
    avsb.WithReasons(true),         // populate flag.Reasons() for this call
    avsb.WithoutExposure(),         // do not record an impression
)
```

`WithoutExposure()` is for a read that is not a real impression: an internal dashboard, a cache warm, or reading the same flag twice in one request. An exposure the visitor never saw dilutes the experiment.

### Reasons

Reasons are off by default because the strings are built eagerly. Turn them on globally in staging, or per call in production:

```go
server, _ := avsb.NewServer(avsb.ServerOptions{
    SDKKey:         os.Getenv("AVSB_SDK_KEY"),
    IncludeReasons: true,
})
```

They read like this, and they are the same strings in every A vs B SDK:

```
rule "rule_paid_users" skipped (audience not matched)
rule "rule_ab_split" matched (ab_test), bucketed into variation "treatment" (hash 4336/5000)
```

---

## 6. Tracking events

### Conversions

```go
revenue := 49.99

server.Track(ctx, "purchase_completed", avsb.TrackEvent{
    Context: evalCtx,   // its primary key becomes the visitor id
    Revenue: &revenue,  // decimal major units (49.99, not 4999), nil for a non-revenue conversion
})
```

`eventName` is the metric key configured in the dashboard. An unknown key is accepted and stored, so a typo shows up as a metric with no data rather than an error: copy the key from the dashboard.

Events are queued and flushed in batches. There is deliberately **no properties field**: the metric endpoint has none, and anything sent under that name is stripped before storage, so it would look delivered and be gone. Put a number in `Value` instead.

```go
type TrackEvent struct {
    Context   avsb.EvalContext // identity; its primary key becomes visitorId
    VisitorID string           // sets the visitor id directly, overriding Context
    Revenue   *float64         // decimal major units, or nil
    Value     *float64         // numeric value for average-value metrics, or nil
}
```

### Purchases

```go
price, quantity := 99.95, 1

err := server.TrackPurchase(ctx, "u_123", avsb.PurchaseOrder{
    OrderID:  "ORD-1001",
    Total:    99.95,         // decimal major units as displayed, must be > 0
    Currency: "USD",
    Items: []avsb.PurchaseItem{
        {SKU: "SKU-1", Price: &price, Quantity: &quantity},
    },
})
```

Purchases are sent immediately rather than batched, because an order is the event least worth buffering. `TrackPurchase` blocks until the send has been attempted, with the same retry ladder as a batch flush, and returns the error so you can log it. Run it in a goroutine if the request path must not wait.

### Delivery

```go
server.Flush(ctx)  // send everything queued now, and wait
server.Close()     // stop the loops, then flush what is left
```

Tune the tracker with `ServerOptions.Events`:

```go
Events: avsb.EventOptions{
    FlushInterval: 5 * time.Second,  // how often the queue drains on its own
    BatchSize:     20,               // flush trigger, and max events per request (capped at 100)
    MaxQueueSize:  10000,            // past this the OLDEST events are dropped, with a warning
    RetryAttempts: 3,                // retries after the first attempt, with jittered backoff
    SendTimeout:   10 * time.Second, // per attempt
    Disabled:      false,            // turn delivery off entirely
}
```

A full queue means ingestion is unreachable. Holding a process's memory hostage to that is worse than losing the stalest events, so the queue is bounded and drops from the front, loudly.

---

## 7. Exposures

An exposure is recorded automatically whenever an `ab_test` rule, a bandit rule, or a holdout allocates a context to a variation. That is what makes a server-side experiment produce results; without it, evaluation is decorative.

The visitor id on an exposure is **the identity the decision bucketed on**: the rule's `hashAttribute` for an experiment, and the holdout's own `hashAttribute` for a holdout. An organization-keyed holdout is attributed to the organization, not to whichever member happened to make the request.

Attributes on an exposure are filtered to the attributes registered in the dashboard. Registering an attribute is what makes it queryable; unregistered keys are dropped.

---

## 8. Errors and logging

`Server` uses `log/slog`. Pass your own logger:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

server, err := avsb.NewServer(avsb.ServerOptions{
    SDKKey: os.Getenv("AVSB_SDK_KEY"),
    Logger: logger,
})
```

The default is `slog.Default()`, so real problems are visible with no configuration at all.

`NewServer` returns `(*Server, error)`; `OnReady` returns `error`. Both are plain Go errors. `Get*Flag` never returns an error: a failed evaluation produces a `Flag[T]` carrying your default and a `Source()` that says why.

Failure messages name the URL, the status, and a command to reproduce:

```
avsb: OnReady could not load a datafile: the datafile at
https://cdn.avsb.cloud/sdk_production_abc123/datafile.json returned 404. That means the
SDK key sdk_producti...c123 has no published datafile: check the key in Settings, then
Environments, and publish the environment at least once. Reproduce with:
curl -i https://cdn.avsb.cloud/sdk_production_abc123/datafile.json
```

When `OnReady` fails the server stays usable: every flag returns your default with source `not_ready`, and the polling loop keeps retrying, so a CDN blip at boot does not need a restart to recover.

---

## 9. Refresh, bootstrap, and streaming

```go
server, _ := avsb.NewServer(avsb.ServerOptions{
    SDKKey:           os.Getenv("AVSB_SDK_KEY"),
    PollingInterval:  30 * time.Second, // default; minimum 5s, jittered by 20%
    PollingDisabled:  false,
    Bootstrap:        datafileBytes,    // serve immediately, with no network call
    StreamingEnabled: false,            // default
})
```

`Bootstrap` takes a datafile you already hold, as raw JSON. Evaluation works immediately with no network call, which is what makes tests, air-gapped deployments, and CI runs possible. With `SDKKey` also set, the bootstrap serves traffic while the first fetch runs.

**Streaming is off by default.** Set `StreamingEnabled` to open an SSE connection so flag changes arrive in about a second instead of on the polling interval. The endpoint is server controlled: the datafile publishes it. When neither the datafile nor `StreamingEndpoint` names one, the SDK says so once and stays on polling. It never guesses a host.

---

## 10. HTTP middleware

The middleware resolves identity once per request and attaches it to the request context. Handlers then evaluate with no plumbing.

### `net/http` and go-chi

```go
import "github.com/avsbhq/avsb-go/middleware/httpmw"

mux := http.NewServeMux()
mux.HandleFunc("/checkout", checkoutHandler)

handler := httpmw.Middleware(func(r *http.Request) avsb.EvalContext {
    return avsb.UserContext(userIDFrom(r), map[string]any{"plan": planFrom(r)})
})(mux)

http.ListenAndServe(":8080", handler)
```

```go
func checkoutHandler(w http.ResponseWriter, r *http.Request) {
    newCheckout := server.GetBoolFlag(r.Context(), "checkout-v2", false)
    oneClick := server.GetBoolFlag(r.Context(), "one-click-buy", false)

    tmpl.Execute(w, map[string]any{
        "NewCheckout": newCheckout.IsEnabled(),
        "OneClick":    oneClick.IsEnabled(),
    })
}
```

go-chi speaks the same `func(http.Handler) http.Handler` shape, so `r.Use(httpmw.Middleware(...))` works directly. There is no separate chi package.

### gin, echo, fiber

```go
// gin
r.Use(ginmw.Middleware(func(c *gin.Context) avsb.EvalContext {
    return avsb.UserContext(c.GetString("userID"), nil)
}))
flag := server.GetBoolFlag(c.Request.Context(), "checkout-v2", false)

// echo
e.Use(echomw.Middleware(func(c echo.Context) avsb.EvalContext {
    return avsb.UserContext(userIDFrom(c), nil)
}))
flag := server.GetBoolFlag(c.Request().Context(), "checkout-v2", false)

// fiber (no *http.Request, so identity rides the Ctx's user context)
app.Use(fibermw.Middleware(func(c *fiber.Ctx) avsb.EvalContext {
    return avsb.UserContext(c.Get("X-User-ID"), nil)
}))
flag := server.GetBoolFlag(c.UserContext(), "checkout-v2", false)
```

| Router | Import path | Module |
|---|---|---|
| `net/http`, go-chi | `github.com/avsbhq/avsb-go/middleware/httpmw` | ships with the SDK |
| gin | `github.com/avsbhq/avsb-go/middleware/ginmw` | separate |
| echo | `github.com/avsbhq/avsb-go/middleware/echomw` | separate |
| fiber | `github.com/avsbhq/avsb-go/middleware/fibermw` | separate |

An explicit `avsb.WithContext(...)` on a call always wins over the middleware's identity.

---

## 11. Sticky bucketing

Sticky bucketing keeps a visitor in the variation they were first bucketed into, even when traffic weights change underneath them.

```go
import "github.com/avsbhq/avsb-go/sticky"

server, _ := avsb.NewServer(avsb.ServerOptions{
    SDKKey:              os.Getenv("AVSB_SDK_KEY"),
    StickyBucketService: sticky.NewInMemoryService(),
})
```

The pipeline runs in this order:

1. runtime override (client)
2. sticky lookup (client)
3. datafile override (evaluator)
4. holdout (evaluator)
5. rules in declared order (evaluator)
6. the flag's default variation (evaluator)

An assignment is saved **only** when a decision came from an `ab_test` rule. A `targeted_delivery` rollout is not an assignment, and a holdout, an override, or a default is not the visitor's to keep.

Write your own backend against this interface:

```go
type StickyBucketService interface {
    Lookup(bucketingID, flagKey string) *core.StickyAssignment
    Save(bucketingID, flagKey string, assignment core.StickyAssignment)
}
```

Redis, DynamoDB, and Postgres implementations ship behind build tags so the root module stays dependency free:

```bash
go build -tags redis ./...
go build -tags dynamodb ./...
go build -tags postgres ./...
```

Use a shared store for anything running more than one process. An in-memory store on three replicas gives a visitor three different assignments, which is worse than no sticky bucketing at all.

---

## 12. Runtime overrides

Force an identity, or everyone, onto a variation until it is cleared or the process restarts. Nothing is persisted and nothing is published: this is a local QA and incident tool, not a targeting rule.

```go
server.SetOverrideForUser("checkout-v2", "u_123", "on")
server.SetOverrideForUser("checkout-v2", "acme", "on", "organization")
server.SetGlobalOverride("checkout-v2", "off")

server.ClearOverrideForUser("checkout-v2", "u_123")
server.ClearGlobalOverride("checkout-v2")
server.ClearAllOverrides()
```

A per-identity override beats a global one, and the `user` identity is checked before any other kind. An override naming a variation the flag does not have resolves to nothing rather than to a broken value.

---

## 13. Bandits

A rule of type `bandit` dispatches through the bandit engine automatically. With no trained model it runs a deterministic cold-start policy (the lowest action id) rather than being skipped, so a configured bandit always serves something and always says what it did in `Reasons()`.

Supply trained models with `ServerOptions.BanditModelStore`.

---

## 14. Decision logging

Forward every decision to your own pipeline:

```go
type DecisionLog interface {
    LogDecision(entry avsb.DecisionLogEntry)
}

server.SetDecisionLog(myLogger)
```

`LogDecision` runs synchronously on the hot path, so it must not block: write to a buffered channel and drain it elsewhere.

For local debugging there is a bounded in-memory collector:

```go
log := avsb.NewMemoryDecisionLog(1000)
server.SetDecisionLog(log)

for _, entry := range log.Drain() {
    fmt.Printf("%s -> %s (%s)\n", entry.FlagKey, entry.VariationKey, entry.Source)
}
```

`ContextSummary` on an entry carries kind and key pairs only. Attributes are deliberately excluded: they may carry personal data, and a decision log is often shipped somewhere with looser access than the request that produced it.

---

## 15. Graceful shutdown

```go
server, _ := avsb.NewServer(avsb.ServerOptions{SDKKey: os.Getenv("AVSB_SDK_KEY")})
defer server.Close()

quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
go func() {
    <-quit
    server.Close()
    os.Exit(0)
}()
```

`Close` stops the polling loop and the streaming connection, then flushes queued events. After it returns, every evaluation reports source `not_ready`.

---

## 16. Testing

`NewTestServer` loads a datafile you supply with no network of any kind: no fetch, no polling, no streaming, no event delivery. Reasons are on, so a failing assertion tells you which rule decided.

```go
var testDatafile = []byte(`{
  "version": 2,
  "sdkKey": "sdk_test_local0000000001",
  "collectEndpoint": "https://ingest.avsb.cloud",
  "flags": [{
    "id": "flag_checkout", "key": "checkout-v2", "type": "boolean", "enabled": true,
    "defaultVariationId": "var_off",
    "variations": [
      {"id": "var_off", "key": "off", "value": false},
      {"id": "var_on",  "key": "on",  "value": true}
    ],
    "overrides": [],
    "rules": [{
      "id": "rule_full", "type": "targeted_delivery", "enabled": true, "audienceIds": [],
      "hashAttribute": "user.key", "trafficAllocation": null,
      "variations": [{"variationId": "var_on", "percentage": 1}]
    }]
  }],
  "audiences": []
}`)

func TestCheckoutFlag(t *testing.T) {
    server, err := avsb.NewTestServer(testDatafile)
    if err != nil {
        t.Fatal(err)
    }
    defer server.Close()

    flag := server.GetBoolFlag(context.Background(), "checkout-v2", false,
        avsb.WithContext(avsb.UserContext("alice", nil)))

    if !flag.IsEnabled() {
        t.Errorf("expected the flag on; reasons: %v", flag.Reasons())
    }
}
```

`NewTestServerFromDatafile(*core.Datafile)` takes a struct instead of JSON.

---

## 17. OpenFeature

```go
import (
    avsb "github.com/avsbhq/avsb-go"
    avsbof "github.com/avsbhq/avsb-go/openfeature"
    of "github.com/open-feature/go-sdk/openfeature"
)

of.SetProvider(avsbof.NewProvider(server))
client := of.NewClient("my-app")

value, _ := client.BooleanValue(ctx, "checkout-v2", false, of.EvaluationContext{
    TargetingKey: "u_123",
    Attributes: map[string]any{
        "plan":              "pro",
        "organization.key":  "acme-corp",
        "organization.tier": "enterprise",
    },
})
```

OpenFeature has no multi-context concept, so **dotted attribute names build the sub-contexts**: `organization.key` and `organization.tier` produce an organization sub-context, and a rule with `hashAttribute: "organization.key"` buckets by account. Without dots the mapping stays a plain single context.

`not_ready` maps to `PROVIDER_NOT_READY` and `not_found` to `FLAG_NOT_FOUND`, so a client can tell "you have not initialised yet" from "that flag does not exist".

---

## 18. Migrating

### From the LaunchDarkly Go SDK

```go
// LaunchDarkly
client, _ := ld.MakeClient("sdk-key", 5*time.Second)
defer client.Close()

user := ldcontext.New("alice")
value, _ := client.BoolVariation("checkout-v2", user, false)
```

```go
// A vs B
server, _ := avsb.NewServer(avsb.ServerOptions{SDKKey: os.Getenv("AVSB_SDK_KEY")})
defer server.Close()
_ = server.OnReady(ctx)

evalCtx := avsb.UserContext("alice", nil)
value := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx)).Value()
```

```go
// LaunchDarkly multi-context
multi := ldcontext.NewMultiBuilder().
    Add(ldcontext.New("alice")).
    Add(ldcontext.NewWithKind("organization", "acme")).
    Build()
```

```go
// A vs B multi-context
evalCtx := avsb.MultiContext(
    avsb.UserContext("alice", nil),
    avsb.OrgContext("acme", map[string]any{"tier": "enterprise"}),
)
```

Differences worth knowing:

- `Get*Flag` returns a `Flag[T]` with full metadata; `.Value()` is the raw value.
- `context.Context` is always the first argument. There is no separate context wrapper type.
- `OnReady(ctx)` blocks until a datafile is loaded or `ctx` is done. Cancel or deadline the context to bound it.

### From the Statsig Go SDK

```go
// Statsig
statsig.Initialize("secret-key", nil)
user := statsig.User{UserID: "alice", Custom: map[string]any{"plan": "pro"}}
gate := statsig.CheckGate(user, "checkout-v2")
```

```go
// A vs B
evalCtx := avsb.UserContext("alice", map[string]any{"plan": "pro"})
gate := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx)).IsEnabled()
```

---

## 19. Conformance

The `core` sub-package is a port of the TypeScript reference engine, locked to it by a shared fixture corpus: values, variation keys, sources, rule ids, rule types, **and the ordered reason strings** are all compared exactly. Run it with the rest of the suite:

```bash
./scripts/check.sh          # gofmt, go vet, go test across every module
./scripts/check.sh --fix    # rewrite formatting first
./scripts/check.sh --tidy   # also refresh go.mod and go.sum (needs the network)
```

A missing fixture fails the run loudly rather than skipping, including in the published standalone repository, where a vendored copy under `conformance/testdata` is used.
