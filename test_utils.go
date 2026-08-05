package avsb

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/avsbhq/avsb-go/core"
)

// Test seams. These are part of the public API on purpose: testing flag
// behaviour should not require a network, a fake CDN, or an interface you have
// to invent yourself.

// NewTestServer creates a Server from a datafile you supply, with no network of
// any kind: no fetch, no polling, no streaming, and no event delivery. Reasons
// are on, so a failing assertion tells you which rule decided.
//
//	server, err := avsb.NewTestServer(datafileJSON)
//	if err != nil { t.Fatal(err) }
//	defer server.Close()
//
//	flag := server.GetBoolFlag(ctx, "checkout-v2", false,
//	    avsb.WithContext(avsb.UserContext("alice", nil)))
//	if !flag.IsEnabled() { t.Errorf("reasons: %v", flag.Reasons()) }
//
// Track and TrackPurchase are deliberately inert here: a unit test that quietly
// posted to the real ingestion endpoint would be worse than one that records
// nothing.
func NewTestServer(datafileJSON []byte) (*Server, error) {
	server, err := NewServer(ServerOptions{
		Bootstrap:       datafileJSON,
		IncludeReasons:  true,
		PollingDisabled: true,
		Events:          EventOptions{Disabled: true},
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return nil, fmt.Errorf("avsb: NewTestServer: %w", err)
	}
	return server, nil
}

// NewTestServerFromDatafile is NewTestServer for a datafile you built as a
// struct rather than as JSON.
func NewTestServerFromDatafile(df *core.Datafile) (*Server, error) {
	raw, err := json.Marshal(df)
	if err != nil {
		return nil, fmt.Errorf("avsb: NewTestServerFromDatafile: %w", err)
	}
	return NewTestServer(raw)
}

// InMemoryDatafileLoader marshals a Datafile struct to the JSON bytes
// ServerOptions.Bootstrap and NewTestServer take.
func InMemoryDatafileLoader(df *core.Datafile) ([]byte, error) {
	return json.Marshal(df)
}
