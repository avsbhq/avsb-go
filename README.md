# avsb-go

Official Go SDK for the [A vs B](https://app.avsb.cloud) feature-flag and experimentation platform.

Go 1.22+. No external dependencies on the core evaluation path. `context.Context` propagation throughout. Generics for `Flag[T]`.

---

## 1. Install

```bash
go get github.com/avsbhq/avsb-go
```

Module path: `github.com/avsbhq/avsb-go`

**Runtime requirement:** Go 1.22 or later.

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

    evalCtx := avsb.UserContext("u_1", map[string]any{"plan": "pro"})
    flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))

    if flag.IsEnabled() {
        // serve new checkout
    }
    log.Printf("variation=%s source=%s", flag.VariationKey(), flag.Source())
}
```

---

## 3. SDK keys

Get a **server SDK key** from your A vs B project's Environments page and supply it via an environment variable. Never expose server SDK keys to browser clients or embed them in public repositories.

```bash
export AVSB_SDK_KEY=sdk-...
```

```go
server, err := avsb.NewServer(avsb.ServerOptions{
    SDKKey: os.Getenv("AVSB_SDK_KEY"),
})
```

---

## 4. Identity

Every evaluation method accepts an `EvalContext` via `WithContext`. Build contexts with the provided factory functions:

```go
// User context (most common)
evalCtx := avsb.UserContext("u_123", map[string]any{
    "plan":       "pro",
    "beta_tester": true,
})

// Organisation context
orgCtx := avsb.OrgContext("acme-corp", map[string]any{"tier": "enterprise"})

// Arbitrary kind
devCtx := avsb.SingleContext("device", "d_abc", map[string]any{"os": "linux"})
```

There is no persistent identity on the `Server` struct. Each request constructs its own `EvalContext` and passes it via `WithContext`, which is idiomatic Go: no global state, safe for concurrent use across goroutines.

---

## 5. Multi-context

```go
evalCtx := avsb.MultiContext(
    avsb.UserContext("u_123", nil),
    avsb.OrgContext("acme-corp", map[string]any{"tier": "enterprise"}),
)

flag := server.GetBoolFlag(ctx, "enterprise-feature", false, avsb.WithContext(evalCtx))
```

`MultiContext` accepts any number of single-kind contexts. When a rule specifies `hashAttribute: "organization.key"`, all users in the same organisation receive the same variation.

---

## 6. Reading flags

### Typed methods

| Method | Return type |
|---|---|
| `GetBoolFlag(ctx, key, default, ...Option)` | `Flag[bool]` |
| `GetStringFlag(ctx, key, default, ...Option)` | `Flag[string]` |
| `GetNumberFlag(ctx, key, default, ...Option)` | `Flag[float64]` |
| `GetJSONFlag(ctx, key, default, ...Option)` | `Flag[json.RawMessage]` |

A `defaultValue` is required on every call. It is returned when the flag is not found, the SDK is not ready, or an evaluation error occurs.

### `Flag[T]` methods

```go
flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))

flag.Value()        // T: the evaluated value
flag.VariationKey() // string: matched variation key, empty when none
flag.Source()       // string: "rule" | "holdout" | "default" | "not_found" | ...
flag.RuleID()       // string: matched rule id, empty when none
flag.RuleType()     // string: "ab_test" | "targeted_delivery" | "holdout" | "bandit"
flag.Reasons()      // []string: populated when WithReasons(true) is set
flag.FlagKey()      // string: the key this flag was evaluated for
flag.IsEnabled()    // bool: source=="rule" && value is truthy
flag.Exists()       // bool: source != "not_found"
```

### Per-call options

```go
flag := server.GetBoolFlag(
    ctx, "checkout-v2", false,
    avsb.WithContext(evalCtx),
    avsb.WithReasons(true),   // populate flag.Reasons()
)
```

### Reasons audit trail

Enable reasons globally at server construction for debugging:

```go
server, _ := avsb.NewServer(avsb.ServerOptions{
    SDKKey:         os.Getenv("AVSB_SDK_KEY"),
    IncludeReasons: true,
})
```

---

## 7. Tracking events

```go
server.Track(ctx, "purchase_completed", avsb.TrackPayload{
    Context:    evalCtx,
    Value:      ptr(49.99),
    Properties: map[string]any{"plan": "pro", "currency": "usd"},
})
```

`Value` maps to the numeric metric quantity. `Properties` is forwarded as-is to the collection endpoint. Both fields are optional.

---

## 8. Error handling

`Server` uses Go's `log/slog` package for structured logging. Pass a custom `*slog.Logger` at construction:

```go
import "log/slog"
import "os"

logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelWarn,
}))

