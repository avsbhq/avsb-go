// Data-plane endpoint contract (SDK/API DX overhaul spec §1.1).
//
// Single source of truth for every network target this SDK talks to.
// Mirror of packages/avsb-core/src/endpoints.ts; both are locked by the
// "endpoints" conformance fixture family
// (packages/avsb-conformance/fixtures/endpoints/manifest.json).
//
// Nothing in this SDK may compose a data-plane URL by hand. Change a URL
// here and the fixture changes with it, in the same commit, or every
// SDK's conformance run fails.
//
// Credential posture:
//   - Datafile and preview reads are keyless by path. The SDK key is the
//     path segment: no header, no query string, no Authorization. The
//     legacy X-SDK-Key header on datafile fetches is dropped.
//   - Event sends carry sdkKey in the JSON body envelope.
//   - The stream ticket request is the one read that takes the key as a
//     query parameter.
//   - No Authorization header anywhere on the data plane.

package avsb

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Default hosts. Overridable for self-hosted proxies, local dev, and
// tests. Nothing here points at a host that does not exist:
// datafile.avsb.cloud and stream.avsb.cloud are NOT real and must never
// appear as defaults.
const (
	// DefaultCDNHost serves datafiles, previews, and static SDK assets.
	DefaultCDNHost = "https://cdn.avsb.cloud"
	// DefaultCollectHost is the event ingestion origin. Origin only: the
	// collect paths are appended.
	DefaultCollectHost = "https://ingest.avsb.cloud"
	// DefaultAppHost is the dashboard origin. Serves the stream ticket
	// and heartbeat routes.
	DefaultAppHost = "https://app.avsb.cloud"
)

// Ingestion paths. Absolute paths appended to a collect ORIGIN (see
// CollectURL).
const (
	// CollectPathSingle takes one event per request.
	CollectPathSingle = "/v1/collect"
	// CollectPathBatch takes {sdkKey, events: [...]}.
	CollectPathBatch = "/v1/collect/batch"
	// CollectPathMetricBatch takes {sdkKey, metricEvents: [...]}.
	CollectPathMetricBatch = "/v1/collect/metric-batch"
	// CollectPathOrders takes {sdkKey, orders: [...]}.
	CollectPathOrders = "/v1/collect/orders"
	// CollectPathErrors takes aggregated client error reports.
	CollectPathErrors = "/v1/collect/errors"
	// CollectPathPing is the install-detection ping (rate limited per IP).
	CollectPathPing = "/v1/ping"
	// CollectPathGeo is the coarse geo lookup for audience targeting.
	CollectPathGeo = "/v1/geo"
)

// App-origin paths.
const (
	// StreamTicketPath mints a short-lived SSE token.
	StreamTicketPath = "/api/sdk/stream"
	// HeartbeatPath records SDK liveness.
	HeartbeatPath = "/api/sdk/heartbeat"
)

// CDN object names under the "<sdkKey>/" prefix.
const (
	DatafileFilename = "datafile.json"
	PreviewFilename  = "preview.json"
)

// CollectPaths maps a kind name to its ingestion path. The keys match the
// conformance fixtures and the other language ports.
var CollectPaths = map[string]string{
	"single":      CollectPathSingle,
	"batch":       CollectPathBatch,
	"metricBatch": CollectPathMetricBatch,
	"orders":      CollectPathOrders,
	"errors":      CollectPathErrors,
	"ping":        CollectPathPing,
	"geo":         CollectPathGeo,
}

func trimTrailingSlashes(host string) string {
	return strings.TrimRight(host, "/")
}

// requireSDKKey is a loud guard: an empty key builds
// https://cdn.avsb.cloud//datafile.json, which 404s at the edge with
// nothing in the message to explain why. Full key-shape validation
// belongs in NewServer (spec §1.2); this only catches "nothing passed".
func requireSDKKey(sdkKey, fnName string) (string, error) {
	trimmed := strings.TrimSpace(sdkKey)
	if trimmed == "" {
		return "", fmt.Errorf(
			"avsb: %s was called without an SDK key. Pass the environment SDK key "+
				"(it looks like \"sdk_production_xxxxxxxx\"), which you can copy from "+
				"Settings, then Environments, in the A vs B dashboard", fnName)
	}
	return trimmed, nil
}

