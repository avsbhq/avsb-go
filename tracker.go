package avsb

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// Batched event delivery to the ingestion endpoint.
//
// Mirrors packages/avsb-node/src/serverTracker.ts: a bounded queue, an
// event id stamped once at enqueue time so a retried batch deduplicates, retry
// with jittered backoff honouring Retry-After, and a flush on close.
//
// Without this the Go SDK could evaluate flags but never record an exposure or
// a conversion, so a server-side experiment produced zero data and looked like
// it was running.

// eventTracker batches exposures and metric events and posts them to ingest.
type eventTracker struct {
	sdkKey     string
	batchURL   string
	metricURL  string
	ordersURL  string
	opts       EventOptions
	httpClient *http.Client
	logger     *slog.Logger

	mu        sync.Mutex
	exposures []core.ExposureEvent
	metrics   []core.MetricEvent
	closed    bool

	flushSignal chan struct{}
	stop        chan struct{}
	done        chan struct{}
}

// exposureBatch is the locked envelope for /v1/collect/batch.
type exposureBatch struct {
	SDKKey string               `json:"sdkKey"`
	Events []core.ExposureEvent `json:"events"`
}

// metricBatch is the locked envelope for /v1/collect/metric-batch.
type metricBatch struct {
	SDKKey       string             `json:"sdkKey"`
	MetricEvents []core.MetricEvent `json:"metricEvents"`
}

// ordersBatch is the locked envelope for /v1/collect/orders.
type ordersBatch struct {
	SDKKey string            `json:"sdkKey"`
	Orders []purchasePayload `json:"orders"`
}

// newEventTracker builds a tracker for one datafile's collect endpoint.
func newEventTracker(
	sdkKey string,
	collectEndpoint string,
	opts EventOptions,
	httpClient *http.Client,
	logger *slog.Logger,
) (*eventTracker, error) {
	batchURL, err := CollectURL("batch", collectEndpoint)
	if err != nil {
		return nil, err
	}
	metricURL, err := CollectURL("metricBatch", collectEndpoint)
	if err != nil {
		return nil, err
	}
	ordersURL, err := CollectURL("orders", collectEndpoint)
	if err != nil {
		return nil, err
	}

	t := &eventTracker{
		sdkKey:      sdkKey,
		batchURL:    batchURL,
		metricURL:   metricURL,
		ordersURL:   ordersURL,
		opts:        opts.withDefaults(),
		httpClient:  httpClient,
		logger:      logger,
		flushSignal: make(chan struct{}, 1),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	go t.run()
	return t, nil
}

// run is the single background worker. Batch-triggered flushes signal it
// instead of spawning goroutines, so a burst of events cannot fan out into a
// burst of concurrent POSTs.
func (t *eventTracker) run() {
	defer close(t.done)
	ticker := time.NewTicker(t.opts.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.flush(context.Background())
		case <-t.flushSignal:
			t.flush(context.Background())
		}
	}
}

// queueExposure enqueues an exposure event, stamping its event id.
func (t *eventTracker) queueExposure(event core.ExposureEvent) {
	if event.EventID == "" {
		event.EventID = newEventID()
	}
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.exposures = append(t.exposures, event)
	dropped := 0
	if len(t.exposures) > t.opts.MaxQueueSize {
		dropped = len(t.exposures) - t.opts.MaxQueueSize
		t.exposures = append([]core.ExposureEvent(nil), t.exposures[dropped:]...)
	}
	full := len(t.exposures) >= t.opts.BatchSize
	t.mu.Unlock()

	if dropped > 0 {
		t.logger.Warn("avsb: exposure queue full, dropped oldest events",
			"queueLimit", t.opts.MaxQueueSize, "dropped", dropped,
			"hint", "ingestion is unreachable or slower than this process produces events")
	}
	if full {
		t.requestFlush()
	}
}

// queueMetric enqueues a conversion or metric event.
func (t *eventTracker) queueMetric(event core.MetricEvent) {
	if event.EventID == "" {
		event.EventID = newEventID()
	}
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.metrics = append(t.metrics, event)
	dropped := 0
	if len(t.metrics) > t.opts.MaxQueueSize {
		dropped = len(t.metrics) - t.opts.MaxQueueSize
		t.metrics = append([]core.MetricEvent(nil), t.metrics[dropped:]...)
	}
	full := len(t.metrics) >= t.opts.BatchSize
	t.mu.Unlock()

	if dropped > 0 {
		t.logger.Warn("avsb: metric queue full, dropped oldest events",
			"queueLimit", t.opts.MaxQueueSize, "dropped", dropped,
			"hint", "ingestion is unreachable or slower than this process produces events")
	}
	if full {
		t.requestFlush()
	}
}

