package siteweb_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/site"
)

// M0-58 (CD-337 (1)): the site's delivery address and contact, shown on its
// record page and edited at /m/site/{id}/delivery.
const (
	msgDeliveryInState = "Delivery address state must match the branch state."
	noDelivery         = "No delivery address yet."
	staleWH            = "Someone else changed Site PUNE-WH after you opened it. Reload it and make your changes again."
)

// deliveryForm is a delivery in Pune posted at version.
func deliveryForm(version int) url.Values {
	return url.Values{
		"version":        {ver(version)},
		"delivery_line1": {"Gate 2, Hinjewadi"}, "delivery_line2": {"Phase 1"}, "delivery_city": {"Pune"},
		"delivery_state_code": {"27"}, "delivery_pincode": {"411057"},
		"contact_name": {"Raj Patil"}, "contact_mobile": {"9876543210"},
	}
}

var deliveryFields = []string{"delivery_line1", "delivery_line2", "delivery_city", "delivery_state_code", "delivery_pincode", "contact_name", "contact_mobile"}

func TestScreens_SiteDeliveryEdit(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	wh, _ := w.seed(t)

	page := w.h.Get(t, kavya, path(wh.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "Delivery address")
	webtest.WantText(t, page, noDelivery)
	if page.Doc.Find(`a[href="`+path(wh.ID, "/delivery")+`"]`).Length() == 0 {
		t.Error("record page has no link to the delivery address form for Kavya (write right)")
	}

	blank := w.h.Get(t, kavya, path(wh.ID, "/delivery"))
	webtest.WantStatus(t, blank, http.StatusOK)
	for _, name := range deliveryFields {
		if !blank.HasControl(name) {
			t.Errorf("delivery form has no control named %q", name)
		}
	}
	if v, _ := blank.Value("version"); v != ver(wh.Version) {
		t.Errorf("delivery form version = %q; want the site's %d", v, wh.Version)
	}
	if blank.Doc.Find(`select[name="delivery_state_code"] option[value="27"]`).Length() != 1 {
		t.Error("delivery state is not a select of GST states")
	}

	// Another branch's state: 422 by the state field, typed values kept.
	bad := deliveryForm(wh.Version)
	bad.Set("delivery_state_code", "29")
	bad.Set("delivery_pincode", "560001")
	resp := w.h.Post(t, kavya, path(wh.ID, "/delivery"), bad)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "delivery_state_code", msgDeliveryInState)
	if v, _ := resp.Value("delivery_city"); v != "Pune" {
		t.Errorf("422 page city = %q; want the typed Pune", v)
	}

	resp = w.h.Post(t, kavya, path(wh.ID, "/delivery"), deliveryForm(wh.Version))
	webtest.WantRedirect(t, resp, path(wh.ID, ""))
	saved := w.mustGet(t, "PUNE-WH")
	want := site.Delivery{Line1: "Gate 2, Hinjewadi", Line2: "Phase 1", City: "Pune", StateCode: "27", Pincode: "411057", ContactName: "Raj Patil", ContactMobile: "+919876543210"}
	if saved.Delivery != want || saved.Name != wh.Name || saved.Version != wh.Version+1 {
		t.Fatalf("after the delivery save = %+v; want %+v, name kept, version %d", saved, want, wh.Version+1)
	}
	page = w.h.Get(t, kavya, path(wh.ID, ""))
	for _, text := range []string{"Gate 2, Hinjewadi", "Pune - 411057", "Maharashtra (27)", "Raj Patil", "+919876543210"} {
		webtest.WantText(t, page, text)
	}
	webtest.WantNoText(t, page, noDelivery)
	edit := w.h.Get(t, kavya, path(wh.ID, "/delivery"))
	if v, _ := edit.Value("contact_mobile"); v != "+919876543210" {
		t.Errorf("delivery form mobile = %q; want the stored +919876543210", v)
	}

	// A stale page: 409, nothing written.
	resp = w.h.Post(t, kavya, path(wh.ID, "/delivery"), deliveryForm(wh.Version))
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleWH)

	// The site's own form keeps the address.
	main := url.Values{"version": {ver(saved.Version)}, "code": {"PUNE-WH"}, "name": {"Pune Stores"}, "type": {"warehouse"}, "branch_id": {dec(w.pune)}, "project_id": {""}}
	webtest.WantRedirect(t, w.h.Post(t, kavya, path(wh.ID, ""), main), path(wh.ID, ""))
	renamed := w.mustGet(t, "PUNE-WH")
	if renamed.Delivery != want || renamed.Name != "Pune Stores" {
		t.Fatalf("after the site's own save = %+v; want the delivery kept", renamed)
	}

	// Moving the site to BLR (Karnataka) with a Maharashtra address: refused on
	// the site's form, the message in the banner.
	main.Set("version", ver(renamed.Version))
	main.Set("branch_id", dec(w.blr))
	resp = w.h.Post(t, kavya, path(wh.ID, ""), main)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantText(t, resp, msgDeliveryInState)

	// Every field blank removes it.
	empty := url.Values{"version": {ver(renamed.Version)}}
	for _, name := range deliveryFields {
		empty.Set(name, "")
	}
	webtest.WantRedirect(t, w.h.Post(t, kavya, path(wh.ID, "/delivery"), empty), path(wh.ID, ""))
	if got := w.mustGet(t, "PUNE-WH").Delivery; got != (site.Delivery{}) {
		t.Errorf("after the blank save = %+v; want none", got)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(wh.ID, "")), noDelivery)

	// A save without a version is a bad form.
	webtest.WantStatus(t, w.h.Post(t, kavya, path(wh.ID, "/delivery"), url.Values{"delivery_city": {"Pune"}}), http.StatusBadRequest)
}

