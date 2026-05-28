package avsb

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// sseEvent is one parsed Server-Sent Event.
type sseEvent struct {
	Event string
	Data  string
}

// streamingClient maintains a persistent SSE connection to the datafile
// streaming endpoint and calls onUpdate whenever a new datafile arrives.
type streamingClient struct {
	url      string
	sdkKey   string
	logger   *slog.Logger
	onUpdate func([]byte)
}

// Start begins the streaming loop. It blocks until ctx is cancelled.
// On any connection error it backs off with capped exponential backoff
// and reconnects automatically.
func (s *streamingClient) Start(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := s.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Warn("avsb: SSE connection failed, retrying",
				"error", err,
				"backoff", backoff.String(),
			)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (s *streamingClient) connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return fmt.Errorf("avsb: create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if s.sdkKey != "" {
		req.Header.Set("X-SDK-Key", s.sdkKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("avsb: SSE request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("avsb: SSE endpoint returned %d", resp.StatusCode)
	}

	return s.readStream(ctx, resp.Body)
}

func (s *streamingClient) readStream(ctx context.Context, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	var ev sseEvent

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Text()
		if line == "" {
			// Blank line — dispatch the accumulated event.
			if ev.Data != "" {
				s.handleEvent(ev)
			}
			ev = sseEvent{}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			ev.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			ev.Data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("avsb: SSE read: %w", err)
	}
	return io.EOF
}

func (s *streamingClient) handleEvent(ev sseEvent) {
	switch ev.Event {
	case "datafile", "":
		// Unmarshal to verify it is valid JSON before passing upstream.
		if !json.Valid([]byte(ev.Data)) {
			s.logger.Warn("avsb: received invalid JSON from SSE stream")
			return
		}
		s.onUpdate([]byte(ev.Data))
	}
}
