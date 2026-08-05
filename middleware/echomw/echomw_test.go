package echomw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/echomw"
)

// TestMiddlewareAttachesIdentityWhereTheSDKReadsIt is the wiring check: the
// identity has to land on the request context, which is what a Get*Flag call
// reads, not only on echo's own key store.
func TestMiddlewareAttachesIdentityWhereTheSDKReadsIt(t *testing.T) {
	evalCtx := avsb.UserContext("echo-user", map[string]any{"role": "admin"})

	e := echo.New()
	e.Use(echomw.Middleware(func(echo.Context) avsb.EvalContext { return evalCtx }))

	var viaHelper, viaSDK avsb.EvalContext
	e.GET("/test", func(c echo.Context) error {
		viaHelper = echomw.EvalContextFrom(c)
		viaSDK = avsb.EvalContextFrom(c.Request().Context())
		return c.NoContent(http.StatusOK)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))

	if viaHelper == nil {
		t.Fatal("the package helper must return the identity")
	}
	if viaSDK == nil {
		t.Fatal("the SDK must find the identity on the request context")
	}
	if avsb.BucketingKey(viaSDK, "user") != "echo-user" {
		t.Errorf("want echo-user, got %q", avsb.BucketingKey(viaSDK, "user"))
	}
}

// TestMiddlewareBuildsIdentityPerRequest covers the per-request part.
func TestMiddlewareBuildsIdentityPerRequest(t *testing.T) {
	e := echo.New()
	e.Use(echomw.Middleware(func(c echo.Context) avsb.EvalContext {
		return avsb.UserContext(c.Request().Header.Get("X-User-ID"), nil)
	}))

	var seen []string
	e.GET("/", func(c echo.Context) error {
		seen = append(seen, avsb.BucketingKey(echomw.EvalContextFrom(c), "user"))
		return c.NoContent(http.StatusOK)
	})

	for _, id := range []string{"u_1", "u_2"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-User-ID", id)
		e.ServeHTTP(httptest.NewRecorder(), req)
	}

	if len(seen) != 2 || seen[0] != "u_1" || seen[1] != "u_2" {
		t.Errorf("want one identity per request, got %v", seen)
	}
}

func TestNilContextFnPassesThrough(t *testing.T) {
	e := echo.New()
	e.Use(echomw.Middleware(nil))

	called := false
	e.GET("/", func(c echo.Context) error {
		called = true
		if got := echomw.EvalContextFrom(c); got != nil {
			t.Errorf("nothing should be attached, got %v", got)
		}
		return c.NoContent(http.StatusOK)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("the handler was not called")
	}
}

func TestEvalContextFromAnUntouchedContextIsNil(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	if echomw.EvalContextFrom(c) != nil {
		t.Fatal("want nil when nothing was attached")
	}
}
