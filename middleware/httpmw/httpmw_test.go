package httpmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/httpmw"
)

func TestMiddleware_InjectsEvalContext(t *testing.T) {
	evalCtx := avsb.UserContext("test-user", map[string]any{"plan": "pro"})

	var captured avsb.EvalContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = httpmw.EvalContextFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := httpmw.Middleware(nil, func(*http.Request) avsb.EvalContext {
		return evalCtx
	})(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if captured == nil {
		t.Fatal("expected eval context to be injected, got nil")
	}
	// Verify key was preserved.
	kindRaw := captured["kind"]
	if kindRaw == nil {
		t.Fatal("expected kind to be present in eval context")
	}
}

func TestMiddleware_NilContextFn_DoesNotPanic(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := httpmw.Middleware(nil, nil)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler was not called")
	}
}

func TestEvalContextFrom_MissingKey_ReturnsNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ec := httpmw.EvalContextFrom(req.Context())
	if ec != nil {
		t.Fatalf("expected nil, got %v", ec)
	}
}

func TestSetEvalContext_RoundTrip(t *testing.T) {
	evalCtx := avsb.OrgContext("acme", map[string]any{"tier": "enterprise"})
	ctx := httpmw.SetEvalContext(req().Context(), evalCtx)

	retrieved := httpmw.EvalContextFrom(ctx)
	if retrieved == nil {
		t.Fatal("expected non-nil eval context after SetEvalContext")
	}
	if len(retrieved) == 0 {
		t.Fatal("expected non-empty eval context")
	}
}

func req() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/", nil)
}
