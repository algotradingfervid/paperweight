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

// NavIcon is the sidebar icon of DocType name, and whether it has one.
func NavIcon(name string) (string, bool) {
	i := docNavIndex(name)
	if i < 0 {
		return "", false
	}
	return docNav[i].icon, true
}

// ListURL is a DocType's list path.
var ListURL = listURL

// HasScreen reports whether the master list at href has screens.
func (a *App) HasScreen(href string) bool { return a.screens[href] }
