// Package ginmw provides a gin middleware for the A vs B Go SDK.
//
// Requires: github.com/gin-gonic/gin
//
// Usage:
//
//	r := gin.Default()
//	r.Use(ginmw.Middleware(server, func(c *gin.Context) avsb.EvalContext {
//	    return avsb.UserContext(c.GetString("userID"), nil)
//	}))
package ginmw

import (
	"github.com/gin-gonic/gin"
	avsb "github.com/avsbhq/avsb-go"
)

const evalContextKey = "avsb_eval_context"

// Middleware returns a gin.HandlerFunc that injects the EvalContext into the
// gin context under the key "avsb_eval_context".
func Middleware(server *avsb.Server, contextFn func(*gin.Context) avsb.EvalContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contextFn != nil {
			evalCtx := contextFn(c)
			c.Set(evalContextKey, evalCtx)
		}
		c.Next()
	}
}

// EvalContextFrom retrieves the EvalContext stored in the gin context, or nil.
func EvalContextFrom(c *gin.Context) avsb.EvalContext {
	v, ok := c.Get(evalContextKey)
	if !ok {
		return nil
	}
	ec, _ := v.(avsb.EvalContext)
	return ec
}

var _ *avsb.Server
