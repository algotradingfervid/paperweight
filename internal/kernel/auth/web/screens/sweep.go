package authscreens

import (
	"context"

	"github.com/vardhan/internaltools/internal/ui/screens"
)

// SweepIDs is what the role screens need from the sweep's fixtures. Role is
// any role but Administrator, which is Admin; pick one no sweep user depends
// on, because the read-notice save changes that role's rights. NotShown names
// a DocType some rights don't apply to, and Team one that offers the Team
// scope.
type SweepIDs struct {
	Role, Admin    int64
	NotShown, Team string
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
	copyNoSource = `document.querySelector("input[name=version]").form.requestSubmit()`
)

// Screens lists the role pages for the overflow sweep (design-system §10,
// CD-219): every variant of /roles, a role's page, its rights on a DocType
// and copy rights. Copy rights with no role to copy from isn't one: only
// deleting every other role shows it.
//
// This package is in the release build, which must not link chromedp
// (TestGallery_NotInRelease), so the sweep passes in submit: it runs js on the
// page of the chromedp context ctx and waits until wait is visible.
func Screens(ids SweepIDs, submit func(ctx context.Context, js, wait string) error) []screens.Screen {
	const role = "/roles/{id}"
	rights, copyRights := role+"/rights/{doctype}", role+"/copy-rights"
	copyPath := rolePath(ids.Role) + "/copy-rights"
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
	}
}
