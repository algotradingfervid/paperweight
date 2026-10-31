package app

import (
	"time"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MsgMethodNotAllowed is the 405's text.
const MsgMethodNotAllowed = msgMethodNotAllowed

// Mount is mount, for the test that it refuses a route's middleware.
var Mount = mount

// JobEvery is how often Run does the jobs.
func (a *App) JobEvery() time.Duration { return a.jobEvery }

// SetJobEvery makes Run do the jobs every d, so a test needn't wait an hour.
func (a *App) SetJobEvery(d time.Duration) { a.jobEvery = d }

// DocDeps is the generic document screens' Deps as routes built them.
func (a *App) DocDeps() docscreen.Deps { return a.docDeps() }
