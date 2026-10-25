package docscreen

import (
	"strconv"

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
// Task 14 adds the wizard and line pages.
func Screens(name string, ids SampleIDs) []screens.Screen {
	const route = "/doc/{doctype}"
	base := "/doc/" + name
	doc := func(id int64) string { return base + "/" + strconv.FormatInt(id, 10) }
	const page, sub = "/doc/{doctype}/{id}", "/doc/{doctype}/{id}/{action}"
	return []screens.Screen{
		{Name: name + " pending", Route: page, URL: doc(ids.Pending), As: sweepUser},
		{Name: name + " approved", Route: page, URL: doc(ids.Approved), As: sweepUser},
		{Name: name + " rejected", Route: page, URL: doc(ids.Rejected), As: sweepUser},
		{Name: name + " cancelled", Route: page, URL: doc(ids.Cancelled), As: sweepUser},
		{Name: name + " closed", Route: page, URL: doc(ids.Closed), As: sweepUser},
		{Name: name + " reject", Route: sub, URL: doc(ids.Pending) + "/reject", As: sweepUser},
		{Name: name + " cancel", Route: sub, URL: doc(ids.Approved) + "/cancel", As: sweepUser},
		{Name: name + " close", Route: sub, URL: doc(ids.Approved) + "/close", As: sweepUser},
		{Name: name + " delete", Route: sub, URL: doc(ids.Draft) + "/delete", As: sweepUser},
		{Name: name + " list", Route: route, URL: base, As: sweepUser},
		{Name: name + " list empty", Route: route, URL: base + "?q=no-such-thing-zz", As: sweepUser},
		{Name: name + " list filtered", Route: route, URL: base + "?" + ListQuery{
			Filters: []Filter{{Field: "state", Op: "=", Values: []string{string(doctype.StateApproved), string(doctype.StatePending)}}},
		}.Encode(), As: sweepUser},
	}
}
