// The A vs B Go SDK root module has ZERO dependencies, on purpose.
//
// It used to require gin, echo, fiber, and the OpenFeature SDK, so anyone who
// wanted flags inherited four web frameworks in their module graph, four sets
// of transitive CVEs for their scanner to report, and four chances of a version
// conflict. Worse, those requires had no go.sum, so `go test ./...` failed
// before it compiled anything.
//
// The framework glue now lives in its own modules, each installed only if you
// want it:
//
//	go get github.com/avsbhq/avsb-go/middleware/ginmw
//	go get github.com/avsbhq/avsb-go/middleware/echomw
//	go get github.com/avsbhq/avsb-go/middleware/fibermw
//	go get github.com/avsbhq/avsb-go/openfeature
//
// Import paths did not change. net/http and go-chi need nothing extra: both use
// middleware/httpmw, which stays in this module because it is stdlib only.
module github.com/avsbhq/avsb-go

go 1.22
