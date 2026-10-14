// Per-framework submodule: echo glue for the A vs B Go SDK.
//
// The import path is unchanged (github.com/avsbhq/avsb-go/middleware/echomw);
// what changed is that it is now its own module, so the root SDK no longer
// forces echo onto every consumer. Install it alongside the SDK:
//
//	go get github.com/avsbhq/avsb-go/middleware/echomw
//
// The replace keeps this module building against the SDK sitting next to it in
// the repository. A replace directive is ignored when this module is a
// DEPENDENCY, so a consumer still resolves the tagged github.com/avsbhq/avsb-go.
//
// The indirect requirements and go.sum are written by `go mod tidy` and
// committed. Rerun it after changing a requirement (scripts/check.sh --tidy
// runs it for every module) and keep the go directive at 1.22.
module github.com/avsbhq/avsb-go/middleware/echomw

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.1
	github.com/labstack/echo/v4 v4.12.0
)

require (
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/crypto v0.22.0 // indirect
	golang.org/x/net v0.24.0 // indirect
	golang.org/x/sys v0.19.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace github.com/avsbhq/avsb-go => ../..
