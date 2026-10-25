package docscreen

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// SampleIDs are the seeded documents the sweep visits, one per state plus a
// draft with 500 lines.
type SampleIDs struct {
	Draft, Pending, Approved, Rejected, Cancelled, Closed, Lines500 int64
}

// sweepUser is the fixture user the sweep signs in as.
const sweepUser = "priya@vardhan.test"

// Screens lists the docscreen pages of the DocType name for the overflow sweep.
// Tasks 13 and 14 add the document, wizard and line pages.
func Screens(name string, _ SampleIDs) []screens.Screen {
	const route = "/doc/{doctype}"
	base := "/doc/" + name
	return []screens.Screen{
		{Name: name + " list", Route: route, URL: base, As: sweepUser},
		{Name: name + " list empty", Route: route, URL: base + "?q=no-such-thing-zz", As: sweepUser},
		{Name: name + " list filtered", Route: route, URL: base + "?" + ListQuery{
			Filters: []Filter{{Field: "state", Op: "=", Values: []string{string(doctype.StateApproved), string(doctype.StatePending)}}},
		}.Encode(), As: sweepUser},
	}
}
