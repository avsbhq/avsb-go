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
// INDIRECT REQUIREMENTS ARE NOT LISTED HERE. `go mod tidy` writes them, along
// with go.sum, and it needs the network to do it. Run it once per module before
// publishing:
//
//	go mod tidy
module github.com/avsbhq/avsb-go/middleware/echomw

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.0
	github.com/labstack/echo/v4 v4.12.0
)

replace github.com/avsbhq/avsb-go => ../..
