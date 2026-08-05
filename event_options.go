package avsb

import "time"

// EventOptions tunes the batched event tracker.
type EventOptions struct {
	// FlushInterval is how often the queue drains on its own. Default 5s.
	FlushInterval time.Duration
	// BatchSize is the queue length that triggers an immediate flush, and the
	// maximum events per request. Default 20, capped at the ingest limit of
	// 100 events per batch.
	BatchSize int
	// MaxQueueSize bounds the queue. Past it the OLDEST events are dropped
	// with a warning naming the count: a full queue means ingestion is
	// unreachable, and holding a process's memory hostage to that is worse
	// than losing the stalest events.
	MaxQueueSize int
	// RetryAttempts is how many times a failed batch is retried after the
	// first attempt. Default 3. Pass a NEGATIVE value to disable retrying
	// entirely, because zero means "unset" here and a silent 3 is friendlier
	// than a silent 0 for everyone who never sets it.
	RetryAttempts int
	// SendTimeout bounds one delivery attempt. Default 10s.
	SendTimeout time.Duration
	// Disabled turns event delivery off entirely. Evaluation still works;
	// nothing is queued and nothing is sent. Use it in tests and in offline
	// deployments, and expect empty experiment results.
	Disabled bool
}

// maxEventsPerBatch is the ingestion worker's hard per-request limit.
const maxEventsPerBatch = 100

func (o EventOptions) withDefaults() EventOptions {
	if o.FlushInterval <= 0 {
		o.FlushInterval = 5 * time.Second
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 20
	}
	if o.BatchSize > maxEventsPerBatch {
		o.BatchSize = maxEventsPerBatch
	}
	if o.MaxQueueSize <= 0 {
		o.MaxQueueSize = 10000
	}
	if o.RetryAttempts < 0 {
		o.RetryAttempts = 0
	} else if o.RetryAttempts == 0 {
		o.RetryAttempts = 3
	}
	if o.SendTimeout <= 0 {
		o.SendTimeout = 10 * time.Second
	}
	return o
}
