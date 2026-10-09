// Package fibermw is the fiber middleware for the A vs B Go SDK.
//
// It is its own Go module, so `go get github.com/avsbhq/avsb-go` does not pull
// fiber into your build graph. Install it alongside the SDK:
//
//	go get github.com/avsbhq/avsb-go/middleware/fibermw
//
// Usage:
//
//	app := fiber.New()
//	app.Use(fibermw.Middleware(func(c *fiber.Ctx) avsb.EvalContext {
//	    return avsb.UserContext(c.Get("X-User-ID"), nil)
//	}))
//
//	app.Get("/checkout", func(c *fiber.Ctx) error {
//	    flag := server.GetBoolFlag(c.UserContext(), "checkout_v2", false)
//	    return c.JSON(fiber.Map{"newCheckout": flag.IsEnabled()})
//	})
//
// Fiber has no *http.Request, so the identity is attached to the Ctx's user
// context: pass c.UserContext() to a Get*Flag call and it is picked up with no
// further wiring.
package fibermw

import (
	"github.com/gofiber/fiber/v2"

	avsb "github.com/avsbhq/avsb-go"
)

// evalContextKey is the Locals key the EvalContext is also stored under.
const evalContextKey = "avsb_eval_context"

// Middleware returns a fiber.Handler that attaches the EvalContext contextFn
// builds to the request.
//
// A nil contextFn passes the request through untouched.
func Middleware(contextFn func(*fiber.Ctx) avsb.EvalContext) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if contextFn != nil {
			if evalCtx := contextFn(c); evalCtx != nil {
				c.Locals(evalContextKey, evalCtx)
				// Also on the Ctx's user context, which is what a Get*Flag
				// call reads. Setting only Locals left the SDK evaluating
				// every request as anonymous while the middleware looked wired.
				c.SetUserContext(avsb.ContextWithEvalContext(c.UserContext(), evalCtx))
			}
		}
		return c.Next()
	}
}

// EvalContextFrom returns the EvalContext the middleware attached, or nil.
func EvalContextFrom(c *fiber.Ctx) avsb.EvalContext {
	if v := c.Locals(evalContextKey); v != nil {
		if evalCtx, ok := v.(avsb.EvalContext); ok {
			return evalCtx
		}
	}
	return avsb.EvalContextFrom(c.UserContext())
}
