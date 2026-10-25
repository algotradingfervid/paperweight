package db

import "time"

const defaultBusyTimeout = 5 * time.Second

// Option configures Open.
type Option func(*options)

type options struct {
	busyTimeout time.Duration
}

// WithBusyTimeout sets the SQLite busy timeout (CD-08). Default 5 s.
func WithBusyTimeout(d time.Duration) Option {
	return func(o *options) { o.busyTimeout = d }
}
