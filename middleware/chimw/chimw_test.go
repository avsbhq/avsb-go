package chimw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/chimw"
)

func TestChiMiddleware_InjectsEvalContext(t *testing.T) {
	evalCtx := avsb.UserContext("chi-user", nil)

	var got avsb.EvalContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = chimw.EvalContextFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := chimw.Middleware(nil, func(*http.Request) avsb.EvalContext { return evalCtx })(inner)
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got == nil {
		t.Fatal("expected eval context, got nil")
	}
}

func TestChiMiddleware_NilFn_PassesThrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	chimw.Middleware(nil, nil)(inner).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil),
	)
	if !called {
		t.Fatal("handler not called")
	}
}

func TestChiEvalContextFrom_AbsentKey_ReturnsNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if chimw.EvalContextFrom(req.Context()) != nil {
		t.Fatal("expected nil for missing key")
	}
}
