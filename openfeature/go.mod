// Submodule: the OpenFeature provider for the A vs B Go SDK.
//
// It is separate so the OpenFeature SDK is opt-in. Forcing it on every consumer
// of the flag SDK was the complaint in the review: a dependency you never use
// still shows up in your module graph and your vulnerability report.
//
//	go get github.com/avsbhq/avsb-go/openfeature
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
module github.com/avsbhq/avsb-go/openfeature

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.1
	github.com/open-feature/go-sdk v1.12.0
)

replace github.com/avsbhq/avsb-go => ../..
