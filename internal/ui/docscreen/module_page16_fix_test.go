package docscreen_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// M0-16 (CD-263 G5): the module's Primary is forced to the primary variant,
// whatever Variant the module set.
func TestModule_PagePrimaryForcedVariant(t *testing.T) {
	e := m16NewOrderEnv(t)
	var calls m16Calls
	var fail error
	for _, v := range []components.Variant{"", components.Secondary, components.Ghost} {
		primary := &components.ButtonProps{Label: m16SendLabel, Href: m16SendHref, Variant: v}
		m16MountOn(e.scopeEnv, fixture.Order, m16PrimaryModule(&calls, &primary, &fail))
		e.who = e.priya
		what := "Priya: approved order, Variant " + string(v)
		m16WantModulePrimary(t, m16Status(t, e.get(t, m15DocURL(fixture.Order, e.approved)), http.StatusOK, what), nil, what)
	}
}
