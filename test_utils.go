package avsb

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/avsbhq/avsb-go/core"
)

// NewTestServer creates a Server with an inline datafile and reasons enabled.
// Intended for unit tests: skips HTTP/SSE and does not validate the SDK key.
func NewTestServer(datafileJSON []byte) (*Server, error) {
	s := &Server{
		opts: ServerOptions{
			SDKKey:         "test",
			InlineDatafile: datafileJSON,
			IncludeReasons: true,
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		log:    noopDecisionLog{},
	}
	if err := s.loadBytes(datafileJSON); err != nil {
		return nil, fmt.Errorf("avsb: NewTestServer: %w", err)
	}
	return s, nil
}

// InMemoryDatafileLoader is a helper for tests that want to supply a typed
// Datafile struct rather than raw JSON.
func InMemoryDatafileLoader(df *core.Datafile) ([]byte, error) {
	return json.Marshal(df)
}
