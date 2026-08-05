package ginmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/ginmw"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestMiddlewareAttachesIdentityWhereTheSDKReadsIt is the wiring check. Setting
// only the gin key left the SDK evaluating every request as anonymous while the
// middleware looked correctly wired.
func TestMiddlewareAttachesIdentityWhereTheSDKReadsIt(t *testing.T) {
	evalCtx := avsb.UserContext("gin-user", map[string]any{"plan": "enterprise"})

	r := gin.New()
	r.Use(ginmw.Middleware(func(*gin.Context) avsb.EvalContext { return evalCtx }))

	var viaHelper, viaSDK avsb.EvalContext
	r.GET("/test", func(c *gin.Context) {
		viaHelper = ginmw.EvalContextFrom(c)
		// What server.GetBoolFlag(c.Request.Context(), ...) reads.
		viaSDK = avsb.EvalContextFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))

	if viaHelper == nil {
		t.Fatal("the package helper must return the identity")
	}
	if viaSDK == nil {
		t.Fatal("the SDK must find the identity on the request context")
	}
	if avsb.BucketingKey(viaSDK, "user") != "gin-user" {
		t.Errorf("want gin-user, got %q", avsb.BucketingKey(viaSDK, "user"))
	}
}

// TestMiddlewareBuildsIdentityPerRequest covers the per-request part: the
// function runs on every request, so two requests get two identities.
func TestMiddlewareBuildsIdentityPerRequest(t *testing.T) {
	r := gin.New()
	r.Use(ginmw.Middleware(func(c *gin.Context) avsb.EvalContext {
		return avsb.UserContext(c.GetHeader("X-User-ID"), nil)
	}))

	var seen []string
	r.GET("/", func(c *gin.Context) {
		seen = append(seen, avsb.BucketingKey(ginmw.EvalContextFrom(c), "user"))
		c.Status(http.StatusOK)
	})

	for _, id := range []string{"u_1", "u_2"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-User-ID", id)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	if len(seen) != 2 || seen[0] != "u_1" || seen[1] != "u_2" {
		t.Errorf("want one identity per request, got %v", seen)
	}
}

func TestNilContextFnPassesThrough(t *testing.T) {
	r := gin.New()
	r.Use(ginmw.Middleware(nil))

	called := false
	r.GET("/", func(c *gin.Context) {
		called = true
		if got := ginmw.EvalContextFrom(c); got != nil {
			t.Errorf("nothing should be attached, got %v", got)
		}
		c.Status(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("the handler was not called")
	}
}

func TestEvalContextFromAnUntouchedContextIsNil(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if ginmw.EvalContextFrom(c) != nil {
		t.Fatal("want nil when nothing was attached")
	}
}
