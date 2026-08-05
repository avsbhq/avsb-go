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

// Server-Sent Events client for datafile-change notifications.
//
// Mirrors packages/avsb-utils/src/streaming.ts. The handshake is two steps,
// because an SSE client cannot set request headers:
//
//  1. GET {ticketEndpoint}?sdkKey=... which answers {url, token, expiresInMs}
//  2. GET {url}?sdkKey=...&token=... which is the stream
//
// The frames carry a NOTIFICATION, not a datafile: {"type":"datafile_updated"}.
// The SDK refetches the datafile from the CDN when one arrives, so the stream
// stays tiny and the datafile stays cacheable. Treating the frame as a datafile
// (which this client used to do) meant every "update" replaced the datafile
// with a two-field object.

const (
	// sseMaxLineBytes is the largest single SSE line the scanner accepts.
	// bufio.Scanner defaults to 64KB, and a datafile-sized line silently
	// killed the stream forever with a "token too long" error nothing surfaced.
	// Notifications are tiny today; the headroom costs nothing and removes a
	// whole class of permanent failure.
	sseMaxLineBytes = 4 << 20 // 4MB
	// sseInitialBufBytes is the starting scanner buffer.
	sseInitialBufBytes = 64 << 10 // 64KB
)

// ticketResponse is step one's answer.
type ticketResponse struct {
	// URL is the stream address to subscribe to. It is not necessarily on the
	// same host as the ticket endpoint.
	URL string `json:"url"`
	// Token is single-use and short lived, which is why it rides in the query
	// string rather than a header.
	Token string `json:"token"`
	// ExpiresInMs is how long the token is valid for.
	ExpiresInMs int64 `json:"expiresInMs"`
}

// streamNotification is one parsed SSE data frame.
type streamNotification struct {
	Type        string `json:"type"`
	PublishedAt string `json:"publishedAt"`
}

// streamingClient keeps an SSE connection open and calls onDatafileUpdated
// whenever the server says the datafile changed.
type streamingClient struct {
	ticketEndpoint    string
	sdkKey            string
	httpClient        *http.Client
	logger            *slog.Logger
	onDatafileUpdated func()
}

// start runs the connect loop until ctx is done. It is meant to run in its own
// goroutine.
func (s *streamingClient) start(ctx context.Context) {
	var backoff backoffState

	for {
		if ctx.Err() != nil {
			return
		}

		connected, err := s.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if connected {
			// The connection worked and then ended, which is normal: tokens
			// expire and load balancers recycle connections. Reset the curve
			// so a healthy reconnect happens in about a second rather than at
			// whatever delay the last outage climbed to. Not resetting is the
			// bug that left a stream reconnecting once a minute forever after
			// one bad hour.
			backoff = backoff.reset()
		}
		if err != nil {
			s.logger.Warn("avsb: streaming connection failed, falling back to polling until it recovers",
				"error", err)
		}

		backoff = backoff.next(0)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff.delay):
		}
	}
}

// connectOnce performs the handshake and pumps frames until the stream ends.
// The bool reports whether the stream was ever established, which is what
// decides if the backoff resets.
func (s *streamingClient) connectOnce(ctx context.Context) (bool, error) {
	ticket, err := s.fetchTicket(ctx)
	if err != nil {
		return false, err
	}

	streamURL, err := StreamSubscribeURL(ticket.URL, s.sdkKey, ticket.Token)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", userAgent())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("avsb: could not open the event stream at %s: %w", ticket.URL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return false, fmt.Errorf(
			"avsb: the event stream rejected the ticket token (401). It expires in about a "+
				"minute, so this is usually a clock or a proxy stripping the query string")
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("avsb: the event stream at %s returned HTTP %d", ticket.URL, resp.StatusCode)
	}

	s.logger.Info("avsb: streaming connected", "endpoint", s.ticketEndpoint)
	return true, s.pumpFrames(ctx, resp.Body)
}

// fetchTicket performs step one of the handshake.
func (s *streamingClient) fetchTicket(ctx context.Context) (*ticketResponse, error) {
	ticketURL, err := StreamTicketURL(s.ticketEndpoint, s.sdkKey)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ticketURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("avsb: could not reach the stream ticket endpoint %s: %w", s.ticketEndpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"avsb: the stream ticket endpoint %s returned HTTP %d",
			s.ticketEndpoint, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	var ticket ticketResponse
	if err := json.Unmarshal(body, &ticket); err != nil {
		return nil, fmt.Errorf("avsb: the stream ticket response is not valid JSON: %w", err)
	}
	if ticket.URL == "" || ticket.Token == "" {
		return nil, fmt.Errorf(
			"avsb: the stream ticket response from %s is missing url or token",
			s.ticketEndpoint)
	}
	return &ticket, nil
}

// pumpFrames reads SSE frames until the stream ends or ctx is done.
func (s *streamingClient) pumpFrames(ctx context.Context, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, sseInitialBufBytes), sseMaxLineBytes)

	var dataLines []string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}

		line := scanner.Text()
		if line == "" {
			// A blank line ends the frame. Per the SSE spec multiple data
			// lines join with a newline BETWEEN them, not by concatenation:
			// gluing them together corrupts any multi-line payload.
			if len(dataLines) > 0 {
				s.handleData(strings.Join(dataLines, "\n"))
				dataLines = dataLines[:0]
			}
			continue
		}
		// Comment lines (": keep-alive") carry no data.
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("avsb: reading the event stream failed: %w", err)
	}
	// A clean end of stream. Normal, and the caller reconnects.
	return nil
}

// handleData reacts to one complete data payload.
func (s *streamingClient) handleData(data string) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return
	}
	var notification streamNotification
	if err := json.Unmarshal([]byte(trimmed), &notification); err != nil {
		s.logger.Warn("avsb: could not parse a stream frame, ignoring it", "frame", trimmed)
		return
	}
	if notification.Type == "datafile_updated" && s.onDatafileUpdated != nil {
		s.onDatafileUpdated()
	}
}
