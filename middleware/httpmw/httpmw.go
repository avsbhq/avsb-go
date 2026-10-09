// Package httpmw is the net/http middleware for the A vs B Go SDK. It works
// with any router that speaks func(http.Handler) http.Handler, which includes
// go-chi, gorilla/mux, and http.ServeMux.
//
// It resolves the evaluation context ONCE per request and attaches it to the
// request context, so handlers evaluate flags with no plumbing:
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/", checkoutHandler)
//
//	handler := httpmw.Middleware(func(r *http.Request) avsb.EvalContext {
//	    return avsb.UserContext(userIDFrom(r), map[string]any{"plan": planFrom(r)})
//	})(mux)
//
//	http.ListenAndServe(":8080", handler)
//
// Then, inside any handler:
//
//	flag := server.GetBoolFlag(r.Context(), "checkout_v2", false)
//
// There is no Server argument: the middleware does not evaluate anything, it
// only carries identity. An earlier version took one and never used it, which
// is why its doc comment, its signature, and the README each described a
// different call.
package httpmw

import (
	"context"
	"net/http"

	avsb "github.com/avsbhq/avsb-go"
)

// Middleware returns a net/http middleware that attaches the EvalContext
// contextFn builds to each request's context.
//
// A nil contextFn passes the request through untouched, so wiring the
// middleware before the identity source exists is harmless.
func Middleware(contextFn func(*http.Request) avsb.EvalContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if contextFn != nil {
				if evalCtx := contextFn(r); evalCtx != nil {
					r = r.WithContext(avsb.ContextWithEvalContext(r.Context(), evalCtx))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// EvalContextFrom returns the EvalContext the middleware attached, or nil.
//
// Handlers rarely need it: passing r.Context() to a Get*Flag call is enough.
// Reach for it when you want to add an attribute to the request's identity
// before evaluating.
func EvalContextFrom(ctx context.Context) avsb.EvalContext {
	return avsb.EvalContextFrom(ctx)
}

// SetEvalContext attaches an EvalContext to a context, for building identity
// from several sources (a session, then request headers).
func SetEvalContext(ctx context.Context, evalCtx avsb.EvalContext) context.Context {
	return avsb.ContextWithEvalContext(ctx, evalCtx)
}
