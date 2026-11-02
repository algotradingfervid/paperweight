package docscreen_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4c: a module's StepIcons name the check cards' tiles, step by step;
// a step it leaves out keeps the default.
func TestCheckPage_StepIcons(t *testing.T) {
	for _, c := range []struct {
		name  string
		icons map[string]string
		want  []uitest.CheckTile
	}{
		{"no module icons keep the defaults", nil, []uitest.CheckTile{{Title: "Details", Icon: "file-text"}, {Title: "Items", Icon: "list-checks"}}},
		{"a fields step", map[string]string{"details": "truck"}, []uitest.CheckTile{{Title: "Details", Icon: "truck"}, {Title: "Items", Icon: "list-checks"}}},
		{"the lines step", map[string]string{"items": "package-check"}, []uitest.CheckTile{{Title: "Details", Icon: "file-text"}, {Title: "Items", Icon: "package-check"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {StepIcons: c.icons}}
			e.remount()
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), sampleRecord())
			uitest.WantCheckTiles(t, uitest.Doc(t, e.get(t, doc42+"/edit/check")), c.want...)
		})
	}
}

// A StepIcons entry that can't show panics when the routes are built, never
// falling back to the default.
func TestRoutes_StepIconsChecked(t *testing.T) {
	sample := func(slug, icon string) map[string]docscreen.Module {
		return map[string]docscreen.Module{docscreentest.Sample: {StepIcons: map[string]string{slug: icon}}}
	}
	for _, c := range []struct {
		name string
		mods map[string]docscreen.Module
		want string
	}{
		{"unknown icon", sample("details", "no-such-icon"), `names icon "no-such-icon"`},
		{"blank icon", sample("details", ""), `names icon ""`},
		{"unknown step", sample("nosuch", "truck"), `step "nosuch"`},
		{"the check step has no card", sample("check", "truck"), `step "check"`},
		{"unregistered DocType", map[string]docscreen.Module{"c4c.Unknown": {StepIcons: map[string]string{"details": "truck"}}}, "c4c.Unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if got, ok := recover().(error); !ok || !strings.Contains(got.Error(), c.want) {
					t.Errorf("panic = %v, want an error holding %q", got, c.want)
				}
			}()
			docscreen.Routes(docscreen.Deps{Modules: c.mods})
		})
	}
	docscreen.Routes(docscreen.Deps{Modules: sample("details", "truck")})
}
