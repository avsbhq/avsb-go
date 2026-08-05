package avsb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// ---------------------------------------------------------------------------
// Event tracking
// ---------------------------------------------------------------------------

// TrackEvent describes one conversion or metric event.
//
// There is deliberately no Properties field: the metric endpoint has none, and
// anything sent under that name is stripped before storage, so it would look
// delivered and be gone. Put the number in Value.
type TrackEvent struct {
	// Context identifies the visitor. Its primary key becomes the visitor id,
	// which is what joins this conversion to the visitor's exposures.
	Context core.EvalContext
	// VisitorID sets the visitor id directly, overriding Context. One of the
	// two is required: an event with no visitor cannot be attributed.
	VisitorID string
	// Revenue is decimal major units (99.95, not 9995), or nil for a
	// non-revenue conversion.
	Revenue *float64
	// Value is the numeric metric value for average-value metrics, nil when
	// the metric counts occurrences.
	Value *float64
}

// Track records a conversion or metric event.
//
// eventName is the metric key configured in the dashboard. An unknown key is
// accepted and stored, so a typo shows up as a metric with no data rather than
// an error, which is why it is worth copying the key from the dashboard.
//
// The event is queued and flushed in a batch. Call Flush or Close to force
// delivery.
func (s *Server) Track(ctx context.Context, eventName string, event TrackEvent) {
	if eventName == "" {
		s.logger.Warn("avsb: Track was called with an empty event name, dropping the event",
			"hint", "pass the metric key from the dashboard, for example \"signup_completed\"")
		return
	}

	visitorID := event.VisitorID
	if visitorID == "" {
		visitorID = visitorIDFromContext(event.Context)
	}
	if visitorID == "" {
		s.logger.Warn("avsb: Track was called with no visitor, dropping the event",
			"eventName", eventName,
			"hint", "set TrackEvent.Context (avsb.UserContext(id, nil)) or TrackEvent.VisitorID; "+
				"an event with no visitor cannot be joined to an exposure")
		return
	}

	s.mu.RLock()
	tracker := s.tracker
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		s.logger.Warn("avsb: Track was called after Close, dropping the event", "eventName", eventName)
		return
	}
	if tracker == nil {
		if s.opts.Events.Disabled {
			// Deliberate, so it is not a warning: the caller switched delivery
			// off and expects nothing to be sent.
			s.logger.Debug("avsb: event delivery is disabled, dropping the event",
				"eventName", eventName)
			return
		}
		s.logger.Warn("avsb: Track was called before a datafile loaded, dropping the event",
			"eventName", eventName,
			"hint", "await OnReady before tracking; the event endpoint comes from the datafile")
		return
	}

	tracker.queueMetric(core.MetricEvent{
		VisitorID: visitorID,
		EventName: eventName,
		Revenue:   event.Revenue,
		Value:     event.Value,
		Timestamp: time.Now().UnixMilli(),
		PageURL:   "",
	})
}

// TrackPurchase records a purchase. Unlike Track it sends immediately rather
// than batching, because an order is the event least worth buffering, and it
// blocks until the send has been attempted (with the same retry ladder as a
// batch flush) so the caller can log a failure. Run it in a goroutine if the
// request path must not wait.
func (s *Server) TrackPurchase(ctx context.Context, visitorID string, order PurchaseOrder) error {
	if visitorID == "" {
		return errors.New("avsb: TrackPurchase needs a visitorID: an order with no visitor cannot be attributed")
	}
	if order.OrderID == "" {
		return errors.New("avsb: TrackPurchase needs order.OrderID, which is what deduplicates a retried purchase")
	}
	if order.Total <= 0 {
		return fmt.Errorf(
			"avsb: TrackPurchase got order.Total %v for order %q. Totals are decimal major "+
				"units greater than zero (99.95, not 9995), and a zero or negative total is rejected by ingestion",
			order.Total, order.OrderID)
	}

	s.mu.RLock()
	tracker := s.tracker
	s.mu.RUnlock()
	if tracker == nil {
		return fmt.Errorf(
			"avsb: TrackPurchase was called before a datafile loaded, so order %q was not sent. "+
				"Await OnReady first: the order endpoint comes from the datafile", order.OrderID)
	}
	return tracker.trackPurchase(ctx, visitorID, order)
}
