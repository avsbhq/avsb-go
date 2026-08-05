package httpmw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/middleware/httpmw"
)

// TestMiddlewareAttachesIdentityToTheRequestContext is the wiring the whole
// package exists for: a handler evaluates with r.Context() and gets the identity
// the middleware resolved, with no plumbing. Attaching it somewhere the SDK does
// not read is the bug this replaces.
func TestMiddlewareAttachesIdentityToTheRequestContext(t *testing.T) {
	evalCtx := avsb.UserContext("test-user", map[string]any{"plan": "pro"})

	var fromRequestContext avsb.EvalContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The SDK's own reader, not the package's helper: this is what
		// server.GetBoolFlag(r.Context(), ...) calls.
		fromRequestContext = avsb.EvalContextFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := httpmw.Middleware(func(*http.Request) avsb.EvalContext {
		return evalCtx
	})(inner)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if fromRequestContext == nil {
		t.Fatal("the SDK must find the identity on the request context")
	}
	if avsb.BucketingKey(fromRequestContext, "user") != "test-user" {
		t.Errorf("want the user key test-user, got %q", avsb.BucketingKey(fromRequestContext, "user"))
	}
}

// TestMiddlewareBuildsIdentityFromTheRequest covers the per-request part: the
// function runs on every request, so two requests get two identities.
func TestMiddlewareBuildsIdentityFromTheRequest(t *testing.T) {
	var seen []string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, avsb.BucketingKey(httpmw.EvalContextFrom(r.Context()), "user"))
	})

	handler := httpmw.Middleware(func(r *http.Request) avsb.EvalContext {
		return avsb.UserContext(r.Header.Get("X-User-ID"), nil)
	})(inner)

	for _, id := range []string{"u_1", "u_2"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-User-ID", id)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if len(seen) != 2 || seen[0] != "u_1" || seen[1] != "u_2" {
		t.Errorf("want one identity per request, got %v", seen)
	}
}

// TestNilContextFnPassesThrough covers wiring the middleware before the identity
// source exists: it must be harmless, not a panic.
func TestNilContextFnPassesThrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := httpmw.EvalContextFrom(r.Context()); got != nil {
			t.Errorf("nothing should be attached, got %v", got)
		}
	})

	httpmw.Middleware(nil)(inner).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("the inner handler was not called")
	}
}

// TestNilEvalContextIsNotAttached covers the other half: a contextFn that cannot
// resolve an identity yet (an anonymous request) returns nil, and nothing is
// attached rather than an empty context that looks like a real one.
func TestNilEvalContextIsNotAttached(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := httpmw.EvalContextFrom(r.Context()); got != nil {
			t.Errorf("a nil identity must not be attached, got %v", got)
		}
	})

	handler := httpmw.Middleware(func(*http.Request) avsb.EvalContext { return nil })(inner)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestEvalContextFromAnUntouchedRequestIsNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := httpmw.EvalContextFrom(req.Context()); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

// TestSetEvalContextRoundTrip covers building identity from several sources, and
// that the package helper and the SDK's own reader agree. A separate context key
// per middleware package is how a chi handler used to read nil from a context a
// net/http middleware had filled.
func TestSetEvalContextRoundTrip(t *testing.T) {
	evalCtx := avsb.OrgContext("acme", map[string]any{"tier": "enterprise"})
	ctx := httpmw.SetEvalContext(context.Background(), evalCtx)

	viaPackage := httpmw.EvalContextFrom(ctx)
	if viaPackage == nil {
		t.Fatal("expected the attached context back")
	}
	if avsb.BucketingKey(viaPackage, "organization") != "acme" {
		t.Errorf("want acme, got %q", avsb.BucketingKey(viaPackage, "organization"))
	}

	viaSDK := avsb.EvalContextFrom(ctx)
	if viaSDK == nil {
		t.Fatal("the SDK must read the same context the middleware wrote")
	}
	if avsb.BucketingKey(viaSDK, "organization") != "acme" {
		t.Errorf("the two readers must agree, got %q", avsb.BucketingKey(viaSDK, "organization"))
	}
}