func hostOrDefault(host, fallback string) string {
	if strings.TrimSpace(host) == "" {
		return fallback
	}
	return trimTrailingSlashes(strings.TrimSpace(host))
}

// DatafileURL builds the canonical datafile URL:
// {cdnHost}/{sdkKey}/datafile.json. Pass an empty cdnHost for the public
// CDN.
//
// This is the ONLY correct shape. datafile.avsb.cloud/v2/{key},
// /datafiles/{key}.json, and /v1/datafiles/{key}.json were all bugs and
// all 404.
func DatafileURL(sdkKey, cdnHost string) (string, error) {
	key, err := requireSDKKey(sdkKey, "DatafileURL")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s", hostOrDefault(cdnHost, DefaultCDNHost), key, DatafileFilename), nil
}

// PreviewURL builds the per-environment preview snapshot URL. The
// snapshot is served with no-store, so a reader always gets the latest
// publish.
func PreviewURL(sdkKey, cdnHost string) (string, error) {
	key, err := requireSDKKey(sdkKey, "PreviewURL")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s", hostOrDefault(cdnHost, DefaultCDNHost), key, PreviewFilename), nil
}

// DatafileObjectKey returns the CDN object key for the live datafile.
func DatafileObjectKey(sdkKey string) (string, error) {
	key, err := requireSDKKey(sdkKey, "DatafileObjectKey")
	if err != nil {
		return "", err
	}
	return key + "/" + DatafileFilename, nil
}

// PreviewObjectKey returns the CDN object key for the preview snapshot.
func PreviewObjectKey(sdkKey string) (string, error) {
	key, err := requireSDKKey(sdkKey, "PreviewObjectKey")
	if err != nil {
		return "", err
	}
	return key + "/" + PreviewFilename, nil
}

// CollectOriginFrom normalises a datafile collectEndpoint value down to a
// bare origin.
//
// The flag datafile publishes an origin (https://ingest.avsb.cloud); the
// web-experiment datafile publishes the full batch URL. Both are valid
// inputs, so an SDK can never produce
// /v1/collect/batch/v1/collect/batch.
func CollectOriginFrom(collectEndpoint string) string {
	trimmed := trimTrailingSlashes(strings.TrimSpace(collectEndpoint))
	for _, path := range CollectPaths {
		if strings.HasSuffix(trimmed, path) {
			return strings.TrimSuffix(trimmed, path)
		}
	}
	return trimmed
}

// CollectURL builds an ingestion URL. kind is a key of CollectPaths;
// pass an empty collectEndpoint for the public ingest host.
func CollectURL(kind, collectEndpoint string) (string, error) {
	path, ok := CollectPaths[kind]
	if !ok {
		known := make([]string, 0, len(CollectPaths))
		for k := range CollectPaths {
			known = append(known, k)
		}
		sort.Strings(known)
		return "", fmt.Errorf(
			"avsb: CollectURL got an unknown endpoint kind %q. Known kinds: %s",
			kind, strings.Join(known, ", "))
	}
	origin := DefaultCollectHost
	if strings.TrimSpace(collectEndpoint) != "" {
		origin = CollectOriginFrom(collectEndpoint)
	}
	return origin + path, nil
}

// StreamEndpointResolution is the outcome of ResolveStreamEndpoint.
//
// Streaming is SERVER CONTROLLED: the publisher writes streamEndpoint
// into the datafile. With neither an explicit option nor a datafile
// field, streaming stays OFF and Reason explains why. Never guess a
// host: a default of stream.avsb.cloud gave every Go customer an
// infinite reconnect loop against DNS that does not resolve.
type StreamEndpointResolution struct {
	Available bool
	// Endpoint is the ticket endpoint when Available.
	Endpoint string
	// Source is "option" or "datafile" when Available.
	Source string
	// Reason is an actionable explanation when not Available.
	Reason string
}

