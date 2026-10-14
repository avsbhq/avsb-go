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
// The indirect requirements and go.sum are written by `go mod tidy` and
// committed. Rerun it after changing a requirement (scripts/check.sh --tidy
// runs it for every module) and keep the go directive at 1.22.
module github.com/avsbhq/avsb-go/openfeature

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.1
	github.com/open-feature/go-sdk v1.12.0
)

require (
	github.com/go-logr/logr v1.4.2 // indirect
	golang.org/x/exp v0.0.0-20240506185415-9bf2ced13842 // indirect
)

replace github.com/avsbhq/avsb-go => ..
