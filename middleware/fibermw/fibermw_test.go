package fibermw_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/fibermw"
)

// TestMiddlewareAttachesIdentityWhereTheSDKReadsIt is the wiring check. Fiber
// has no *http.Request, so the identity goes on the Ctx's user context, which is
// what a caller passes to a Get*Flag call.
func TestMiddlewareAttachesIdentityWhereTheSDKReadsIt(t *testing.T) {
	evalCtx := avsb.UserContext("fiber-user", map[string]any{"plan": "pro"})

	app := fiber.New()
	app.Use(fibermw.Middleware(func(*fiber.Ctx) avsb.EvalContext { return evalCtx }))

	var viaHelper, viaSDK avsb.EvalContext
	app.Get("/test", func(c *fiber.Ctx) error {
		viaHelper = fibermw.EvalContextFrom(c)
		viaSDK = avsb.EvalContextFrom(c.UserContext())
		return c.SendStatus(fiber.StatusOK)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/test", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if viaHelper == nil {
		t.Fatal("the package helper must return the identity")
	}
	if viaSDK == nil {
		t.Fatal("the SDK must find the identity on the user context")
	}
	if avsb.BucketingKey(viaSDK, "user") != "fiber-user" {
		t.Errorf("want fiber-user, got %q", avsb.BucketingKey(viaSDK, "user"))
	}
}

// TestMiddlewareBuildsIdentityPerRequest covers the per-request part.
func TestMiddlewareBuildsIdentityPerRequest(t *testing.T) {
	app := fiber.New()
	app.Use(fibermw.Middleware(func(c *fiber.Ctx) avsb.EvalContext {
		return avsb.UserContext(c.Get("X-User-ID"), nil)
	}))

	var seen []string
	app.Get("/", func(c *fiber.Ctx) error {
		seen = append(seen, avsb.BucketingKey(fibermw.EvalContextFrom(c), "user"))
		return c.SendStatus(fiber.StatusOK)
	})

	for _, id := range []string{"u_1", "u_2"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-User-ID", id)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		_ = resp.Body.Close()
	}

	if len(seen) != 2 || seen[0] != "u_1" || seen[1] != "u_2" {
		t.Errorf("want one identity per request, got %v", seen)
	}
}

func TestNilContextFnPassesThrough(t *testing.T) {
	app := fiber.New()
	app.Use(fibermw.Middleware(nil))

	called := false
	app.Get("/", func(c *fiber.Ctx) error {
		called = true
		if got := fibermw.EvalContextFrom(c); got != nil {
			t.Errorf("nothing should be attached, got %v", got)
		}
		return c.SendStatus(fiber.StatusOK)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !called {
		t.Fatal("the handler was not called")
	}
}

func TestEvalContextFromAnUntouchedContextIsNil(t *testing.T) {
	app := fiber.New()

	var got avsb.EvalContext
	app.Get("/", func(c *fiber.Ctx) error {
		got = fibermw.EvalContextFrom(c)
		return c.SendStatus(fiber.StatusOK)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got != nil {
		t.Fatalf("want nil when nothing was attached, got %v", got)
	}
}
