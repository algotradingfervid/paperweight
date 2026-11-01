package app_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// uiv2c2KnownLong are today's step labels over wizard.MaxLabel, by wizard.
// UI v2 C2b shortens them and empties this list; until then the test only
// warns about them, and fails on any other long label or on an entry that is
// no longer long (A-14).
var uiv2c2KnownLong = map[string][]string{
	purchase.DocType:        {"Check and submit"},
	purchase.RequestDocType: {"Check and submit"},
	goodsreceipt.DocType:    {"Choose purchase order", "Delivery details", "Received quantities", "Check and submit"},
	expense.ClaimDocType:    {"Claim details", "Check and submit"},
	expense.AdvanceDocType:  {"Check and submit"},
	"/m/party/new":          {"Party details", "Check and save"},
	"/m/employee/new":       {"Check and save"},
	"/m/import/choose":      {"Choose master", "Check results"},
}

// TestWizards_StepLabelLength (A-14, warn-only in C2a): every registered
// document wizard (FormSteps with its Module's StepLabels) and every master
// wizard (as rendered) has labels of 12 characters or fewer, apart from the
// known exceptions above.
func TestWizards_StepLabelLength(t *testing.T) {
	w := newWorld(t)
	deps := w.app.DocDeps()
	found := map[string][]string{}
	for _, dt := range doctype.All() {
		if dt.Master != nil || dt.TestOnly {
			continue
		}
		steps, err := docscreen.FormSteps(dt)
		if err != nil {
			t.Fatalf("%s: %v", dt.Name, err)
		}
		m := wizard.Model{}
		for _, s := range steps {
			label := s.Label
			if l, ok := deps.Modules[dt.Name].StepLabels[s.Slug]; ok {
				label = l
			}
			m.Steps = append(m.Steps, wizard.Step{Slug: s.Slug, Label: label})
		}
		found[dt.Name] = m.LongLabels()
	}

	w.seedUsers()
	b := w.signedIn(adminEmail)
	for _, path := range []string{"/m/party/new", "/m/employee/new", "/m/import/choose"} {
		r := b.get(path)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d", path, r.status)
		}
		m := wizard.Model{}
		r.doc.Find("nav.stepper .stepper__label").Each(func(_ int, s *goquery.Selection) {
			c := s.Clone()
			c.Find(".visually-hidden").Remove()
			m.Steps = append(m.Steps, wizard.Step{Label: strings.TrimSpace(c.Text())})
		})
		if len(m.Steps) < 3 {
			t.Fatalf("GET %s: %d stepper labels, want the wizard's steps", path, len(m.Steps))
		}
		found[path] = m.LongLabels()
	}

	for name, long := range found {
		known := uiv2c2KnownLong[name]
		for _, l := range long {
			if slices.Contains(known, l) {
				t.Logf("A-14 (C2b): %s step label %q is over %d characters", name, l, wizard.MaxLabel)
			} else {
				t.Errorf("%s: step label %q is over %d characters (A-14)", name, l, wizard.MaxLabel)
			}
		}
		for _, l := range known {
			if !slices.Contains(long, l) {
				t.Errorf("%s: known long label %q is gone: remove it from uiv2c2KnownLong", name, l)
			}
		}
	}
	for name := range uiv2c2KnownLong {
		if _, ok := found[name]; !ok {
			t.Errorf("uiv2c2KnownLong names %s, which is not a checked wizard", name)
		}
	}
}

// TestModules_StepIntros (UI v2 §5.6): the PO, GRN and expense claim wizards,
// as the app wires them, have an intro on every step, keyed by their step
// slugs.
func TestModules_StepIntros(t *testing.T) {
	deps := newWorld(t).app.DocDeps()
	for _, name := range []string{purchase.DocType, goodsreceipt.DocType, expense.ClaimDocType} {
		dt, ok := doctype.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		steps, err := docscreen.FormSteps(dt)
		if err != nil {
			t.Fatal(err)
		}
		intros := deps.Modules[name].StepIntros
		if len(intros) != len(steps) {
			t.Errorf("%s: %d intros for %d steps", name, len(intros), len(steps))
		}
		for _, s := range steps {
			if strings.TrimSpace(intros[s.Slug]) == "" {
				t.Errorf("%s: step %q has no intro", name, s.Slug)
			}
		}
	}
}
