package docscreen

import (
	"context"
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

// Screens lists the docscreen pages of the DocType name for the overflow sweep:
// the wizard (step 1 of a new document, every step of ids.Draft, a 422 and a
// 409 variant), the line pages, the document page in every other state and
// the reason, confirm and list pages, and the search page.
func Screens(name string, ids SampleIDs) []screens.Screen {
	const route = "/doc/{doctype}"
	base := "/doc/" + name
	doc := func(id int64) string { return base + "/" + strconv.FormatInt(id, 10) }
	const page, sub = "/doc/{doctype}/{id}", "/doc/{doctype}/{id}/{action}"
	const step, lines = "/doc/{doctype}/{id}/edit/{step}", "/doc/{doctype}/{id}/lines/{no}"
	draft := doc(ids.Draft)
	out := []screens.Screen{
		{Name: name + " new", Route: "/doc/{doctype}/new", URL: base + "/new", As: sweepUser},
		{Name: name + " details 422", Route: step, URL: draft + "/edit/" + slugDetails, As: sweepUser, Setup: submitInvalid},
		{Name: name + " details 409", Route: step, URL: draft + "/edit/" + slugDetails, As: sweepUser, Setup: submitStale},
		{Name: name + " items 500", Route: step, URL: doc(ids.Lines500) + "/edit/" + slugItems + "?all=1", As: sweepUser},
		{Name: name + " line new", Route: "/doc/{doctype}/{id}/lines/new", URL: draft + "/lines/new", As: sweepUser},
		{Name: name + " line", Route: lines, URL: draft + "/lines/1", As: sweepUser},
		{Name: name + " line remove", Route: lines + "/remove", URL: draft + "/lines/1/remove", As: sweepUser},
	}
	slugs := []string{slugDetails, slugItems, slugCheck}
	if dt, ok := doctype.Lookup(name); ok {
		if steps, err := FormSteps(dt); err == nil {
			slugs = slugs[:0]
			for _, s := range steps {
				slugs = append(slugs, s.Slug)
			}
		}
	}
	for _, slug := range slugs {
		out = append(out, screens.Screen{Name: name + " step " + slug, Route: step, URL: draft + "/edit/" + slug, As: sweepUser})
	}
	return append(out, []screens.Screen{
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
		{Name: name + " search", Route: "/search", URL: "/search", As: sweepUser},
		{Name: name + " search results", Route: "/search", URL: "/search?q=Draft", As: sweepUser},
		{Name: name + " search no match", Route: "/search", URL: "/search?q=no-such-thing-zz", As: sweepUser},
	}...)
}

// submitInvalid types a value no parser accepts into step 1's number and
// date inputs and saves it, so the step shows a 422: UI's own parse error,
// or DOC's type error for an Int field (CD-178: a draft save no longer
// refuses empty Required fields).
func submitInvalid(ctx context.Context) error {
	return browserSubmit(ctx, `(() => {
  const f = document.getElementById("wizard-form");
  f.querySelectorAll("input[inputmode=numeric], input[inputmode=decimal]").forEach(function (e) { e.value = "x"; });
  f.requestSubmit();
})()`, ".error-summary")
}

// submitStale saves the step with an old version, so it shows DOC's 409.
func submitStale(ctx context.Context) error {
	return browserSubmit(ctx, `(() => {
  const v = document.getElementById("doc-version");
  v.value = String(Number(v.value) - 1);
  document.getElementById("wizard-form").requestSubmit();
})()`, "#wizard-conflict")
}