const streamUnavailableReason = "avsb: streaming is on, but there is no stream endpoint to " +
	"connect to: the datafile carries no \"streamEndpoint\" field and no StreamingEndpoint " +
	"option was set. Republish the environment from the A vs B dashboard (Settings, then " +
	"Environments) to pick up the field, or set StreamingEndpoint explicitly. Polling stays " +
	"on in the meantime, so flag changes still arrive, just on the polling interval instead " +
	"of instantly."

// ResolveStreamEndpoint resolves the stream ticket endpoint. Precedence:
// explicit option, then the datafile field, then nothing.
func ResolveStreamEndpoint(optionEndpoint, datafileStreamEndpoint string) StreamEndpointResolution {
	if option := strings.TrimSpace(optionEndpoint); option != "" {
		return StreamEndpointResolution{
			Available: true,
			Endpoint:  trimTrailingSlashes(option),
			Source:    "option",
		}
	}
	if fromDatafile := strings.TrimSpace(datafileStreamEndpoint); fromDatafile != "" {
		return StreamEndpointResolution{
			Available: true,
			Endpoint:  trimTrailingSlashes(fromDatafile),
			Source:    "datafile",
		}
	}
	return StreamEndpointResolution{Available: false, Reason: streamUnavailableReason}
}

// queryValue percent-encodes a query VALUE.
//
// url.QueryEscape is FORM encoding: it turns a space into "+" and a literal
// plus into "%2B". A "+" is legal in a form body but means a space to a
// query-string reader, so forwarding form encoding into a query value sends a
// different token than the reference implementation does. Any "+" left after
// escaping can only have come from a space (a literal plus is already %2B), so
// rewriting it to %20 is safe and matches encodeURIComponent. Locked by the
// endpoints fixture case stream_subscribe_url_encodes_a_space_as_percent20.
func queryValue(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// StreamTicketURL is step 1 of streaming: the ticket request URL.
func StreamTicketURL(streamEndpoint, sdkKey string) (string, error) {
	key, err := requireSDKKey(sdkKey, "StreamTicketURL")
	if err != nil {
		return "", err
	}
	return trimTrailingSlashes(streamEndpoint) + "?sdkKey=" + queryValue(key), nil
}

// StreamSubscribeURL is step 2 of streaming: the SSE URL.
//
// subscribeBaseURL is the `url` field the TICKET RESPONSE handed back, not the
// ticket endpoint itself. The token rides in the query string because SSE
// clients cannot set request headers; tokens are single-resource and expire in
// 60 seconds.
func StreamSubscribeURL(subscribeBaseURL, sdkKey, token string) (string, error) {
	key, err := requireSDKKey(sdkKey, "StreamSubscribeURL")
	if err != nil {
		return "", err
	}
	return trimTrailingSlashes(subscribeBaseURL) +
		"?sdkKey=" + queryValue(key) +
		"&token=" + queryValue(token), nil
}

// StreamEndpointURL is the fallback stream ticket URL, for docs and
// tests. Pass an empty appHost for the public dashboard origin.
func StreamEndpointURL(appHost string) string {
	return hostOrDefault(appHost, DefaultAppHost) + StreamTicketPath
}

// HeartbeatURL is the fallback heartbeat URL, for docs and tests. Pass an
// empty appHost for the public dashboard origin.
func HeartbeatURL(appHost string) string {
	return hostOrDefault(appHost, DefaultAppHost) + HeartbeatPath
}

// ResolveHeartbeatEndpoint returns the datafile's heartbeat endpoint, or
// an empty string meaning "do not send heartbeats".
//
// Deliberately not defaulted to app.avsb.cloud: a proxied deployment
// publishes its own origin, and heartbeats posted to the wrong host would
// report liveness nowhere while looking healthy.
func ResolveHeartbeatEndpoint(datafileHeartbeatEndpoint string) string {
	return strings.TrimSpace(datafileHeartbeatEndpoint)
}
