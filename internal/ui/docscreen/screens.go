package docscreen

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// SampleIDs are the seeded documents the sweep visits, one per state plus a
// draft with 500 lines. Screens is test-time only, so a missing id panics:
// every id is needed for a state the DocType has (pending and rejected only
// when it is approvable, the states after draft only when it is
// submittable, closed only when it can be closed). CloseAs is a user holding
// the DocType's close right; blank is the sweep user, who must then need only
// submit (CD-247 (3)).
type SampleIDs struct {
	Draft, Pending, Approved, Rejected, Cancelled, Closed, Lines500 int64
	CloseAs                                                         string
}

// sweepUser is the fixture user the sweep signs in as.
const sweepUser = "priya@vardhan.test"

// Screens lists the docscreen pages of the DocType name for the overflow sweep:
// the wizard (step 1 of a new document, every step of ids.Draft, a 422 and a
// 409 variant of the first step), the line pages, the document page in every
// other state the DocType has and the reason, confirm and list pages, and the
// search page. The close page is left out for a DocType that can't be closed
// (CD-277), and the filtered list page for a DocType list can't filter by
// state (M0-38). An unregistered name is taken to have every state. The line
// pages are left out when d, the app's Deps, gives name a NoLinePages Module,
// as those routes answer 404 for it (CD-329).
func Screens(name string, ids SampleIDs, d ...Deps) []screens.Screen {
	const route = "/doc/{doctype}"
	base := "/doc/" + name
	doc := func(id int64) string { return base + "/" + strconv.FormatInt(id, 10) }
	const page, sub = "/doc/{doctype}/{id}", "/doc/{doctype}/{id}/{action}"
	const step, lines = "/doc/{doctype}/{id}/edit/{step}", "/doc/{doctype}/{id}/lines/{no}"
	slugs := []string{slugDetails, slugItems, slugCheck}
	approvable, submittable, filterable := true, true, true
	if dt, ok := doctype.Lookup(name); ok {
		approvable, submittable = dt.Approvable, dt.Submittable
		filterable = list.CanFilter(dt, "state", list.OpIn)
		if steps, err := FormSteps(dt); err == nil {
			slugs = slugs[:0]
			for _, s := range steps {
				slugs = append(slugs, s.Slug)
			}
		}
	}
	closeAs := ids.CloseAs
	right, closable := closeRight(name)
	closable = closable && submittable
	if closable && closeAs == "" {
		if right != auth.RightSubmit {
			panic("docscreen: Screens(" + name + ") needs SampleIDs.CloseAs, a user holding " + string(right))
		}
		closeAs = sweepUser
	}
	need := func(id int64, field string) int64 {
		if id == 0 {
			panic("docscreen: Screens(" + name + ") needs SampleIDs." + field)
		}
		return id
	}
	draft := doc(need(ids.Draft, "Draft"))
	out := []screens.Screen{
		{Name: name + " new", Route: "/doc/{doctype}/new", URL: base + "/new", As: sweepUser},
		{Name: name + " " + slugs[0] + " 422", Route: step, URL: draft + "/edit/" + slugs[0], As: sweepUser, Setup: submitInvalid},
		{Name: name + " " + slugs[0] + " 409", Route: step, URL: draft + "/edit/" + slugs[0], As: sweepUser, Setup: submitStale},
		{Name: name + " items 500", Route: step, URL: doc(need(ids.Lines500, "Lines500")) + "/edit/" + slugItems + "?all=1", As: sweepUser},
	}
	if len(d) == 0 || !d[0].Modules[name].NoLinePages {
		out = append(out, []screens.Screen{
			{Name: name + " line new", Route: "/doc/{doctype}/{id}/lines/new", URL: draft + "/lines/new", As: sweepUser},
			{Name: name + " line", Route: lines, URL: draft + "/lines/1", As: sweepUser},
			{Name: name + " line remove", Route: lines + "/remove", URL: draft + "/lines/1/remove", As: sweepUser},
		}...)
	}
	for _, slug := range slugs {
		out = append(out, screens.Screen{Name: name + " step " + slug, Route: step, URL: draft + "/edit/" + slug, As: sweepUser})
	}
	for _, s := range []struct {
		has         bool
		id          int64
		field, name string
		path, as    string
	}{
		{approvable, ids.Pending, "Pending", "pending", "", sweepUser},
		{submittable, ids.Approved, "Approved", "approved", "", sweepUser},
		{approvable, ids.Rejected, "Rejected", "rejected", "", sweepUser},
		{submittable, ids.Cancelled, "Cancelled", "cancelled", "", sweepUser},
		{closable, ids.Closed, "Closed", "closed", "", sweepUser},
		{approvable, ids.Pending, "Pending", "reject", "/reject", sweepUser},
		{submittable, ids.Approved, "Approved", "cancel", "/cancel", sweepUser},
		{closable, ids.Approved, "Approved", "close", "/close", closeAs},
		{true, ids.Draft, "Draft", "delete", "/delete", sweepUser},
	} {
		if !s.has {
			continue
		}
		r := page
		if s.path != "" {
			r = sub
		}
		out = append(out, screens.Screen{Name: name + " " + s.name, Route: r, URL: doc(need(s.id, s.field)) + s.path, As: s.as})
	}
	out = append(out,
		screens.Screen{Name: name + " list", Route: route, URL: base, As: sweepUser},
		screens.Screen{Name: name + " list empty", Route: route, URL: base + "?q=no-such-thing-zz", As: sweepUser},
	)
	if filterable {
		out = append(out, screens.Screen{Name: name + " list filtered", Route: route, URL: base + "?" + ListQuery{
			Filters: []Filter{{Field: "state", Op: string(list.OpIn), Values: []string{string(doctype.StateApproved), string(doctype.StatePending)}}},
		}.Encode(), As: sweepUser})
	}
	return append(out, []screens.Screen{
		{Name: name + " search", Route: "/search", URL: "/search", As: sweepUser},
		{Name: name + " search results", Route: "/search", URL: "/search?q=Draft", As: sweepUser},
		{Name: name + " search no match", Route: "/search", URL: "/search?q=no-such-thing-zz", As: sweepUser},
	}...)
}

// Unswept returns, sorted, the registered document DocTypes that list misses
// one of their Screens routes for: a module that doesn't add its own Screens
// to the sweep is named here (hooks spec §6). d is passed on to Screens.
func Unswept(list []screens.Screen, d ...Deps) []string {
	var out []string
	for _, dt := range doctype.All() {
		if !dt.IsDocument() {
			continue
		}
		base := "/doc/" + dt.Name
		mine := func(url string) bool {
			return url == base || strings.HasPrefix(url, base+"/") || strings.HasPrefix(url, base+"?")
		}
		all := SampleIDs{Draft: 1, Pending: 2, Approved: 3, Rejected: 4, Cancelled: 5, Closed: 6, Lines500: 7, CloseAs: sweepUser}
		for _, w := range Screens(dt.Name, all, d...) {
			if mine(w.URL) && !slices.ContainsFunc(list, func(s screens.Screen) bool { return s.Route == w.Route && mine(s.URL) }) {
				out = append(out, dt.Name)
				break
			}
		}
	}
	return out
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