// A reader sees the address but no link to change it, and the form is refused.
func TestScreens_SiteDeliveryReadOnly(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	wh, _ := w.seed(t)
	if _, err := w.svc.Update(context.Background(), w.h.Env.Kavya, wh.ID, wh.Version, site.Input{
		Code: wh.Code, Name: wh.Name, BranchID: wh.BranchID, Type: wh.Type,
		Delivery: &site.Delivery{Line1: "Gate 2", City: "Pune", StateCode: "27"},
	}); err != nil {
		t.Fatal(err)
	}
	page := w.h.Get(t, priya, path(wh.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "Gate 2")
	if page.Doc.Find(`a[href="`+path(wh.ID, "/delivery")+`"]`).Length() != 0 {
		t.Error("record page links to the delivery form for Priya (read only)")
	}
	deny := (&auth.DeniedError{Right: auth.RightWrite, Label: "Site"}).Error()
	resp := w.h.Get(t, priya, path(wh.ID, "/delivery"))
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny)
	resp = w.h.Post(t, priya, path(wh.ID, "/delivery"), deliveryForm(wh.Version+1))
	webtest.WantStatus(t, resp, http.StatusForbidden)
	if got := w.mustGet(t, "PUNE-WH").Delivery.ContactName; got != "" {
		t.Errorf("Priya's refused save stored contact %q", got)
	}
}

// A user restricted to one site reads and edits its address, and reaches
// nothing of another site's (restricted users are scoped by site).
func TestScreens_SiteDeliveryRestrictedUser(t *testing.T) {
	w := newWorld(t)
	own, other := w.seed(t)
	ctx := context.Background()
	if _, err := w.svc.Update(ctx, w.h.Env.Kavya, other.ID, other.Version, site.Input{
		Code: other.Code, Name: other.Name, BranchID: other.BranchID, ProjectID: other.ProjectID, Type: other.Type,
		Delivery: &site.Delivery{Line1: "Secret Yard", City: "Pune", StateCode: "27", ContactName: "Hidden Person"},
	}); err != nil {
		t.Fatal(err)
	}
	w.restrictPriya(t, own.ID, auth.RightRead, auth.RightWrite)
	priya := w.h.Env.Priya

	form := w.h.Get(t, priya, path(own.ID, "/delivery"))
	webtest.WantStatus(t, form, http.StatusOK)
	webtest.WantRedirect(t, w.h.Post(t, priya, path(own.ID, "/delivery"), deliveryForm(own.Version)), path(own.ID, ""))
	webtest.WantText(t, w.h.Get(t, priya, path(own.ID, "")), "Raj Patil")

	for _, resp := range []webtest.Response{
		w.h.Get(t, priya, path(other.ID, "")),
		w.h.Get(t, priya, path(other.ID, "/delivery")),
		w.h.Post(t, priya, path(other.ID, "/delivery"), deliveryForm(other.Version+1)),
	} {
		webtest.WantStatus(t, resp, http.StatusNotFound)
		webtest.WantNoText(t, resp, "Secret Yard")
		webtest.WantNoText(t, resp, "Hidden Person")
	}
	if got := w.mustGet(t, "METRO-SITE").Delivery.ContactName; got != "Hidden Person" {
		t.Errorf("METRO-SITE contact after Priya's refused save = %q; want Hidden Person", got)
	}
}
