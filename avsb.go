package avsb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

const (
	defaultPollingInterval = 30 * time.Second
	defaultHTTPTimeout     = 10 * time.Second
)

// minPollingInterval is the floor on ServerOptions.PollingInterval: anything
// tighter is a hot loop against the CDN for no benefit, because a publish takes
// longer than that to propagate anyway.
//
// It is a variable rather than a constant so this package's own tests can drive
// the polling loop in milliseconds instead of spending five real seconds waiting
// for one tick. Nothing outside this package can change it.
var minPollingInterval = 5 * time.Second

// Server is the A vs B SDK entry point. It holds the current datafile and
// evaluates flag decisions against it.
//
// Create ONE Server per process and share it across goroutines: it is safe for
// concurrent use, and a second instance means a second datafile fetch, a second
// polling loop, and two event queues that each hold half your exposures.
type Server struct {
	opts   ServerOptions
	logger *slog.Logger

	// datafileURL is resolved once at construction from the endpoint contract.
	datafileURL string
	// httpClient bounds every datafile fetch and heartbeat.
	httpClient *http.Client
	// streamClient uses a transport with no overall deadline, because an SSE
	// response body is meant to stay open.
	streamHTTPClient *http.Client

	mu      sync.RWMutex
	df      *core.Datafile
	log     DecisionLog
	tracker *eventTracker
	// overrides is the runtime-override table, keyed by flag key.
	overrides map[string]*flagOverrides
	closed    bool

	// stop cancels the polling loop and the streaming connection.
	stop     context.CancelFunc
	stopOnce sync.Once
	// warnedEmptyContext makes the empty-context warning once per process.
	warnedEmptyContext sync.Once
	// warnedNoStream makes the "streaming has nowhere to connect" warning once.
	warnedNoStream sync.Once
	// pollWG waits for the background loops on Close.
	pollWG sync.WaitGroup
}

// flagOverrides is one flag's runtime overrides: per identity, plus a global.
type flagOverrides struct {
	// perKindKey maps "<kind>:<key>" to a variation key.
	perKindKey map[string]string
	// global applies to every context that has no per-identity entry.
	global string
}

// NewServer creates a Server. It makes no network calls and never blocks: call
// OnReady to load the first datafile.
//
// A key that does not look like an SDK key is logged loudly and used anyway,
// because the mint format can grow and the datafile fetch is the real arbiter.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.SDKKey == "" && len(opts.Bootstrap) == 0 && opts.DatafileURL == "" {
		return nil, errors.New(
			"avsb: ServerOptions.SDKKey is required. Copy the environment SDK key " +
				"(it looks like \"sdk_production_xxxxxxxx\") from Settings, then Environments, " +
				"in the A vs B dashboard. Pass ServerOptions.Bootstrap instead to run " +
				"offline from a datafile you already hold")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if opts.SDKKey != "" {
		if ok, message := ValidateSDKKeyShape(opts.SDKKey); !ok {
			logger.Error(message)
		}
	}

	datafileURL := opts.DatafileURL
	if datafileURL == "" && opts.SDKKey != "" {
		built, err := DatafileURL(opts.SDKKey, opts.CDNHost)
		if err != nil {
			return nil, err
		}
		datafileURL = built
	}

	timeout := opts.HTTPTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}

	s := &Server{
		opts:        opts,
		logger:      logger,
		datafileURL: datafileURL,
		httpClient:  &http.Client{Timeout: timeout},
		// An SSE body stays open for as long as the stream lives, so the
		// overall client timeout has to be zero here. The handshake is still
		// bounded: ResponseHeaderTimeout covers connect plus first byte.
		streamHTTPClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: timeout,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		log:       noopDecisionLog{},
		overrides: map[string]*flagOverrides{},
	}

	if len(opts.Bootstrap) > 0 {
		if err := s.loadBytes(opts.Bootstrap); err != nil {
			return nil, fmt.Errorf("avsb: ServerOptions.Bootstrap is not a valid datafile: %w", err)
		}
	}
	return s, nil
}

// SetDecisionLog replaces the decision-log handler. Safe to call before or
// after OnReady.
func (s *Server) SetDecisionLog(dl DecisionLog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dl == nil {
		dl = noopDecisionLog{}
	}
	s.log = dl
}

// OnReady loads the first datafile and starts the background loops. It blocks
// until a datafile is loaded or ctx is done.
//
// With a bootstrap datafile it returns immediately, and the first fetch happens
// on the polling loop, so a CDN blip at startup cannot stop the process from
// booting.
func (s *Server) OnReady(ctx context.Context) error {
	if s.isClosed() {
		return errors.New("avsb: OnReady was called after Close. Create a new Server")
	}

	bootstrapped := s.datafile() != nil

	var fetchErr error
	if s.datafileURL != "" {
		fetchErr = s.fetchDatafile(ctx)
	}

	if fetchErr != nil && !bootstrapped {
		// No datafile at all. Every evaluation returns the caller's default
		// with source not_ready until a poll succeeds, which the background
		// loop keeps trying.
		s.startBackgroundLoops()
		return fmt.Errorf("avsb: OnReady could not load a datafile: %w", fetchErr)
	}
	if fetchErr != nil {
		s.logger.Warn("avsb: serving the bootstrap datafile because the first fetch failed",
			"error", fetchErr)
	}

	s.startBackgroundLoops()
	return nil
}

// Close stops the polling loop and the streaming connection, then flushes
// queued events. Call it before the process exits, or the last batch of
// exposures and conversions never leaves.
//
// After Close every evaluation returns the caller's default with source
// not_ready.
func (s *Server) Close() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		stop := s.stop
		tracker := s.tracker
		s.mu.Unlock()

		if stop != nil {
			stop()
		}
		s.pollWG.Wait()
		if tracker != nil {
			// A fresh context: the caller's may already be cancelled, and a
			// cancelled context here would discard the final flush, which is
			// the batch that matters most.
			flushCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			tracker.close(flushCtx)
		}
	})
}

// Flush sends every queued event now and waits for the attempt to finish. Use
// it before a deploy drains a process, or in a test that asserts delivery.
func (s *Server) Flush(ctx context.Context) {
	s.mu.RLock()
	tracker := s.tracker
	s.mu.RUnlock()
	if tracker != nil {
		tracker.flush(ctx)
	}
}
