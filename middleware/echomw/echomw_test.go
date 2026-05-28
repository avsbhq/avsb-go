package echomw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/echomw"
)

func TestEchoMiddleware_InjectsEvalContext(t *testing.T) {
	evalCtx := avsb.UserContext("echo-user", map[string]any{"role": "admin"})

	e := echo.New()
	e.Use(echomw.Middleware(nil, func(echo.Context) avsb.EvalContext {
		return evalCtx
	}))

	var got avsb.EvalContext
	e.GET("/test", func(c echo.Context) error {
		got = echomw.EvalContextFrom(c)
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	if got == nil {
		t.Fatal("expected eval context injected, got nil")
	}
}

func TestEchoMiddleware_NilFn_NoContextSet(t *testing.T) {
	e := echo.New()
	e.Use(echomw.Middleware(nil, nil))

	var got avsb.EvalContext
	e.GET("/", func(c echo.Context) error {
		got = echomw.EvalContextFrom(c)
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	e.ServeHTTP(httptest.NewRecorder(), req)

	if got != nil {
		t.Fatalf("expected nil eval context, got %v", got)
	}
}

func TestEchoEvalContextFrom_AbsentKey_ReturnsNil(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	c := e.NewContext(req, rr)
	if echomw.EvalContextFrom(c) != nil {
		t.Fatal("expected nil for absent key")
	}
}
