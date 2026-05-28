// Package chimw provides a go-chi middleware for the A vs B Go SDK.
//
// Requires: github.com/go-chi/chi/v5
//
// Usage:
//
//	r := chi.NewRouter()
//	r.Use(chimw.Middleware(server, func(r *http.Request) avsb.EvalContext {
//	    return avsb.UserContext(getUserID(r), nil)
//	}))
package chimw

import (
	"context"
	"net/http"

	avsb "github.com/avsbhq/avsb-go"
)

type contextKey struct{}

// Middleware returns a chi-compatible middleware function that injects the
// EvalContext into each request's context.
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

// EvalContextFrom retrieves the EvalContext stored by the middleware.
func EvalContextFrom(ctx context.Context) avsb.EvalContext {
	v := ctx.Value(contextKey{})
	if v == nil {
		return nil
	}
	ec, _ := v.(avsb.EvalContext)
	return ec
}

var _ *avsb.Server
