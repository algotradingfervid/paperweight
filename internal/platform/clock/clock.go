// Package clock provides the time source for the whole application.
// Production code takes a Clock and never calls time.Now directly, so tests
// can use a Fixed clock.
package clock

import (
	"sync"
	"time"
	_ "time/tzdata" // embed the zone database so IST loads without system zoneinfo
)

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// Real is the wall clock.
type Real struct{}

// Now returns the current wall-clock time.
func (Real) Now() time.Time { return time.Now() }

// Fixed is a manually controlled clock for tests. It is safe for concurrent use.
type Fixed struct {
	mu  sync.Mutex
	now time.Time
}

// NewFixed returns a Fixed clock set to t.
func NewFixed(t time.Time) *Fixed { return &Fixed{now: t} }

// Now returns the clock's current time.
func (f *Fixed) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the clock forward by d.
func (f *Fixed) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set moves the clock to t.
func (f *Fixed) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// IST is India Standard Time (Asia/Kolkata), loaded from the embedded zone database.
var IST = mustLoad("Asia/Kolkata")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("clock: cannot load time zone " + name + " from the embedded zone database (" + err.Error() + "); check that time/tzdata is still imported")
	}
	return loc
}