server, err := avsb.NewServer(avsb.ServerOptions{
    SDKKey: os.Getenv("AVSB_SDK_KEY"),
    Logger: logger,
})
```

`NewServer` returns `(*Server, error)`. `OnReady` returns `error`. Both use standard Go error values, not custom error types. When `OnReady` fails, the server is still usable and returns default values; the background polling loop continues to retry.

`GetBoolFlag` (and all other `Get*Flag` methods) never return an error. Evaluation failures produce a `Flag[T]` with `Source() == "not_found"` and `Value()` equal to the supplied default.

---

## 9. Pre-evaluating flags for HTTP handlers

Go templates and HTTP handlers are server-rendered by design. Evaluate flag state at the top of each handler and pass the results to the template:

```go
func checkoutHandler(w http.ResponseWriter, r *http.Request) {
    userID := getUserID(r)
    evalCtx := avsb.UserContext(userID, map[string]any{
        "plan": getPlan(r),
    })
    ctx := r.Context()

    newCheckout := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))
    oneClick    := server.GetBoolFlag(ctx, "one-click-buy", false, avsb.WithContext(evalCtx))

    tmpl.Execute(w, map[string]any{
        "NewCheckout": newCheckout.IsEnabled(),
        "OneClick":    oneClick.IsEnabled(),
    })
}
```

`Server` is safe for concurrent use across goroutines. Register it once at startup and share it across all handlers.

### `net/http` middleware

```go
import "github.com/avsbhq/avsb-go/middleware/httpmw"

mux := http.NewServeMux()
mux.HandleFunc("/", checkoutHandler)
http.ListenAndServe(":8080", httpmw.Middleware(server, mux))
```

The middleware stores a per-request `avsb.EvalContext` in `context.Context` under `httpmw.ContextKey`. Retrieve it in handlers:

```go
evalCtx := httpmw.ContextFrom(r.Context())
```

### Other supported routers

| Package | Import path |
|---|---|
| `net/http` | `github.com/avsbhq/avsb-go/middleware/httpmw` |
| go-chi | `github.com/avsbhq/avsb-go/middleware/chimw` |
| gin | `github.com/avsbhq/avsb-go/middleware/ginmw` |
| echo | `github.com/avsbhq/avsb-go/middleware/echomw` |
| fiber | `github.com/avsbhq/avsb-go/middleware/fibermw` |

---

## 10. Graceful shutdown

```go
server, _ := avsb.NewServer(avsb.ServerOptions{SDKKey: os.Getenv("AVSB_SDK_KEY")})
defer server.Close()
```

`Close` stops the background polling goroutine and the SSE streaming connection (if active), then flushes any buffered decision log entries. Call it before your process exits, typically via `defer` or a SIGTERM handler:

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
go func() {
    <-quit
    server.Close()
    os.Exit(0)
}()
```

---

## 11. Testing

Use `NewTestServer` to load a datafile inline without network access:

```go
import (
    "testing"
    avsb "github.com/avsbhq/avsb-go"
)

var testDatafile = []byte(`{
  "version": 2,
  "sdkKey": "sdk-test",
  "flags": [{
    "id": "flag_checkout-v2",
    "key": "checkout-v2",
    "type": "boolean",
    "enabled": true,
    "defaultVariationId": "var_off",
    "variations": [
      {"id": "var_off", "key": "off", "value": false},
      {"id": "var_on",  "key": "on",  "value": true}
    ],
    "rules": []
  }],
  "audiences": []
}`)

func TestCheckoutFlag(t *testing.T) {
    server, err := avsb.NewTestServer(testDatafile)
    if err != nil {
        t.Fatal(err)
    }
    defer server.Close()

    ctx := context.Background()
    evalCtx := avsb.UserContext("alice", nil)
    flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))

    if !flag.Exists() {
        t.Error("expected flag to exist")
    }
}
```

`NewTestServer` enables `IncludeReasons` automatically and skips all HTTP/SSE setup. `InMemoryDatafileLoader` marshals a `*core.Datafile` struct to JSON for the same purpose.

---

## 12. Migration from LaunchDarkly / Statsig / OpenFeature

### From LaunchDarkly Go SDK

```go
// LaunchDarkly
import ld "github.com/launchdarkly/go-server-sdk/v7"

client, _ := ld.MakeClient("sdk-key", 5*time.Second)
defer client.Close()

user := ldcontext.New("alice")
value, _ := client.BoolVariation("checkout-v2", user, false)
```

```go
// A vs B
import avsb "github.com/avsbhq/avsb-go"

server, _ := avsb.NewServer(avsb.ServerOptions{SDKKey: os.Getenv("AVSB_SDK_KEY")})
defer server.Close()
server.OnReady(context.Background())

evalCtx := avsb.UserContext("alice", nil)
flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))
value := flag.Value()
```

```go
// LaunchDarkly (multi-context)
multi := ldcontext.NewMultiBuilder().
    Add(ldcontext.New("alice")).
    Add(ldcontext.NewWithKind("organization", "acme")).
    Build()
```

```go
// A vs B (multi-context)
evalCtx := avsb.MultiContext(
    avsb.UserContext("alice", nil),
    avsb.OrgContext("acme", map[string]any{"tier": "enterprise"}),
)
```

Key differences:
- `Get*Flag` returns a `Flag[T]` struct with full metadata. Access `.Value()` for the raw value.
- `context.Context` is always the first argument; there's no separate `LDContext` wrapper.
- `OnReady` is a synchronous blocking call (or use the timeout form). No callback/channel pattern.

### From Statsig Go SDK

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

### OpenFeature provider

```go
import (
    of "github.com/open-feature/go-sdk/openfeature"
)

of.SetProvider(server.OpenFeatureProvider())
client := of.NewClient("my-app")

value, _ := client.BooleanValue(ctx, "checkout-v2", false, of.EvaluationContext{
    TargetingKey: "alice",
})
```
