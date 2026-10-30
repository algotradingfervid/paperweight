package authscreens

import (
	"context"
	"net/url"
	"strconv"

	"github.com/vardhan/internaltools/internal/ui/screens"
)

// SweepIDs is what the screens need from the sweep's fixtures. Role is any role
// but Administrator, which is Admin; pick one no sweep user depends on, because
// the read-notice save changes that role's rights. NotShown names a DocType
// some rights don't apply to, and Team one that offers the Team scope. User is
// the user whose pages are shown.
type SweepIDs struct {
	Role, Admin, User int64
	NotShown, Team    string
}

// sweepUser is the fixture admin the sweep signs in as.
const sweepUser = "admin@vardhan.test"

// Scripts the Setups run. saveWithoutRead clears read itself: the sweep runs it
// at every width, and the save before added read back.
const (
	saveWithoutRead = `(() => {
  const f = document.querySelector("input[name=version]").form;
  f.querySelector("input[name=read][value='']").checked = true;
  f.querySelector("input[name=create][value=own]").checked = true;
  f.requestSubmit();
})()`
	saveStale = `(() => {
  const v = document.querySelector("input[name=version]");
  v.value = String(Number(v.value) - 1);
  v.form.requestSubmit();
})()`
	copyNoSource  = `document.querySelector("input[name=version]").form.requestSubmit()`
	refusedSignIn = `(() => {
  const f = document.querySelector("input[name=password]").form;
  f.elements.email.value = "nobody" + Date.now() + "@vardhan.test"; // six widths must not lock it out
  f.elements.password.value = "wrong";
  f.requestSubmit();
})()`
)

// Screens lists AUTH's pages for the overflow sweep (design-system §10,
// CD-219): every variant of /roles, a role's page, its rights on a DocType and
// copy rights, then sign-in, the account pages, users and who-can. Copy rights
// with no role to copy from isn't one: only deleting every other role shows it.
// First-run /setup isn't either: only an empty install shows it.
//
// This package is in the release build, which must not link chromedp
// (TestGallery_NotInRelease), so the sweep passes in submit: it runs js on the
// page of the chromedp context ctx and waits until wait is visible.
func Screens(ids SweepIDs, submit func(ctx context.Context, js, wait string) error) []screens.Screen {
	const role = "/roles/{id}"
	rights, copyRights := role+"/rights/{doctype}", role+"/copy-rights"
	copyPath := rolePath(ids.Role) + "/copy-rights"
	const user = "/users/{id}"
	userPath := "/users/" + strconv.FormatInt(ids.User, 10)
	setup := func(js, wait string) func(context.Context) error {
		return func(ctx context.Context) error { return submit(ctx, js, wait) }
	}
	return []screens.Screen{
		{Name: "roles", Route: "/roles", URL: "/roles", As: sweepUser},
		{Name: "role", Route: role, URL: rolePath(ids.Role), As: sweepUser},
		{Name: "role administrator", Route: role, URL: rolePath(ids.Admin), As: sweepUser},
		{Name: "rights not shown", Route: rights, URL: RightsPath(ids.Role, ids.NotShown), As: sweepUser},
		{Name: "rights team", Route: rights, URL: RightsPath(ids.Role, ids.Team), As: sweepUser},
		{Name: "rights read notice", Route: rights, URL: RightsPath(ids.Role, ids.Team), As: sweepUser, Setup: setup(saveWithoutRead, "main .banner--info")},
		{Name: "rights 409", Route: rights, URL: RightsPath(ids.Role, ids.Team), As: sweepUser, Setup: setup(saveStale, "main .banner--danger")},
		{Name: "copy rights", Route: copyRights, URL: copyPath, As: sweepUser},
		{Name: "copy rights 422", Route: copyRights, URL: copyPath, As: sweepUser, Setup: setup(copyNoSource, ".error-summary")},
		{Name: "sign in", Route: "/signin", URL: "/signin"},
		{Name: "sign in refused", Route: "/signin", URL: "/signin", Setup: setup(refusedSignIn, ".error-summary")},
		{Name: "password", Route: "/account/password", URL: "/account/password", As: sweepUser},
		{Name: "sessions", Route: "/account/sessions", URL: "/account/sessions", As: sweepUser},
		{Name: "users", Route: "/users", URL: "/users", As: sweepUser},
		{Name: "user", Route: user, URL: userPath, As: sweepUser},
		{Name: "user access", Route: user + "/access", URL: userPath + "/access", As: sweepUser},
		{Name: "who can", Route: "/rights/who-can", URL: "/rights/who-can", As: sweepUser},
		{Name: "who can results", Route: "/rights/who-can", URL: "/rights/who-can?" + url.Values{"doctype": {ids.Team}, "right": {"read"}}.Encode(), As: sweepUser},
	}
}
