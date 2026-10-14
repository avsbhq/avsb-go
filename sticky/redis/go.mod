// Submodule: the Redis sticky bucketing store for the A vs B Go SDK.
//
// It is separate so the Redis client is opt-in. As build-tagged files in the
// root module, `go mod tidy` pulled this client (and every other adapter's)
// into the root go.mod, because tidy cannot be told to ignore build tags. The
// root module promises zero dependencies; a submodule keeps that promise.
//
//	go get github.com/avsbhq/avsb-go/sticky/redis
//
// The replace keeps this module building against the SDK sitting next to it in
// the repository. A replace directive is ignored when this module is a
// DEPENDENCY, so a consumer still resolves the tagged github.com/avsbhq/avsb-go.
//
// The Redis client is pinned to the newest release that still supports Go
// 1.22, this module's floor. A service on a newer Go can require a newer
// client in its own go.mod; minimal version selection takes the higher one.
module github.com/avsbhq/avsb-go/sticky/redis

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.1
	github.com/redis/go-redis/v9 v9.17.3
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
)

replace github.com/avsbhq/avsb-go => ../..
