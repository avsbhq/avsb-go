// Submodule: the DynamoDB sticky bucketing store for the A vs B Go SDK.
//
// It is separate so the DynamoDB client is opt-in. As build-tagged files in the
// root module, `go mod tidy` pulled this client (and every other adapter's)
// into the root go.mod, because tidy cannot be told to ignore build tags. The
// root module promises zero dependencies; a submodule keeps that promise.
//
//	go get github.com/avsbhq/avsb-go/sticky/dynamodb
//
// The replace keeps this module building against the SDK sitting next to it in
// the repository. A replace directive is ignored when this module is a
// DEPENDENCY, so a consumer still resolves the tagged github.com/avsbhq/avsb-go.
//
// The DynamoDB client is pinned to the newest release that still supports Go
// 1.22, this module's floor. A service on a newer Go can require a newer
// client in its own go.mod; minimal version selection takes the higher one.
module github.com/avsbhq/avsb-go/sticky/dynamodb

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.1
	github.com/aws/aws-sdk-go-v2 v1.39.2
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.51.0
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.9 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.11.9 // indirect
	github.com/aws/smithy-go v1.23.0 // indirect
)

replace github.com/avsbhq/avsb-go => ../..
