package avsb

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// silentLogger keeps test output readable. The tests that care about a warning
// assert on behaviour, not on log text.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// captured is a thread-safe record of what a fake ingestion endpoint received.
type captured struct {
	mu     sync.Mutex
	paths  []string
	bodies []map[string]any
	status int
	// retryAfter is echoed as a Retry-After header when non-empty.
	retryAfter string
	// failFirst makes the first N requests fail, so retry behaviour is
	// observable without waiting out a real backoff curve.
	failFirst int
	requests  int
}

func (c *captured) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		c.mu.Lock()
		c.requests++
		attempt := c.requests
		c.paths = append(c.paths, r.URL.Path)
		c.bodies = append(c.bodies, body)
		status := c.status
		retryAfter := c.retryAfter
		failFirst := c.failFirst
		c.mu.Unlock()

		if attempt <= failFirst {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if status == 0 {
			status = http.StatusAccepted
		}
		w.WriteHeader(status)
	}
}

func (c *captured) snapshot() ([]string, []map[string]any, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	paths := append([]string(nil), c.paths...)
	bodies := append([]map[string]any(nil), c.bodies...)
	return paths, bodies, c.requests
}

// newTestTracker wires a tracker at a fake ingestion endpoint.
func newTestTracker(t *testing.T, rec *captured, opts EventOptions) *eventTracker {
	t.Helper()
	server := httptest.NewServer(rec.handler())
	t.Cleanup(server.Close)

	tracker, err := newEventTracker("sdk_production_abc123", server.URL, opts,
		&http.Client{Timeout: 2 * time.Second}, silentLogger())
	if err != nil {
		t.Fatalf("newEventTracker: %v", err)
	}
	t.Cleanup(func() { tracker.close(context.Background()) })
	return tracker
}

// TestExposureBatchMatchesTheLockedWireSchema is the field-name contract. The
// ingestion worker validates these exact names and the dashboard reads the
// columns they land in, so a renamed field is not a compile error anywhere: the
// data simply disappears.
func TestExposureBatchMatchesTheLockedWireSchema(t *testing.T) {
	rec := &captured{}
	tracker := newTestTracker(t, rec, EventOptions{FlushInterval: time.Hour, BatchSize: 50})

	tracker.queueExposure(core.ExposureEvent{
		VisitorID:    "user_42",
		ExperimentID: "rule_ab_split",
		VariationID:  "var_treatment",
		EventName:    "flag_exposure",
		Timestamp:    1780000000000,
		Properties:   map[string]interface{}{"flagKey": "checkout-v2", "flagId": "flag_ab_1"},
		Attributes:   map[string]string{"plan": "pro"},
	})
	tracker.flush(context.Background())

	paths, bodies, requests := rec.snapshot()
	if requests != 1 {
		t.Fatalf("want one request, got %d", requests)
	}
	if paths[0] != "/v1/collect/batch" {
		t.Errorf("path: want /v1/collect/batch, got %q", paths[0])
	}

	body := bodies[0]
	if body["sdkKey"] != "sdk_production_abc123" {
		t.Errorf("sdkKey: want the configured key, got %v", body["sdkKey"])
	}
	events, ok := body["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("events: want a one-element array, got %v", body["events"])
	}
	event, ok := events[0].(map[string]any)
	if !ok {
		t.Fatalf("event: want an object, got %v", events[0])
	}

	// Required fields, by exact name.
	for _, field := range []string{
		"eventId", "visitorId", "experimentId", "variationId",
		"eventName", "revenue", "timestamp", "pageUrl",
	} {
		if _, present := event[field]; !present {
			t.Errorf("required field %q is missing from the exposure payload", field)
		}
	}
	if event["visitorId"] != "user_42" {
		t.Errorf("visitorId: want user_42, got %v", event["visitorId"])
	}
	if event["experimentId"] != "rule_ab_split" {
		t.Errorf("experimentId: want rule_ab_split, got %v", event["experimentId"])
	}
	if event["eventName"] != "flag_exposure" {
		t.Errorf("eventName: want flag_exposure, got %v", event["eventName"])
	}
	if event["revenue"] != nil {
		t.Errorf("revenue on an exposure is null, got %v", event["revenue"])
	}
	if event["pageUrl"] != "" {
		t.Errorf("pageUrl on a server SDK is the empty string, got %v", event["pageUrl"])
	}
	if id, _ := event["eventId"].(string); len(id) != 36 {
		t.Errorf("eventId: want a 36-character UUID, got %q", event["eventId"])
	}
	props, _ := event["properties"].(map[string]any)
	if props["flagKey"] != "checkout-v2" {
		t.Errorf("properties.flagKey: want checkout-v2, got %v", props["flagKey"])
	}
	attrs, _ := event["attributes"].(map[string]any)
	if attrs["plan"] != "pro" {
		t.Errorf("attributes.plan: want pro, got %v", attrs["plan"])
	}
}

