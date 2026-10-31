package docscreen_test

import (
	"errors"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// errOtherModule is what every hook of otherModule returns: it must never
// run for a page of another DocType.
var errOtherModule = errors.New("docscreen_test: another DocType's Module ran")

// otherModule sets every Module field, each to something that changes or
// breaks a page: a label, a failing step Body and Save on every slug the
// golden pages use, a failing line page, a card, both flags, a Hide that
// hides everything and failing extras. Keyed by another DocType, it must
// change nothing on the sample's or the Shipment's pages.
func otherModule() docscreen.Module {
	failStep := docscreen.Step{
		Body: func(docscreen.Ctx, docscreen.StepState) (templ.Component, error) { return nil, errOtherModule },
		Save: func(docscreen.Ctx) (docscreen.Save, error) { return docscreen.Save{}, errOtherModule },
	}
	return docscreen.Module{
		StepLabels: map[string]string{"details": "Other details", "delivery": "Other delivery", "items": "Other items", "check": "Other check"},
		Steps:      map[string]docscreen.Step{"details": failStep, "delivery": failStep, "items": failStep, "check": failStep},
		Line: &docscreen.Line{
			Body: func(docscreen.Ctx, int, docscreen.StepState) (templ.Component, error) { return nil, errOtherModule },
			Save: func(docscreen.Ctx, int) (docscreen.Save, error) { return docscreen.Save{}, errOtherModule },
		},
		LineCard: func(docscreen.Ctx, docscreen.LineView) components.LineCard {
			return components.LineCard{Item: "Other card"}
		},
		CardsOnly:   true,
		NoLinePages: true,
		Hide:        func(docscreen.Ctx, doctype.Field) bool { return true },
		Check:       func(docscreen.Ctx) (docscreen.CheckExtra, error) { return docscreen.CheckExtra{}, errOtherModule },
		Page:        func(docscreen.Ctx) (docscreen.PageExtra, error) { return docscreen.PageExtra{}, errOtherModule },
		Reason: func(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
			return docscreen.ReasonExtra{}, errOtherModule
		},
	}
}

// HOOK-001: a DocType with no Module renders byte-identically to today.
// Every page testdata/module_none/ captured on base e55f56e (see
// module_golden_test.go) is rendered again with Deps.Modules nil, with an
// entry only for other DocTypes, and with an empty Module (nil and empty
// maps) for the page's own DocType, and must match the capture byte for byte
// after the asset hashes are normalised.
func TestModule_NoneUnchanged(t *testing.T) {
	variants := []struct {
		name    string
		modules map[string]docscreen.Module
	}{
		{name: "nil", modules: nil},
		{name: "empty map", modules: map[string]docscreen.Module{}},
		{name: "other doctype", modules: map[string]docscreen.Module{claimType: otherModule(), "docscreentest.NoSuchType": otherModule()}},
		{name: "empty module", modules: map[string]docscreen.Module{
			docscreentest.Sample: {}, shipType: {}, claimType: otherModule(),
		}},
		{name: "empty maps", modules: map[string]docscreen.Module{
			docscreentest.Sample: {StepLabels: map[string]string{}, Steps: map[string]docscreen.Step{}},
			shipType:             {StepLabels: map[string]string{}, Steps: map[string]docscreen.Step{}},
		}},
	}
	pages := goldenPages()
	if len(pages) < 31 {
		t.Fatalf("goldenPages lists %d pages, want the 31 captured", len(pages))
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			for _, p := range pages {
				t.Run(p.name, func(t *testing.T) {
					got := renderGolden(t, p, func(d *docscreen.Deps) { d.Modules = v.modules })
					wantGolden(t, p.name, got)
				})
			}
		})
	}
}
