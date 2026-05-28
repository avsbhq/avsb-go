# @avsbhq/avsb-go

Official Go SDK for the [A vs B](https://avsb.cloud) feature-flag and experimentation platform.

## Quick start

```go
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

    ctx := context.Background()
    if err := server.OnReady(ctx); err != nil {
        log.Fatal(err)
    }
    defer server.Close()

    evalCtx := avsb.UserContext("u_1", map[string]any{"plan": "pro"})
    flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))
    if flag.IsEnabled() {
        // serve new checkout
    }
}
```

## Installation

```bash
go get github.com/avsbhq/avsb-go
```

## Flag types

| Method | Return type |
|---|---|
| `GetBoolFlag` | `Flag[bool]` |
| `GetStringFlag` | `Flag[string]` |
| `GetNumberFlag` | `Flag[float64]` |
| `GetJSONFlag` | `Flag[json.RawMessage]` |

Every method accepts `...Option` varargs for per-call context and reasons control.

## Contexts

```go
// Single user context
evalCtx := avsb.UserContext("u_1", map[string]any{"plan": "pro"})

// Multi-context (user + organization)
evalCtx := avsb.MultiContext(
    avsb.UserContext("u_1", nil),
    avsb.OrgContext("acme", map[string]any{"tier": "enterprise"}),
)
```

## Middleware

| Package | Framework |
|---|---|
| `middleware/httpmw` | `net/http` |
| `middleware/chimw` | `go-chi` |
| `middleware/ginmw` | `gin` |
| `middleware/echomw` | `echo` |
| `middleware/fibermw` | `fiber` |

## Sticky bucketing

```go
import "github.com/avsbhq/avsb-go/sticky"

server, _ := avsb.NewServer(avsb.ServerOptions{
    SDKKey:              os.Getenv("AVSB_SDK_KEY"),
    StickyBucketService: sticky.NewInMemoryService(),
})
```

Redis, DynamoDB, and PostgreSQL adapters are available with build tags:

```bash
go build -tags redis     ./...
go build -tags dynamodb  ./...
go build -tags postgres  ./...
```

## OpenFeature

```go
import of "github.com/open-feature/go-sdk/openfeature"

of.SetProvider(server.OpenFeatureProvider())
client := of.NewClient("my-app")
```

## Conformance

The `conformance/` package runs the shared cross-language fixture suite and verifies that the Go evaluator produces bit-identical decisions to the TypeScript reference implementation.

```bash
go test ./conformance/...
```

## Requirements

- Go 1.22+
- No external dependencies in the core evaluation path