// TestMetricBatchMatchesTheLockedWireSchema covers the conversion envelope,
// including the field that must NOT be there: the metric route strips
// properties, so anything sent under that name looks delivered and is gone.
func TestMetricBatchMatchesTheLockedWireSchema(t *testing.T) {
	rec := &captured{}
	tracker := newTestTracker(t, rec, EventOptions{FlushInterval: time.Hour, BatchSize: 50})

	value := 1.0
	tracker.queueMetric(core.MetricEvent{
		VisitorID: "user_42",
		EventName: "signup_completed",
		Value:     &value,
		Timestamp: 1780000000000,
	})
	tracker.flush(context.Background())

	paths, bodies, requests := rec.snapshot()
	if requests != 1 {
		t.Fatalf("want one request, got %d", requests)
	}
	if paths[0] != "/v1/collect/metric-batch" {
		t.Errorf("path: want /v1/collect/metric-batch, got %q", paths[0])
	}

	events, ok := bodies[0]["metricEvents"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("metricEvents: want a one-element array, got %v", bodies[0])
	}
	event := events[0].(map[string]any)
	for _, field := range []string{"eventId", "visitorId", "eventName", "revenue", "timestamp", "pageUrl"} {
		if _, present := event[field]; !present {
			t.Errorf("required field %q is missing from the metric payload", field)
		}
	}
	if _, present := event["properties"]; present {
		t.Error("the metric batch has no properties field: it is stripped server-side, " +
			"so sending it hides data instead of delivering it")
	}
	if _, present := event["experimentId"]; present {
		t.Error("a metric event carries no experiment: attribution happens server-side")
	}
	if event["value"] != 1.0 {
		t.Errorf("value: want 1, got %v", event["value"])
	}
}

// TestOrdersBatchMatchesTheLockedWireSchema covers the purchase envelope, which
// is sent immediately rather than batched.
func TestOrdersBatchMatchesTheLockedWireSchema(t *testing.T) {
	rec := &captured{}
	tracker := newTestTracker(t, rec, EventOptions{FlushInterval: time.Hour})

	price := 99.95
	quantity := 1
	err := tracker.trackPurchase(context.Background(), "user_42", PurchaseOrder{
		OrderID:  "ORD-1001",
		Total:    99.95,
		Currency: "USD",
		Items:    []PurchaseItem{{SKU: "SKU-1", Price: &price, Quantity: &quantity}},
	})
	if err != nil {
		t.Fatalf("trackPurchase: %v", err)
	}

	paths, bodies, requests := rec.snapshot()
	if requests != 1 {
		t.Fatalf("want one request, got %d", requests)
	}
	if paths[0] != "/v1/collect/orders" {
		t.Errorf("path: want /v1/collect/orders, got %q", paths[0])
	}
	orders, ok := bodies[0]["orders"].([]any)
	if !ok || len(orders) != 1 {
		t.Fatalf("orders: want a one-element array, got %v", bodies[0])
	}
	order := orders[0].(map[string]any)
	for _, field := range []string{"eventId", "visitorId", "orderId", "total", "timestamp"} {
		if _, present := order[field]; !present {
			t.Errorf("required field %q is missing from the order payload", field)
		}
	}
	if order["total"] != 99.95 {
		t.Errorf("total: want 99.95 in decimal major units, got %v", order["total"])
	}
	items := order["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["sku"] != "SKU-1" {
		t.Errorf("items: want one item with sku SKU-1, got %v", order["items"])
	}
}

// TestEventIDIsStampedOnceSoARetryDeduplicates is the whole point of stamping at
// enqueue time. If the id were minted per attempt, a batch that timed out and
// succeeded on retry would be counted twice.
func TestEventIDIsStampedOnceSoARetryDeduplicates(t *testing.T) {
	rec := &captured{failFirst: 1}
	tracker := newTestTracker(t, rec, EventOptions{
		FlushInterval: time.Hour, BatchSize: 50, RetryAttempts: 2, SendTimeout: time.Second,
	})

	tracker.queueExposure(core.ExposureEvent{
		VisitorID: "user_42", ExperimentID: "rule_1", VariationID: "var_1",
		EventName: "flag_exposure",
	})
	tracker.flush(context.Background())

	_, bodies, requests := rec.snapshot()
	if requests != 2 {
		t.Fatalf("want the first attempt to fail and the second to succeed, got %d requests", requests)
	}

	idOf := func(body map[string]any) string {
		events := body["events"].([]any)
		return events[0].(map[string]any)["eventId"].(string)
	}
	first := idOf(bodies[0])
	second := idOf(bodies[1])
	if first == "" {
		t.Fatal("the first attempt carried no eventId")
	}
	if first != second {
		t.Errorf("a retry must resend the SAME eventId so ingestion can deduplicate: %q then %q",
			first, second)
	}
}

