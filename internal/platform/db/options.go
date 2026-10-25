package db

import "time"

// Option configures Open.
type Option func(*options)

type options struct {
	busyTimeout time.Duration
}

// WithBusyTimeout sets the SQLite busy timeout (CD-08). Default 5 s.
// STUB: has no effect yet.
func WithBusyTimeout(d time.Duration) Option {
	return func(*options) {}
}
