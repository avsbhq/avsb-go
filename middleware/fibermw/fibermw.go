// Package fibermw provides a fiber middleware for the A vs B Go SDK.
//
// Requires: github.com/gofiber/fiber/v2
//
// Usage:
//
//	app := fiber.New()
//	app.Use(fibermw.Middleware(server, func(c *fiber.Ctx) avsb.EvalContext {
//	    return avsb.UserContext(c.Get("X-User-ID"), nil)
//	}))
package fibermw

import (
	"github.com/gofiber/fiber/v2"
	avsb "github.com/avsbhq/avsb-go"
)

const evalContextKey = "avsb_eval_context"

// Middleware returns a fiber.Handler that injects the EvalContext into each
// fiber context's Locals store.
func Middleware(server *avsb.Server, contextFn func(*fiber.Ctx) avsb.EvalContext) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if contextFn != nil {
			evalCtx := contextFn(c)
			c.Locals(evalContextKey, evalCtx)
		}
		return c.Next()
	}
}

// EvalContextFrom retrieves the EvalContext stored by the middleware, or nil.
func EvalContextFrom(c *fiber.Ctx) avsb.EvalContext {
	v := c.Locals(evalContextKey)
	if v == nil {
		return nil
	}
	ec, _ := v.(avsb.EvalContext)
	return ec
}

var _ *avsb.Server