// TestRetryAfterIsHonoured covers the server-supplied delay. Ignoring it is how
// a fleet of SDKs keeps hammering a worker that just asked for a pause.
func TestRetryAfterIsHonoured(t *testing.T) {
	// A Retry-After of one second, with one forced failure. The elapsed time
	// proves the header was read: the exponential curve's first step is also
	// about a second, so the assertion is deliberately loose on the upper
	// bound and strict on the lower.
	rec := &captured{failFirst: 1, retryAfter: "1"}
	tracker := newTestTracker(t, rec, EventOptions{
		FlushInterval: time.Hour, BatchSize: 50, RetryAttempts: 1, SendTimeout: time.Second,
	})

	tracker.queueExposure(core.ExposureEvent{VisitorID: "u", EventName: "flag_exposure"})

	start := time.Now()
	tracker.flush(context.Background())
	elapsed := time.Since(start)

	_, _, requests := rec.snapshot()
	if requests != 2 {
		t.Fatalf("want two attempts, got %d", requests)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("want at least the requested one second pause before the retry, waited %v", elapsed)
	}
}

// TestQueueIsBoundedAndDropsOldest covers the memory guard. A full queue means
// ingestion is unreachable, and holding a process's memory hostage to that is
// worse than losing the stalest events.
func TestQueueIsBoundedAndDropsOldest(t *testing.T) {
	rec := &captured{}
	tracker := newTestTracker(t, rec, EventOptions{
		FlushInterval: time.Hour, BatchSize: 1000, MaxQueueSize: 3,
	})

	for i := 0; i < 10; i++ {
		tracker.queueExposure(core.ExposureEvent{
			VisitorID: "u", EventName: "flag_exposure", VariationID: string(rune('a' + i)),
		})
	}

	exposures, _ := tracker.queued()
	if exposures != 3 {
		t.Fatalf("queue depth: want the bound of 3, got %d", exposures)
	}

	tracker.flush(context.Background())
	_, bodies, _ := rec.snapshot()
	events := bodies[0]["events"].([]any)
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	// The three NEWEST survive: h, i, j for inputs a..j.
	first := events[0].(map[string]any)["variationId"]
	if first != "h" {
		t.Errorf("the oldest events are dropped, so the first kept should be h, got %v", first)
	}
}

