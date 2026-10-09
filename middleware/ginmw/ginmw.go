// Package ginmw is the gin middleware for the A vs B Go SDK.
//
// It is its own Go module, so `go get github.com/avsbhq/avsb-go` does not pull
// gin into your build graph. Install it alongside the SDK:
//
//	go get github.com/avsbhq/avsb-go/middleware/ginmw
//
// Usage:
//
//	r := gin.Default()
//	r.Use(ginmw.Middleware(func(c *gin.Context) avsb.EvalContext {
//	    return avsb.UserContext(c.GetString("userID"), nil)
//	}))
//
//	r.GET("/checkout", func(c *gin.Context) {
//	    flag := server.GetBoolFlag(c.Request.Context(), "checkout_v2", false)
//	    // ...
//	})
//
// The identity is attached to the request's Go context, so any Get*Flag call
// that receives c.Request.Context() picks it up with no further wiring.
// EvalContextFrom is there for the cases where you want to read or extend it.
package ginmw

import (
	"github.com/gin-gonic/gin"

	avsb "github.com/avsbhq/avsb-go"
)

// evalContextKey is the gin-context key the EvalContext is also stored under,
// for handlers that prefer c.Get over the request context.
const evalContextKey = "avsb_eval_context"

// Middleware returns a gin.HandlerFunc that attaches the EvalContext contextFn
// builds to the request.
//
// A nil contextFn passes the request through untouched.
func Middleware(contextFn func(*gin.Context) avsb.EvalContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contextFn != nil {
			if evalCtx := contextFn(c); evalCtx != nil {
				c.Set(evalContextKey, evalCtx)
				// Also on the request's Go context, which is what the SDK
				// reads. Setting only the gin key left the SDK evaluating
				// every request as anonymous while the middleware looked wired.
				if c.Request != nil {
					c.Request = c.Request.WithContext(
						avsb.ContextWithEvalContext(c.Request.Context(), evalCtx),
					)
				}
			}
		}
		c.Next()
	}
}

// EvalContextFrom returns the EvalContext the middleware attached, or nil.
func EvalContextFrom(c *gin.Context) avsb.EvalContext {
	if v, ok := c.Get(evalContextKey); ok {
		if evalCtx, ok := v.(avsb.EvalContext); ok {
			return evalCtx
		}
	}
	if c.Request == nil {
		return nil
	}
	return avsb.EvalContextFrom(c.Request.Context())
}
