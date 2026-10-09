// Package echomw is the echo middleware for the A vs B Go SDK.
//
// It is its own Go module, so `go get github.com/avsbhq/avsb-go` does not pull
// echo into your build graph. Install it alongside the SDK:
//
//	go get github.com/avsbhq/avsb-go/middleware/echomw
//
// Usage:
//
//	e := echo.New()
//	e.Use(echomw.Middleware(func(c echo.Context) avsb.EvalContext {
//	    return avsb.UserContext(userIDFrom(c), nil)
//	}))
//
//	e.GET("/checkout", func(c echo.Context) error {
//	    flag := server.GetBoolFlag(c.Request().Context(), "checkout_v2", false)
//	    return c.JSON(200, map[string]any{"newCheckout": flag.IsEnabled()})
//	})
//
// The identity is attached to the request's Go context, so any Get*Flag call
// that receives c.Request().Context() picks it up with no further wiring.
package echomw

import (
	"github.com/labstack/echo/v4"

	avsb "github.com/avsbhq/avsb-go"
)

// evalContextKey is the echo-context key the EvalContext is also stored under.
const evalContextKey = "avsb_eval_context"

// Middleware returns an echo.MiddlewareFunc that attaches the EvalContext
// contextFn builds to the request.
//
// A nil contextFn passes the request through untouched.
func Middleware(contextFn func(echo.Context) avsb.EvalContext) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if contextFn != nil {
				if evalCtx := contextFn(c); evalCtx != nil {
					c.Set(evalContextKey, evalCtx)
					// Also on the request's Go context, which is what the SDK
					// reads.
					if req := c.Request(); req != nil {
						c.SetRequest(req.WithContext(
							avsb.ContextWithEvalContext(req.Context(), evalCtx),
						))
					}
				}
			}
			return next(c)
		}
	}
}

// EvalContextFrom returns the EvalContext the middleware attached, or nil.
func EvalContextFrom(c echo.Context) avsb.EvalContext {
	if v := c.Get(evalContextKey); v != nil {
		if evalCtx, ok := v.(avsb.EvalContext); ok {
			return evalCtx
		}
	}
	req := c.Request()
	if req == nil {
		return nil
	}
	return avsb.EvalContextFrom(req.Context())
}
