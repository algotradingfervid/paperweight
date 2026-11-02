package partyweb_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/party"
)

// No case ID: UI v2 C5, A-20 (§5.5 item 9, CD-369). The party form shows the
// two role columns as one segmented choice, role: vendor, customer or both.
// Create and edit save the columns it stands for; an unknown or missing role
// sets neither, so the service refuses it with its own text, under the
// choice. A stored party pre-selects its role.

var roleCases = []struct {
	role             string
	vendor, customer bool
	label            string
}{
	{"vendor", true, false, "Vendor"},
	{"customer", false, true, "Customer"},
	{"both", true, true, "Both"},
}

// wantRoleChoice checks that r shows role as the three segmented options with
// want chosen.
func wantRoleChoice(t *testing.T, r webtest.Response, want string) {
	t.Helper()
	if n := r.Doc.Find(`form .segmented input[type="radio"][name="role"]`).Length(); n != 3 {
		t.Fatalf("%d segmented role options, want 3\ntext: %s", n, r.Text())
	}
	opts := []webtest.Choice{{Value: "vendor", Label: "Vendor"}, {Value: "customer", Label: "Customer"}, {Value: "both", Label: "Both"}}
	if got := r.Choices("role"); !slices.Equal(got, opts) {
		t.Errorf("role options = %v; want %v", got, opts)
	}
	if got := chosen(r, "role"); got != want {
		t.Errorf("chosen role = %q; want %q", got, want)
	}
	if !strings.Contains(r.Text(), "This party is a * (required)") {
		t.Errorf("the role's legend isn't marked required: %s", r.Text())
	}
	if r.Doc.Find(`input[name="role"][aria-required="true"]`).Length() != 3 {
		t.Error("not every role option is aria-required")
	}
	noControl(t, r, "is_vendor")
	noControl(t, r, "is_customer")
}

func wantRoles(t *testing.T, p party.Party, vendor, customer bool) {
	t.Helper()
	if p.IsVendor != vendor || p.IsCustomer != customer {
		t.Errorf("%s: is_vendor=%v is_customer=%v; want %v/%v", p.Code, p.IsVendor, p.IsCustomer, vendor, customer)
	}
}

// Each role saves its columns through the wizard, and Back shows it chosen.
func TestRoleC5_Create(t *testing.T) {
	for _, c := range roleCases {
		t.Run(c.role, func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			webtest.WantStatus(t, b.Get(t, stepDetails), http.StatusOK)
			wantRoleChoice(t, b.Get(t, stepDetails), "")
			toCheck(t, b, with(details("MEHTA"), "role", c.role), rows(mumbaiRow), rows())
			wantRoleChoice(t, b.Get(t, stepDetails), c.role)
			if !strings.Contains(b.Get(t, stepCheck).Text(), "This party is a"+c.label) {
				t.Errorf("Check doesn't show the role %s", c.label)
			}
			webtest.WantStatus(t, b.Post(t, stepCheck, nil), http.StatusSeeOther)
			wantRoles(t, w.get(t, "MEHTA"), c.vendor, c.customer)
		})
	}
}

// An unknown or missing role is refused under the choice with the service's
// text, on Next and at Check, and nothing is saved.
func TestRoleC5_CreateRefused(t *testing.T) {
	for _, posted := range []string{"seller", ""} {
		w := newWorld(t)
		b := w.kavya()
		r := b.Post(t, stepDetails, with(details("MEHTA"), "role", posted))
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantFieldError(t, r, "role", msgRole)
		wantRoleChoice(t, r, "")
	}
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, with(details("MEHTA"), "role", "both"), rows(mumbaiRow), rows())
	r := b.Post(t, stepDetails, with(details("MEHTA"), "role", "seller", "nav", "back"))
	webtest.WantRedirect(t, r, stepDetails)
	r = b.Post(t, stepCheck, nil)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "role", msgRole)
	if _, ok := w.byCode(t, "MEHTA"); ok {
		t.Error("a party with no role was saved")
	}
}

// The details section pre-selects each stored combination, and saving each
// role sets its columns; the summary names it.
func TestRoleC5_Edit(t *testing.T) {
	for _, stored := range roleCases {
		for _, c := range roleCases {
			t.Run(stored.role+" to "+c.role, func(t *testing.T) {
				w := newWorld(t)
				p := w.create(t, party.Input{Code: "ROLE", Name: "Role Traders", IsVendor: stored.vendor, IsCustomer: stored.customer})
				b := w.kavya()
				target := recordURL(p.ID) + "/edit/details"
				page := b.Get(t, target)
				webtest.WantStatus(t, page, http.StatusOK)
				wantRoleChoice(t, page, stored.role)
				webtest.WantStatus(t, b.Post(t, target, with(formValues(t, page, target), "role", c.role)), http.StatusSeeOther)
				wantRoles(t, w.get(t, "ROLE"), c.vendor, c.customer)
				if !strings.Contains(b.Get(t, recordURL(p.ID)).Text(), "This party is a"+c.label) {
					t.Errorf("the summary doesn't show the role %s", c.label)
				}
			})
		}
	}
}

// An unknown or missing role on edit is refused under the choice and changes
// nothing.
func TestRoleC5_EditRefused(t *testing.T) {
	for _, posted := range []string{"seller", ""} {
		w := newWorld(t)
		p := w.create(t, party.Input{Code: "ROLE", Name: "Role Traders", IsCustomer: true})
		b := w.kavya()
		target := recordURL(p.ID) + "/edit/details"
		r := b.Post(t, target, with(formValues(t, b.Get(t, target), target), "role", posted))
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantFieldError(t, r, "role", msgRole)
		wantRoleChoice(t, r, "")
		wantRoles(t, w.get(t, "ROLE"), false, true)
	}
}