// requestFlush nudges the worker without blocking. A signal already pending is
// enough: the worker drains everything queued when it wakes.
func (t *eventTracker) requestFlush() {
	select {
	case t.flushSignal <- struct{}{}:
	default:
	}
}

// queued reports the current queue depths. Used by tests.
func (t *eventTracker) queued() (exposures, metrics int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.exposures), len(t.metrics)
}

// flush drains both queues and posts them in batches. It blocks until every
// batch has been attempted.
func (t *eventTracker) flush(ctx context.Context) {
	t.mu.Lock()
	exposures := t.exposures
	metrics := t.metrics
	t.exposures = nil
	t.metrics = nil
	t.mu.Unlock()

	if len(exposures) == 0 && len(metrics) == 0 {
		return
	}

	for start := 0; start < len(exposures); start += t.opts.BatchSize {
		end := start + t.opts.BatchSize
		if end > len(exposures) {
			end = len(exposures)
		}
		t.send(ctx, t.batchURL, exposureBatch{SDKKey: t.sdkKey, Events: exposures[start:end]})
	}
	for start := 0; start < len(metrics); start += t.opts.BatchSize {
		end := start + t.opts.BatchSize
		if end > len(metrics) {
			end = len(metrics)
		}
		t.send(ctx, t.metricURL, metricBatch{SDKKey: t.sdkKey, MetricEvents: metrics[start:end]})
	}
}

// close stops the worker and flushes what is left. Events queued after close
// are dropped rather than silently held forever.
func (t *eventTracker) close(ctx context.Context) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()

	close(t.stop)
	<-t.done
	t.flush(ctx)
}

// trackPurchase posts one order immediately, with the same retry ladder as a
// batch flush. Orders are not buffered: an order is the event a customer least
// wants sitting in a queue.
func (t *eventTracker) trackPurchase(ctx context.Context, visitorID string, order PurchaseOrder) error {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return fmt.Errorf("avsb: TrackPurchase called after Close, order %q was not sent", order.OrderID)
	}

	payload := purchasePayload{
		PurchaseOrder: order,
		EventID:       newEventID(),
		VisitorID:     visitorID,
		Timestamp:     time.Now().UnixMilli(),
		PageURL:       "",
	}
	return t.send(ctx, t.ordersURL, ordersBatch{SDKKey: t.sdkKey, Orders: []purchasePayload{payload}})
}

// send serialises the body ONCE and posts it, retrying on failure. Every
// attempt sends the identical bytes, so the event ids never change and
// ingestion can deduplicate a batch that was delivered twice.
func (t *eventTracker) send(ctx context.Context, url string, body interface{}) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		t.logger.Error("avsb: could not serialise an event batch, dropping it",
			"url", url, "error", err)
		return err
	}

	var backoff backoffState
	var pendingRetryAfter time.Duration
	var lastErr error

	for attempt := 0; attempt <= t.opts.RetryAttempts; attempt++ {
		retryAfter, err := t.attemptSend(ctx, url, encoded)
		if err == nil {
			return nil
		}
		lastErr = err
		if retryAfter > 0 {
			pendingRetryAfter = retryAfter
		}
		if ctx.Err() != nil {
			break
		}
		if attempt == t.opts.RetryAttempts {
			break
		}
		backoff = backoff.next(pendingRetryAfter)
		pendingRetryAfter = 0
		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(backoff.delay):
		}
	}

	t.logger.Error("avsb: giving up on an event batch after retries. The events in it are lost",
		"url", url,
		"retries", t.opts.RetryAttempts,
		"error", lastErr,
		"reproduce", fmt.Sprintf("curl -X POST %s -H 'Content-Type: application/json' -d '<the batch body>'", url),
	)
	return lastErr
}

// attemptSend performs one POST. It returns the Retry-After it was told to
// honour, if any, alongside the error.
func (t *eventTracker) attemptSend(ctx context.Context, url string, encoded []byte) (time.Duration, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, t.opts.SendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0, nil
	}
	return parseRetryAfter(resp.Header.Get("Retry-After")),
		fmt.Errorf("avsb: %s returned HTTP %d", url, resp.StatusCode)
}

// newEventID mints a UUID v4 from crypto/rand. Hand-rolled because the root
// module carries no dependencies, and a UUID is 16 bytes and two bit twiddles.
func newEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is not recoverable and not worth taking the
		// caller's request down for. A timestamp-derived id still lets
		// ingestion deduplicate a RETRY of this same batch, which is what the
		// id is for.
		return fmt.Sprintf("avsb-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant

	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}

// userAgent identifies this SDK on every outbound request.
func userAgent() string {
	return SDKType + "/" + SDKVersion()
}
