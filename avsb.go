package avsb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

const defaultDatafileBaseURL = "https://datafile.avsb.cloud/v2/"
const defaultStreamingBaseURL = "https://stream.avsb.cloud/v2/"

// Server is the primary A vs B SDK entry point. It holds a copy of the
// current datafile and evaluates flag decisions against it.
//
// Create one Server per application and share it across goroutines — it is
// safe for concurrent use after OnReady returns.
type Server struct {
	opts    ServerOptions
	logger  *slog.Logger
	log     DecisionLog
	mu      sync.RWMutex
	df      *core.Datafile
	stopSSE context.CancelFunc
}

// NewServer creates a Server from the supplied options. It does not block or
// make network requests; call OnReady to fetch the initial datafile.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.SDKKey == "" && opts.InlineDatafile == nil && opts.DatafileURL == "" {
		return nil, errors.New("avsb: ServerOptions.SDKKey must be non-empty unless InlineDatafile or DatafileURL is set")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		opts:   opts,
		logger: logger,
		log:    noopDecisionLog{},
	}
	return s, nil
}

// SetDecisionLog replaces the decision log handler. Safe to call before or
// after OnReady.
func (s *Server) SetDecisionLog(dl DecisionLog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = dl
}

// OnReady fetches the initial datafile and optionally starts the SSE
// streaming connection. It blocks until a valid datafile has been loaded or
// the context is cancelled.
func (s *Server) OnReady(ctx context.Context) error {
	if len(s.opts.InlineDatafile) > 0 {
		return s.loadBytes(s.opts.InlineDatafile)
	}

	url := s.opts.DatafileURL
	if url == "" {
		url = defaultDatafileBaseURL + s.opts.SDKKey
	}

	if err := s.fetchDatafile(ctx, url); err != nil {
		return fmt.Errorf("avsb: OnReady: %w", err)
	}

	// Start SSE streaming when enabled (default on when SDKKey is set).
	streaming := s.opts.StreamingEnabled == nil || *s.opts.StreamingEnabled
	if streaming && s.opts.SDKKey != "" {
		streamURL := defaultStreamingBaseURL + s.opts.SDKKey
		sseCtx, cancel := context.WithCancel(context.Background())
		s.stopSSE = cancel

		client := &streamingClient{
			url:    streamURL,
			sdkKey: s.opts.SDKKey,
			logger: s.logger,
			onUpdate: func(raw []byte) {
				if err := s.loadBytes(raw); err != nil {
					s.logger.Warn("avsb: failed to load streamed datafile", "error", err)
				}
			},
		}
		go client.Start(sseCtx)
	}

	return nil
}

// Close shuts down the SSE streaming connection (when active).
func (s *Server) Close() {
	if s.stopSSE != nil {
		s.stopSSE()
	}
}

// GetBoolFlag evaluates a boolean flag. defaultValue is returned when the
// flag is not found, disabled, or the evaluation fails.
func (s *Server) GetBoolFlag(ctx context.Context, key string, defaultValue bool, opts ...Option) Flag[bool] {
	raw, _ := json.Marshal(defaultValue)
	eval := s.evaluate(ctx, key, raw, opts...)
	var val bool
	if err := json.Unmarshal(eval.Value, &val); err != nil {
		val = defaultValue
	}
	return buildFlag(val, eval)
}

// GetStringFlag evaluates a string flag.
func (s *Server) GetStringFlag(ctx context.Context, key string, defaultValue string, opts ...Option) Flag[string] {
	raw, _ := json.Marshal(defaultValue)
	eval := s.evaluate(ctx, key, raw, opts...)
	var val string
	if err := json.Unmarshal(eval.Value, &val); err != nil {
		val = defaultValue
	}
	return buildFlag(val, eval)
}

// GetNumberFlag evaluates a numeric flag (returned as float64).
func (s *Server) GetNumberFlag(ctx context.Context, key string, defaultValue float64, opts ...Option) Flag[float64] {
	raw, _ := json.Marshal(defaultValue)
	eval := s.evaluate(ctx, key, raw, opts...)
	var val float64
	if err := json.Unmarshal(eval.Value, &val); err != nil {
		val = defaultValue
	}
	return buildFlag(val, eval)
}

// GetJSONFlag evaluates a flag and returns its raw JSON value. The caller is
// responsible for deserialising into the concrete type.
func (s *Server) GetJSONFlag(ctx context.Context, key string, defaultValue json.RawMessage, opts ...Option) Flag[json.RawMessage] {
	eval := s.evaluate(ctx, key, defaultValue, opts...)
	return buildFlag(eval.Value, eval)
}

// evaluate is the shared evaluation path for all Get*Flag methods.
func (s *Server) evaluate(
	_ context.Context,
	key string,
	defaultValue json.RawMessage,
	opts ...Option,
) core.FlagEvaluation {
	co := applyCallOpts(opts)

	includeReasons := s.opts.IncludeReasons
	if co.includeReasons != nil {
		includeReasons = *co.includeReasons
	}

	evalCtx := co.ctx
	if evalCtx == nil {
		evalCtx = EvalContext{}
	}

	start := time.Now()

	df := s.datafile()
	if df == nil {
		return core.FlagEvaluation{
			Value:   defaultValue,
			Source:  "not_found",
			FlagKey: key,
			Reasons: []string{"datafile not loaded"},
		}
	}

	result := core.EvaluateFlag(key, defaultValue, evalCtx, df, core.EvaluateOptions{
		IncludeReasons: includeReasons,
		StickyBucketService: s.opts.StickyBucketService,
	})

	s.log.LogDecision(DecisionLogEntry{
		FlagKey:        result.FlagKey,
		VariationKey:   ptrToStr(result.VariationKey),
		Source:         result.Source,
		RuleID:         ptrToStr(result.RuleID),
		RuleType:       ptrToStr(result.RuleType),
		Reasons:        result.Reasons,
		EvaluatedAt:    time.Now(),
		DurationMicros: time.Since(start).Microseconds(),
		ContextSummary: core.ContextSummary(evalCtx),
	})

	return result
}

// datafile returns the current datafile under a read lock.
func (s *Server) datafile() *core.Datafile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.df
}

// loadBytes deserialises raw JSON bytes into a Datafile and swaps it in.
func (s *Server) loadBytes(raw []byte) error {
	var df core.Datafile
	if err := json.Unmarshal(raw, &df); err != nil {
		return fmt.Errorf("avsb: parse datafile: %w", err)
	}
	s.mu.Lock()
	s.df = &df
	s.mu.Unlock()
	return nil
}

// fetchDatafile performs a one-shot HTTP GET to retrieve the datafile.
func (s *Server) fetchDatafile(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if s.opts.SDKKey != "" {
		req.Header.Set("X-SDK-Key", s.opts.SDKKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("datafile endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return s.loadBytes(body)
}

// buildFlag converts a core.FlagEvaluation into a typed Flag[T].
func buildFlag[T any](value T, eval core.FlagEvaluation) Flag[T] {
	return Flag[T]{
		value:        value,
		variationKey: eval.VariationKey,
		source:       eval.Source,
		ruleID:       eval.RuleID,
		ruleType:     eval.RuleType,
		reasons:      eval.Reasons,
		flagKey:      eval.FlagKey,
	}
}

func ptrToStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
