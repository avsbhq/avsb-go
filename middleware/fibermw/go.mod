// Per-framework submodule: fiber glue for the A vs B Go SDK.
//
// The import path is unchanged (github.com/avsbhq/avsb-go/middleware/fibermw);
// what changed is that it is now its own module, so the root SDK no longer
// forces fiber onto every consumer. Install it alongside the SDK:
//
//	go get github.com/avsbhq/avsb-go/middleware/fibermw
//
// The replace keeps this module building against the SDK sitting next to it in
// the repository. A replace directive is ignored when this module is a
// DEPENDENCY, so a consumer still resolves the tagged github.com/avsbhq/avsb-go.
//
// The indirect requirements and go.sum are written by `go mod tidy` and
// committed. Rerun it after changing a requirement (scripts/check.sh --tidy
// runs it for every module) and keep the go directive at 1.22.
module github.com/avsbhq/avsb-go/middleware/fibermw

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.1
	github.com/gofiber/fiber/v2 v2.52.5
)

require (
	github.com/andybalholm/brotli v1.0.5 // indirect
	github.com/google/uuid v1.5.0 // indirect
	github.com/klauspost/compress v1.17.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.51.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
)

replace github.com/avsbhq/avsb-go => ../..
