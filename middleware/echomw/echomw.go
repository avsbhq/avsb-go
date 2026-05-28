// Package echomw provides an echo middleware for the A vs B Go SDK.
//
// Requires: github.com/labstack/echo/v4
//
// Usage:
//
//	e := echo.New()
//	e.Use(echomw.Middleware(server, func(c echo.Context) avsb.EvalContext {
//	    return avsb.UserContext(c.Get("userID").(string), nil)
//	}))
package echomw

import (
	"github.com/labstack/echo/v4"
	avsb "github.com/avsbhq/avsb-go"
)

const evalContextKey = "avsb_eval_context"

// Middleware returns an echo.MiddlewareFunc that injects the EvalContext into
// each request's echo context.
func Middleware(server *avsb.Server, contextFn func(echo.Context) avsb.EvalContext) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if contextFn != nil {
				evalCtx := contextFn(c)
				c.Set(evalContextKey, evalCtx)
			}
			return next(c)
		}
	}
}

// EvalContextFrom retrieves the EvalContext stored by the middleware, or nil.
func EvalContextFrom(c echo.Context) avsb.EvalContext {
	v := c.Get(evalContextKey)
	if v == nil {
		return nil
	}
	ec, _ := v.(avsb.EvalContext)
	return ec
}

var _ *avsb.Server
