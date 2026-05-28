package fibermw_test

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/fibermw"
)

func TestFiberMiddleware_InjectsEvalContext(t *testing.T) {
	evalCtx := avsb.UserContext("fiber-user", map[string]any{"plan": "pro"})

	app := fiber.New()
	app.Use(fibermw.Middleware(nil, func(*fiber.Ctx) avsb.EvalContext {
		return evalCtx
	}))

	var got avsb.EvalContext
	app.Get("/test", func(c *fiber.Ctx) error {
		got = fibermw.EvalContextFrom(c)
		return c.SendStatus(200)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	io.Discard.Write(nil) // discard body

	if got == nil {
		t.Fatal("expected eval context, got nil")
	}
}

func TestFiberMiddleware_NilFn_NoContextSet(t *testing.T) {
	app := fiber.New()
	app.Use(fibermw.Middleware(nil, nil))

	var got avsb.EvalContext
	app.Get("/", func(c *fiber.Ctx) error {
		got = fibermw.EvalContextFrom(c)
		return c.SendStatus(200)
	})

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFiberEvalContextFrom_AbsentLocals_ReturnsNil(t *testing.T) {
	app := fiber.New()
	var got avsb.EvalContext
	app.Get("/", func(c *fiber.Ctx) error {
		got = fibermw.EvalContextFrom(c)
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if got != nil {
		t.Fatal("expected nil for absent key")
	}
}
