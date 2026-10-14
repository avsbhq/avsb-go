// Submodule: the Postgres sticky bucketing store for the A vs B Go SDK.
//
// It is separate so the Postgres client is opt-in. As build-tagged files in the
// root module, `go mod tidy` pulled this client (and every other adapter's)
// into the root go.mod, because tidy cannot be told to ignore build tags. The
// root module promises zero dependencies; a submodule keeps that promise.
//
//	go get github.com/avsbhq/avsb-go/sticky/postgres
//
// The replace keeps this module building against the SDK sitting next to it in
// the repository. A replace directive is ignored when this module is a
// DEPENDENCY, so a consumer still resolves the tagged github.com/avsbhq/avsb-go.
//
// The Postgres client is pinned to the newest release that still supports Go
// 1.22, this module's floor. A service on a newer Go can require a newer
// client in its own go.mod; minimal version selection takes the higher one.
module github.com/avsbhq/avsb-go/sticky/postgres

go 1.22

require (
	github.com/avsbhq/avsb-go v1.0.1
	github.com/jackc/pgx/v5 v5.7.4
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

replace github.com/avsbhq/avsb-go => ../..
