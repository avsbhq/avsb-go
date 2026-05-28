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

func TestGinMiddleware_InjectsEvalContext(t *testing.T) {
	evalCtx := avsb.UserContext("gin-user", map[string]any{"plan": "enterprise"})

	r := gin.New()
	r.Use(ginmw.Middleware(nil, func(c *gin.Context) avsb.EvalContext {
		return evalCtx
	}))

	var got avsb.EvalContext
	r.GET("/test", func(c *gin.Context) {
		got = ginmw.EvalContextFrom(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if got == nil {
		t.Fatal("expected eval context, got nil")
	}
}

func TestGinMiddleware_NilFn_NoContextSet(t *testing.T) {
	r := gin.New()
	r.Use(ginmw.Middleware(nil, nil))

	var got avsb.EvalContext
	r.GET("/", func(c *gin.Context) {
		got = ginmw.EvalContextFrom(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if got != nil {
		t.Fatalf("expected nil eval context, got %v", got)
	}
}

func TestGinEvalContextFrom_MissingKey_ReturnsNil(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if ginmw.EvalContextFrom(c) != nil {
		t.Fatal("expected nil for missing key")
	}
}
