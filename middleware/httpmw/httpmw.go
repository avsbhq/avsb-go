// Package httpmw provides a net/http middleware for the A vs B Go SDK.
//
// Usage:
//
//	mux := http.NewServeMux()
//	handler := httpmw.Middleware(server)(mux)
//	http.ListenAndServe(":8080", handler)
//
// Inside a handler retrieve the evaluated context key via:
//
//	evalCtx := httpmw.EvalContextFrom(r.Context())
package httpmw

import (
	"context"
	"net/http"

	avsb "github.com/avsbhq/avsb-go"
)

type contextKey struct{}

// Middleware wraps an http.Handler, extracting the eval context from the
// request and making it available to downstream handlers via r.Context().
//
// The middleware itself does not evaluate any flags — it only injects the
// EvalContext so handlers can call server.GetBoolFlag(r.Context(), ...) with
// a pre-built context.
func Middleware(server *avsb.Server, contextFn func(*http.Request) avsb.EvalContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if contextFn != nil {
				evalCtx := contextFn(r)
				ctx := context.WithValue(r.Context(), contextKey{}, evalCtx)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// EvalContextFrom retrieves the EvalContext stored by the middleware, or nil
// when not set.
func EvalContextFrom(ctx context.Context) avsb.EvalContext {
	v := ctx.Value(contextKey{})
	if v == nil {
		return nil
	}
	ec, _ := v.(avsb.EvalContext)
	return ec
}

// SetEvalContext stores an EvalContext into the given context. Useful when
// building the context from multiple sources (session + request headers).
func SetEvalContext(ctx context.Context, evalCtx avsb.EvalContext) context.Context {
	return context.WithValue(ctx, contextKey{}, evalCtx)
}

// Ensure the server is used so the import is not flagged as unused by tools
// that don't parse the Middleware function type.
var _ *avsb.Server
