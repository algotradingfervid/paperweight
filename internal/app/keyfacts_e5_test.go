package app_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// UI v2 E5 (§5.13 item 3): every real module's KeyFacts names only header
// fields its DocType declares, read from the app's own Deps, so a typo in a
// module fails here as well as when the routes are built; and the PO's are
// the four §5.13 names, in order.
func TestApp_KeyFactsAreHeaderFields(t *testing.T) {
	mods := newWorld(t).app.DocDeps().Modules
	if err := docscreen.CheckKeyFacts(mods); err != nil {
		t.Error(err)
	}
	want := []string{"vendor_id", "site_id", "order_date", "required_by"}
	if got := mods[purchase.DocType].KeyFacts; !slices.Equal(got, want) {
		t.Errorf("%s KeyFacts = %q, want %q", purchase.DocType, got, want)
	}
}