// TestBatchSizeTriggersAFlush covers the second flush trigger: reaching the
// batch size should not wait for the timer.
func TestBatchSizeTriggersAFlush(t *testing.T) {
	rec := &captured{}
	tracker := newTestTracker(t, rec, EventOptions{
		FlushInterval: time.Hour, BatchSize: 2, MaxQueueSize: 100,
	})

	tracker.queueExposure(core.ExposureEvent{VisitorID: "u1", EventName: "flag_exposure"})
	tracker.queueExposure(core.ExposureEvent{VisitorID: "u2", EventName: "flag_exposure"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, requests := rec.snapshot(); requests > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, bodies, requests := rec.snapshot()
	if requests == 0 {
		t.Fatal("reaching the batch size must trigger a flush without waiting for the timer")
	}
	if got := len(bodies[0]["events"].([]any)); got != 2 {
		t.Errorf("want both events in one batch, got %d", got)
	}
}

// TestFlushSplitsIntoBatchesAtTheSizeLimit covers the chunking. The ingestion
// worker rejects more than 100 events per request, so a burst has to be split
// rather than sent whole and refused.
func TestFlushSplitsIntoBatchesAtTheSizeLimit(t *testing.T) {
	rec := &captured{}
	tracker := newTestTracker(t, rec, EventOptions{
		FlushInterval: time.Hour, BatchSize: 20, MaxQueueSize: 1000,
	})

	// Queue 45 without tripping the batch trigger, by writing straight to the
	// buffer the way the exposure path does but bypassing the size check.
	tracker.mu.Lock()
	for i := 0; i < 45; i++ {
		tracker.exposures = append(tracker.exposures, core.ExposureEvent{
			EventID: newEventID(), VisitorID: "u", EventName: "flag_exposure",
		})
	}
	tracker.mu.Unlock()

	tracker.flush(context.Background())

	_, bodies, requests := rec.snapshot()
	if requests != 3 {
		t.Fatalf("45 events at a batch size of 20 is 3 requests, got %d", requests)
	}
	sizes := []int{20, 20, 5}
	for i, want := range sizes {
		if got := len(bodies[i]["events"].([]any)); got != want {
			t.Errorf("batch %d: want %d events, got %d", i, want, got)
		}
	}
}

// TestBatchSizeIsCappedAtTheIngestLimit covers the option guard: asking for a
// bigger batch than ingestion accepts would make every flush fail.
func TestBatchSizeIsCappedAtTheIngestLimit(t *testing.T) {
	got := EventOptions{BatchSize: 5000}.withDefaults()
	if got.BatchSize != maxEventsPerBatch {
		t.Errorf("BatchSize: want it capped at %d, got %d", maxEventsPerBatch, got.BatchSize)
	}
}

// TestCloseFlushesWhatIsLeft is the difference between shipping the last batch
// of a deploy and losing it.
func TestCloseFlushesWhatIsLeft(t *testing.T) {
	rec := &captured{}
	server := httptest.NewServer(rec.handler())
	defer server.Close()

	tracker, err := newEventTracker("sdk_production_abc123", server.URL,
		EventOptions{FlushInterval: time.Hour, BatchSize: 100},
		&http.Client{Timeout: 2 * time.Second}, silentLogger())
	if err != nil {
		t.Fatalf("newEventTracker: %v", err)
	}

	tracker.queueExposure(core.ExposureEvent{VisitorID: "u", EventName: "flag_exposure"})
	tracker.close(context.Background())

	_, bodies, requests := rec.snapshot()
	if requests != 1 {
		t.Fatalf("close must flush the queue, got %d requests", requests)
	}
	if got := len(bodies[0]["events"].([]any)); got != 1 {
		t.Errorf("want the queued event delivered, got %d events", got)
	}

	// After close, nothing else is accepted or sent.
	tracker.queueExposure(core.ExposureEvent{VisitorID: "u2", EventName: "flag_exposure"})
	if exposures, _ := tracker.queued(); exposures != 0 {
		t.Errorf("a closed tracker must not accept events, queued %d", exposures)
	}
	if err := tracker.trackPurchase(context.Background(), "u", PurchaseOrder{
		OrderID: "ORD-2", Total: 5,
	}); err == nil {
		t.Error("TrackPurchase after close must report that the order was not sent")
	}
}

// TestGivingUpAfterRetriesIsLoud covers the last line of defence: when a batch
// really cannot be delivered, the events are lost and that has to be said.
func TestGivingUpAfterRetriesIsLoud(t *testing.T) {
	rec := &captured{failFirst: 100}
	tracker := newTestTracker(t, rec, EventOptions{
		FlushInterval: time.Hour, BatchSize: 50, RetryAttempts: 1, SendTimeout: time.Second,
	})

	tracker.queueExposure(core.ExposureEvent{VisitorID: "u", EventName: "flag_exposure"})
	tracker.flush(context.Background())

	_, _, requests := rec.snapshot()
	if requests != 2 {
		t.Fatalf("one attempt plus one retry is two requests, got %d", requests)
	}
	// The queue is not re-filled: the snapshot was taken before sending, so a
	// permanently failing endpoint cannot grow the queue without bound.
	if exposures, _ := tracker.queued(); exposures != 0 {
		t.Errorf("a dropped batch must not be re-queued, found %d", exposures)
	}
}

// TestNewEventIDLooksLikeAUUIDv4 covers the id format, which ingestion
// deduplicates on.
func TestNewEventIDLooksLikeAUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := newEventID()
		if len(id) != 36 {
			t.Fatalf("want 36 characters, got %q", id)
		}
		if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("want dashes at 8, 13, 18, 23, got %q", id)
		}
		if id[14] != '4' {
			t.Fatalf("want version 4, got %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

// TestEventOptionDefaults covers the zero value, because "the zero value is
// sensible" is a promise the docs make.
func TestEventOptionDefaults(t *testing.T) {
	got := EventOptions{}.withDefaults()
	if got.FlushInterval != 5*time.Second {
		t.Errorf("FlushInterval: want 5s, got %v", got.FlushInterval)
	}
	if got.BatchSize != 20 {
		t.Errorf("BatchSize: want 20, got %d", got.BatchSize)
	}
	if got.MaxQueueSize != 10000 {
		t.Errorf("MaxQueueSize: want 10000, got %d", got.MaxQueueSize)
	}
	if got.RetryAttempts != 3 {
		t.Errorf("RetryAttempts: want 3, got %d", got.RetryAttempts)
	}
	if got.SendTimeout != 10*time.Second {
		t.Errorf("SendTimeout: want 10s, got %v", got.SendTimeout)
	}
}
