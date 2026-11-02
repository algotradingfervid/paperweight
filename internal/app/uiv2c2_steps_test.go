package app_test

import (
	"net/http"
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

// TestWizards_StepLabelLength (A-14): every registered document wizard
// (FormSteps with its Module's StepLabels), every master wizard and the
// company setup wizard (as rendered) has labels of 12 characters or fewer.
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
	for _, path := range []string{"/m/party/new", "/m/employee/new", "/m/import/choose", "/setup/company"} {
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
		for _, l := range long {
			t.Errorf("%s: step label %q is over %d characters (A-14)", name, l, wizard.MaxLabel)
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
