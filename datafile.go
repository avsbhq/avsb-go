package avsb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// ---------------------------------------------------------------------------
// Datafile loading, polling, streaming, heartbeat
// ---------------------------------------------------------------------------

func (s *Server) datafile() *core.Datafile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.df
}

func (s *Server) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// loadBytes parses raw datafile JSON, swaps it in, and makes sure the event
// tracker points at the endpoint this datafile published.
func (s *Server) loadBytes(raw []byte) error {
	var df core.Datafile
	if err := json.Unmarshal(raw, &df); err != nil {
		return fmt.Errorf("avsb: the datafile is not valid JSON: %w", err)
	}

	s.mu.Lock()
	previous := s.df
	s.df = &df
	needsTracker := s.tracker == nil && !s.opts.Events.Disabled && !s.closed
	sdkKey := s.opts.SDKKey
	if sdkKey == "" {
		sdkKey = df.SDKKey
	}
	s.mu.Unlock()

	if needsTracker {
		collectEndpoint := s.opts.CollectEndpoint
		if collectEndpoint == "" {
			collectEndpoint = df.CollectEndpoint
		}
		tracker, err := newEventTracker(sdkKey, collectEndpoint, s.opts.Events, s.httpClient, s.logger)
		if err != nil {
			s.logger.Error("avsb: could not start the event tracker, so no exposures or "+
				"conversions will be recorded", "error", err)
		} else {
			s.mu.Lock()
			if s.tracker == nil && !s.closed {
				s.tracker = tracker
			} else {
				// Lost a race, or closed while we were building: do not leak
				// the worker goroutine.
				go tracker.close(context.Background())
			}
			s.mu.Unlock()
		}
	}

	if previous != nil && previous.PublishedAt != df.PublishedAt {
		s.logger.Info("avsb: datafile updated", "publishedAt", df.PublishedAt)
	}
	return nil
}

// fetchDatafile performs one datafile GET.
//
// The request is keyless by design: the SDK key is the path segment, and the
// CDN serves the object at that path. There is no header, no query string, and
// no Authorization; the X-SDK-Key header this SDK used to send was read by
// nothing.
func (s *Server) fetchDatafile(ctx context.Context) error {
	if s.datafileURL == "" {
		return errors.New("avsb: no datafile URL to fetch: set ServerOptions.SDKKey")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.datafileURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf(
			"could not reach the datafile at %s: %w. Check outbound network access and "+
				"any egress proxy, then reproduce with: curl -i %s",
			s.datafileURL, err, s.datafileURL)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf(
			"the datafile at %s returned 404. That means the SDK key %s has no published "+
				"datafile: check the key in Environments in the project sidebar, and publish the "+
				"environment at least once. Reproduce with: curl -i %s",
			s.datafileURL, DescribeSDKKey(s.opts.SDKKey), s.datafileURL)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"the datafile at %s returned HTTP %d. Reproduce with: curl -i %s",
			s.datafileURL, resp.StatusCode, s.datafileURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read the datafile body from %s: %w", s.datafileURL, err)
	}
	if err := s.loadBytes(body); err != nil {
		return err
	}

	s.sendHeartbeat(ctx)
	return nil
}

// startBackgroundLoops starts polling and, when enabled, streaming.
func (s *Server) startBackgroundLoops() {
	s.mu.Lock()
	if s.closed || s.stop != nil {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	s.mu.Unlock()

	if !s.opts.PollingDisabled && s.datafileURL != "" {
		s.pollWG.Add(1)
		go func() {
			defer s.pollWG.Done()
			s.pollLoop(loopCtx)
		}()
	}
	if s.opts.StreamingEnabled {
		s.startStreaming(loopCtx)
	}
}

// pollLoop refetches the datafile forever, with jitter.
//
// It keeps running after a failed fetch, which is the difference between a
// transient CDN blip and a process that serves defaults until someone restarts
// it.
func (s *Server) pollLoop(ctx context.Context) {
	interval := s.opts.PollingInterval
	if interval <= 0 {
		interval = defaultPollingInterval
	}
	if interval < minPollingInterval {
		s.logger.Warn("avsb: PollingInterval is below the 5s minimum, using 5s",
			"requested", interval.String())
		interval = minPollingInterval
	}

	var backoff backoffState
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitteredInterval(interval)):
		}

		if err := s.fetchDatafile(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			backoff = backoff.next(0)
			s.logger.Warn("avsb: datafile refresh failed, retrying",
				"error", err, "retryIn", backoff.delay.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.delay):
			}
			continue
		}
		backoff = backoff.reset()
	}
}

// sendHeartbeat reports SDK liveness so the dashboard's Environments page can
// show Connected instead of Not connected.
//
// The endpoint comes from the datafile. When the datafile carries none, nothing
// is sent: a proxied deployment publishes its own origin, and posting to the
// public app host instead would report liveness nowhere while looking healthy.
func (s *Server) sendHeartbeat(ctx context.Context) {
	df := s.datafile()
	if df == nil {
		return
	}
	endpoint := ResolveHeartbeatEndpoint(df.HeartbeatEndpoint)
	if endpoint == "" {
		return
	}
	sdkKey := s.opts.SDKKey
	if sdkKey == "" {
		sdkKey = df.SDKKey
	}
	if sdkKey == "" {
		return
	}

	body, err := json.Marshal(heartbeatBody{
		SDKKey:     sdkKey,
		SDKType:    SDKType,
		SDKVersion: SDKVersion(),
	})
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		// A heartbeat is liveness telemetry, not a feature. It is logged at
		// debug so a blocked egress path to the dashboard host never floods a
		// production log, and never blocks evaluation.
		s.logger.Debug("avsb: heartbeat failed", "endpoint", endpoint, "error", err)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.logger.Debug("avsb: heartbeat rejected", "endpoint", endpoint, "status", resp.StatusCode)
	}
}

// heartbeatBody is the locked heartbeat payload.
type heartbeatBody struct {
	SDKKey     string `json:"sdkKey"`
	SDKType    string `json:"sdkType"`
	SDKVersion string `json:"sdkVersion"`
}

// startStreaming resolves the stream endpoint and connects, or explains once
// why it cannot.
func (s *Server) startStreaming(ctx context.Context) {
	df := s.datafile()
	fromDatafile := ""
	if df != nil {
		fromDatafile = df.StreamEndpoint
	}
	resolution := ResolveStreamEndpoint(s.opts.StreamingEndpoint, fromDatafile)
	if !resolution.Available {
		s.warnedNoStream.Do(func() {
			s.logger.Warn(resolution.Reason)
		})
		return
	}

	client := &streamingClient{
		ticketEndpoint: resolution.Endpoint,
		sdkKey:         s.opts.SDKKey,
		httpClient:     s.streamHTTPClient,
		logger:         s.logger,
		onDatafileUpdated: func() {
			// The stream carries a NOTIFICATION, not the datafile, so the
			// update is a refetch. Treating the SSE payload as a datafile was
			// wrong: the frame is {"type":"datafile_updated"}.
			if err := s.fetchDatafile(ctx); err != nil {
				s.logger.Warn("avsb: refetch after a stream update failed", "error", err)
			}
		},
	}
	s.pollWG.Add(1)
	go func() {
		defer s.pollWG.Done()
		client.start(ctx)
	}()
}
