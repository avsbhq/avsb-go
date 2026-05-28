module github.com/avsbhq/avsb-go

go 1.22

require (
	github.com/gin-gonic/gin v1.10.0
	github.com/labstack/echo/v4 v4.12.0
	github.com/gofiber/fiber/v2 v2.52.5
	github.com/open-feature/go-sdk v1.12.0
)

// chi is not required — chimw uses the standard net/http middleware interface
// which is natively compatible with go-chi. Users add chi to their own go.mod.
