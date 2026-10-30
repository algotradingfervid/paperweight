package partyweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/ui/components"
)

func input(p party.Party) party.Input {
	return party.Input{
		Code: p.Code, Name: p.Name, IsVendor: p.IsVendor, IsCustomer: p.IsCustomer, PAN: p.PAN,
		GSTCategory: p.GSTCategory, DefaultPaymentTermsID: p.DefaultPaymentTermsID,
	}
}

func (w *world) create(t *testing.T, in party.Input) party.Party {
	t.Helper()
	if _, err := w.parties.Create(context.Background(), w.h.Env.Kavya, in); err != nil {
		t.Fatalf("create party %s: %v", in.Code, err)
	}
	return w.get(t, in.Code)
}

func (w *world) disable(t *testing.T, code string) {
	t.Helper()
	p := w.get(t, code)
	if err := w.parties.SetDisabled(context.Background(), w.h.Env.Kavya, p.ID, p.Version, true); err != nil {
		t.Fatalf("disable %s: %v", code, err)
	}
}

// formValues is what the form posting to action posts as it stands, as a
// browser would send it. Tests post it back so they don't name the controls
// they don't change.
func formValues(t *testing.T, r webtest.Response, action string) url.Values {
	t.Helper()
	form := r.Doc.Find(`form[action="` + action + `"]`).First()
	if form.Length() == 0 {
		t.Fatalf("no form posts to %s\nbody: %s", action, webtest.Squash(r.Body))
	}
	v := url.Values{}
	form.Find("input, select, textarea").Each(func(_ int, s *goquery.Selection) {
		name, ok := s.Attr("name")
		if !ok {
			return
		}
		kind, _ := s.Attr("type")
		switch {
		case kind == "checkbox" || kind == "radio":
			if _, on := s.Attr("checked"); !on {
				return
			}
			got, ok := s.Attr("value")
			if !ok {
				got = "on"
			}
			v.Add(name, got)
		case s.Is("select"):
			got, _ := s.Find("option[selected]").First().Attr("value")
			v.Add(name, got)
		case s.Is("textarea"):
			v.Add(name, s.Text())
		default:
			got, _ := s.Attr("value")
			v.Add(name, got)
		}
	})
	return v
}

// sections maps each control name to the section form that shows it, found from
// the record page's Change links: the slugs are the screen's own.
func sections(t *testing.T, b *webtest.Browser, id int64) map[string]string {
	t.Helper()
	page := b.Get(t, recordURL(id))
	webtest.WantStatus(t, page, http.StatusOK)
	prefix := recordURL(id) + "/edit/"
	out := map[string]string{}
	page.Doc.Find(`a[href^="` + prefix + `"]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		section := b.Get(t, href)
		webtest.WantStatus(t, section, http.StatusOK)
		section.Doc.Find(`form[action="` + href + `"] [name]`).Each(func(_ int, c *goquery.Selection) {
			name, _ := c.Attr("name")
			if name != "version" {
				out[name] = href
			}
		})
	})
	if len(out) == 0 {
		t.Fatalf("the record page has no section to change\ntext: %s", page.Text())
	}
	return out
}

// sectionForm is the section holding field, as rendered, with kv set.
func sectionForm(t *testing.T, b *webtest.Browser, id int64, field string, kv ...string) (string, url.Values) {
	t.Helper()
	target, ok := sections(t, b, id)[field]
	if !ok {
		t.Fatalf("no section shows %s", field)
	}
	return target, with(formValues(t, b.Get(t, target), target), kv...)
}

// CD-50: a section saves only its fields, with the posted version; the rest
// goes through Update as stored, and the version moves by one. SHREE's addresses
// go first, because a party can't change to a PAN its GSTINs don't carry
// (MST-040, CD-167).
func TestScreens_EditSavesPerSection(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	b := w.kavya()
	terms := w.terms(t, "NET30", "Net 30 days")
	shree := w.get(t, "SHREE")
	w.exec(t, `DELETE FROM address WHERE party_id = ?`, shree.ID)
	in := input(shree)
	in.IsCustomer, in.DefaultPaymentTermsID = true, terms
	if _, err := w.parties.Update(ctx, w.h.Env.Kavya, shree.ID, shree.Version, in); err != nil {
		t.Fatal(err)
	}
	before := w.get(t, "SHREE")
	audits := func() int {
		return w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type = ? AND doc_id = ?`, party.DocType, before.ID)
	}
	entries := audits()

	page := b.Get(t, recordURL(before.ID))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "SHREE Shree Steel Traders")
	webtest.WantText(t, page, "AABCS1429B")
	webtest.WantText(t, page, "Registered Regular")
	webtest.WantText(t, page, "NET30 Net 30 days") // CD-68
	if page.HasControl("name") {
		t.Errorf("the record page shows the form; want the summary")
	}
	found := sections(t, b, before.ID)
	var links []string
	page.Doc.Find(`a[href^="` + recordURL(before.ID) + `/edit/"]`).Each(func(_ int, s *goquery.Selection) {
		if !strings.HasPrefix(webtest.Squash(s.Text()), "Change") {
			t.Errorf("section link says %q; want Change", webtest.Squash(s.Text()))
		}
		href, _ := s.Attr("href")
		links = append(links, href)
	})
	if len(links) < 2 {
		t.Fatalf("sections = %q; want PAN's apart from the others", links)
	}
	target := found["pan"]
	if target == "" || found["name"] == target {
		t.Fatalf("pan is on %q and name on %q; want separate sections", target, found["name"])
	}
	for _, field := range []string{"code", "name", "is_vendor", "is_customer", "pan", "gst_category", "default_payment_terms_id"} {
		if found[field] == "" {
			t.Errorf("no section shows %s", field)
		}
	}

	section := b.Get(t, target)
	webtest.WantStatus(t, section, http.StatusOK)
	value(t, section, "pan", "AABCS1429B")
	noControl(t, section, "name")
	version, _ := section.FormValue(target, "version")
	if version != strconv.Itoa(before.Version) {
		t.Errorf("hidden version = %q; want %d", version, before.Version)
	}

	done := b.Post(t, target, with(formValues(t, section, target), "pan", panMehta))
	webtest.WantRedirect(t, done, recordURL(before.ID))
	webtest.WantBanner(t, b.Get(t, done.Location), "Party saved.")

	after := w.get(t, "SHREE")
	if after.PAN != panMehta || after.Version != before.Version+1 {
		t.Errorf("PAN %q version %d; want %s, %d", after.PAN, after.Version, panMehta, before.Version+1)
	}
	if after.Name != before.Name || after.IsVendor != before.IsVendor || after.IsCustomer != before.IsCustomer ||
		after.GSTCategory != before.GSTCategory || after.DefaultPaymentTermsID != terms {
		t.Errorf("another section's field moved: %+v; were %+v", after, before)
	}
	if got := audits(); got != entries+1 {
		t.Errorf("audit entries = %d; want %d", got, entries+1)
	}

	// The other sections, saved as they stand, keep every Input field; a change
	// on one keeps PAN's, and PAN's the one it made.
	seen := map[string]bool{target: true}
	for _, href := range links {
		if seen[href] {
			continue
		}
		seen[href] = true
		b0 := w.get(t, "SHREE")
		webtest.WantRedirect(t, b.Post(t, href, formValues(t, b.Get(t, href), href)), recordURL(before.ID))
		if got := w.get(t, "SHREE"); got != b0 {
			t.Errorf("saving %s as it stands changed %+v to %+v", href, b0, got)
		}
	}
	nameSection, form := sectionForm(t, b, before.ID, "name", "name", "Shree Steels")
	webtest.WantRedirect(t, b.Post(t, nameSection, form), recordURL(before.ID))
	if got := w.get(t, "SHREE"); got.Name != "Shree Steels" || got.PAN != panMehta || got.GSTCategory != gstin.Regular ||
		got.DefaultPaymentTermsID != terms || !got.IsCustomer || !got.IsVendor {
		t.Errorf("after the name's section: %+v", got)
	}
}

// A stale section save is DOC's 409 with the typed values kept and a Reload, at
// the version posted, and writes nothing.
func TestScreens_EditSectionStale(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	target, form := sectionForm(t, b, shree.ID, "name", "name", "Typed Name", "version", strconv.Itoa(shree.Version-1))
	stale := b.Post(t, target, form)
	webtest.WantStatus(t, stale, http.StatusConflict)
	webtest.WantBanner(t, stale, staleShree)
	value(t, stale, "name", "Typed Name")
	if got, _ := stale.FormValue(target, "version"); got != strconv.Itoa(shree.Version-1) {
		t.Errorf("hidden version = %q; want %d", got, shree.Version-1)
	}
	if _, found := stale.RowLink(target); !found {
		t.Errorf("the 409 has no Reload to the section\ntext: %s", stale.Text())
	}
	if got := w.get(t, "SHREE"); got != shree {
		t.Errorf("a stale save wrote: %+v", got)
	}
	form.Del("version")
	webtest.WantStatus(t, b.Post(t, target, form), http.StatusBadRequest)
}

// Sections replace the whole-record form (CD-222): a post to the record isn't a
// save, an unknown section is a 404, and so is an unknown party.
func TestScreens_EditRoutes(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	webtest.WantStatus(t, b.Post(t, recordURL(shree.ID), url.Values{"version": {strconv.Itoa(shree.Version)}, "name": {"x"}}), http.StatusMethodNotAllowed)
	webtest.WantStatus(t, b.Get(t, recordURL(shree.ID)+"/edit/nosuch"), http.StatusNotFound)
	webtest.WantStatus(t, b.Post(t, recordURL(shree.ID)+"/edit/nosuch", url.Values{"version": {"1"}}), http.StatusNotFound)
	webtest.WantStatus(t, b.Get(t, recordURL(999999)), http.StatusNotFound)
}

// Each refusal Party's service names, through the section that shows the field.
func TestScreens_EditRefusals(t *testing.T) {
	const (
		otherPANShree = "GSTIN 27AABCS1429B1ZU belongs to PAN AABCS1429B, but this party's PAN is " + panMehta + ". Use a separate party for a different business."
		overseasShree = "GSTIN 27AABCS1429B1ZU doesn't match GST category Overseas. Check the GSTIN and the category."
		inUse         = "C-ONLY is used as a customer on Project PRJ-MIX. Disable the party instead."
	)
	for _, c := range []struct {
		name, code string
		setup      func(t *testing.T, w *world)
		kv         []string
		terms      bool // kv is the id of the NET30 setup made
		field, msg string
	}{
		{name: "PAN format", code: "SHREE", kv: []string{"pan", "AABCS1429"}, field: "pan", msg: msgPAN},
		{name: "PAN of the GSTINs", code: "SHREE", kv: []string{"pan", panMehta}, field: "pan", msg: otherPANShree},
		{name: "no role", code: "SHREE", kv: []string{"is_vendor", "", "is_customer", ""}, field: "is_vendor", msg: msgRole},
		{name: "code taken", code: "SHREE", kv: []string{"code", "BHARAT"}, field: "code", msg: "Code BHARAT is already used by Party Bharat Tools. Choose another code."},
		{name: "name", code: "SHREE", kv: []string{"name", ""}, field: "name", msg: msgNameReq},
		{name: "category of the GSTINs", code: "SHREE", kv: []string{"gst_category", "Overseas"}, field: "gst_category", msg: overseasShree},
		{name: "terms unknown", code: "SHREE", kv: []string{"default_payment_terms_id", "999999"}, field: "default_payment_terms_id", msg: msgNoTerms},
		{name: "terms not a number", code: "SHREE", kv: []string{"default_payment_terms_id", "abc"}, field: "default_payment_terms_id", msg: msgNoTerms},
		{
			name: "terms disabled", code: "SHREE",
			setup: func(t *testing.T, w *world) {
				t.Helper()
				w.exec(t, `UPDATE payment_terms SET disabled = 1 WHERE id = ?`, w.terms(t, "NET30", "Net 30 days"))
			},
			terms: true, field: "default_payment_terms_id", msg: msgOffTerms,
		},
		{
			name: "a role in use", code: "C-ONLY",
			setup: func(t *testing.T, w *world) {
				t.Helper()
				c := w.create(t, party.Input{Code: "C-ONLY", Name: "Only Customer Ltd", IsCustomer: true})
				if _, err := project.New(w.h.Env.Kit).Create(context.Background(), w.h.Env.Kavya, project.Input{
					Code: "PRJ-MIX", Name: "Mixed Project", BranchID: w.m.Branches["PUNE"], CustomerID: c.ID,
				}); err != nil {
					t.Fatal(err)
				}
			},
			kv: []string{"is_customer", "", "is_vendor", "1"}, field: "is_customer", msg: inUse,
		},
		{
			name: "a registered party needs its billing address", code: "UNR",
			setup: func(t *testing.T, w *world) {
				t.Helper()
				p := w.create(t, party.Input{Code: "UNR", Name: "Unregistered Ltd", IsVendor: true})
				if _, err := w.addresses.Create(context.Background(), w.h.Env.Kavya, kolkata(p.ID)); err != nil {
					t.Fatal(err)
				}
			},
			kv: []string{"gst_category", "Registered Regular"}, field: "gst_category", msg: msgNeedBill,
		},
		{
			name: "an address outside India", code: "OVS",
			setup: func(t *testing.T, w *world) {
				t.Helper()
				p := w.create(t, party.Input{Code: "OVS", Name: "Overseas Ltd", IsVendor: true, GSTCategory: gstin.Overseas})
				in := kolkata(p.ID)
				in.Country, in.StateCode, in.Pincode = "Singapore", "", "118524"
				if _, err := w.addresses.Create(context.Background(), w.h.Env.Kavya, in); err != nil {
					t.Fatal(err)
				}
			},
			kv: []string{"gst_category", "Registered Regular"}, field: "gst_category", msg: msgOverseas,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			if c.setup != nil {
				c.setup(t, w)
			}
			p := w.get(t, c.code)
			kv := c.kv
			if c.terms {
				id := w.count(t, `SELECT id FROM payment_terms WHERE code = 'NET30'`)
				kv = []string{"default_payment_terms_id", strconv.Itoa(id)}
			}
			target, form := sectionForm(t, b, p.ID, c.field, kv...)
			r := b.Post(t, target, form)
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldErrorID(t, r, components.FieldID(c.field), c.msg)
			if got := w.get(t, c.code); got != p {
				t.Errorf("a refused section wrote: %+v", got)
			}
			if got, _ := r.FormValue(target, "version"); got != strconv.Itoa(p.Version) {
				t.Errorf("hidden version = %q; want the posted %d", got, p.Version)
			}
		})
	}
}

// CD-217: the optional picker has Clear, which posts the link blank.
func TestScreens_EditClearsPaymentTerms(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	terms := w.terms(t, "NET30", "Net 30 days")
	shree := w.get(t, "SHREE")
	in := input(shree)
	in.DefaultPaymentTermsID = terms
	if _, err := w.parties.Update(context.Background(), w.h.Env.Kavya, shree.ID, shree.Version, in); err != nil {
		t.Fatal(err)
	}
	target, form := sectionForm(t, b, shree.ID, "default_payment_terms_id", "default_payment_terms_id", "")
	page := b.Get(t, target)
	webtest.WantText(t, page, "Payment Terms")
	webtest.WantText(t, page, "NET30 Net 30 days")
	if n := page.Doc.Find(`[data-picker-clear][aria-label="Clear Payment Terms"]`).Length(); n != 1 {
		t.Errorf("Clear controls on Payment Terms = %d; want 1", n)
	}
	webtest.WantRedirect(t, b.Post(t, target, form), recordURL(shree.ID))
	if got := w.get(t, "SHREE").DefaultPaymentTermsID; got != 0 {
		t.Errorf("terms = %d; want none", got)
	}
}

// The PAN box on a section is the CodeInput too.
func TestScreens_EditPANInput(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	target := sections(t, b, shree.ID)["pan"]
	wantCode(t, b.Get(t, target), "pan", "pan")
}

// A read-only user sees the summary with no Change or actions; the sections are
// refused, and the right comes before the form's own errors.
func TestScreens_EditReadOnly(t *testing.T) {
	w := newWorld(t)
	shree := w.get(t, "SHREE")
	target := sections(t, w.kavya(), shree.ID)["name"]
	priya := w.h.Browser(w.h.Env.Priya)
	page := priya.Get(t, recordURL(shree.ID))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "SHREE Shree Steel Traders")
	webtest.WantText(t, page, "Mumbai")
	for _, text := range []string{"Change", "Disable", "Delete", "Add address", "Add contact"} {
		webtest.WantNoText(t, page, text)
	}
	for _, r := range []webtest.Response{
		priya.Get(t, target),
		priya.Post(t, target, url.Values{"version": {strconv.Itoa(shree.Version)}, "name": {"x"}, "default_payment_terms_id": {"abc"}}),
	} {
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, msgNoWrite)
	}
	if got := w.get(t, "SHREE"); got != shree {
		t.Errorf("a read-only post wrote: %+v", got)
	}
}

// Ref is the service's: Party declares no dimension, so a project-restricted
// reader is unaffected; on the real auth.Service an own-scope user is refused
// as the service refuses, and a read-only record shows no Change.
func TestScreens_PartyScopeOnRealAuth(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	env := w.h.Env
	shree := w.get(t, "SHREE")
	target := sections(t, w.kavya(), shree.ID)["name"]
	webtest.WantStatus(t, w.h.Browser(env.Priya).Get(t, recordURL(shree.ID)), http.StatusOK)

	a := env.Kit.Auth.(*auth.Auth)
	set := func(rights map[auth.Right]auth.Scope) {
		t.Helper()
		if err := a.SetRights(ctx, env.Admin, "Purchase User", party.DocType, rights); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SetRights(ctx, env.Admin, "Employee", party.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatal(err)
	}
	set(map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn})
	webtest.WantStatus(t, w.h.Browser(env.Priya).Get(t, recordURL(shree.ID)), http.StatusNotFound)

	set(map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeOwn, auth.RightDelete: auth.ScopeOwn})
	priya := w.h.Browser(env.Priya)
	page := priya.Get(t, recordURL(shree.ID))
	webtest.WantStatus(t, page, http.StatusOK)
	for _, text := range []string{"Change", "Disable", "Delete"} {
		webtest.WantNoText(t, page, text)
	}
	version := url.Values{"version": {strconv.Itoa(shree.Version)}}
	r := priya.Post(t, target, version)
	webtest.WantStatus(t, r, http.StatusForbidden)
	_, err := w.parties.Update(ctx, env.Priya, shree.ID, shree.Version, input(shree))
	if err == nil {
		t.Fatal("the service let an own-scope user save")
	}
	webtest.WantText(t, r, err.Error())
	off := priya.Post(t, recordURL(shree.ID)+"/disable", version)
	webtest.WantStatus(t, off, http.StatusForbidden)
	webtest.WantText(t, off, err.Error())
	del := priya.Post(t, recordURL(shree.ID)+"/delete", version)
	webtest.WantStatus(t, del, http.StatusForbidden)
	webtest.WantText(t, del, env.Kit.Auth.Can(ctx, env.Priya, party.DocType, auth.RightDelete, &auth.RecordRef{DocType: party.DocType, ID: shree.ID}).Error())
	if got := w.get(t, "SHREE"); got != shree {
		t.Errorf("a refused action wrote: %+v", got)
	}
}

// The list: its columns, name order (fixture ids aren't in name order), disabled
// hidden until asked for, search, the empty search, and export.
func TestScreens_PartyList(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	w.disable(t, "LOCAL")

	page := b.Get(t, base)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "Parties")
	webtest.WantText(t, page, "4 parties")
	row, found := page.RowLink(recordURL(w.get(t, "SHREE").ID))
	if !found || !strings.Contains(row, "SHREE Shree Steel Traders") || !strings.Contains(row, "Registered Regular") {
		t.Errorf("row = %q, %v", row, found)
	}
	at := func(text string) int { return strings.Index(page.Body, text) }
	for _, pair := range [][2]string{{"Bharat Tools", "Coromandel Cables"}, {"Coromandel Cables", "Rajkot Castings"}, {"Rajkot Castings", "Shree Steel Traders"}} {
		if at(pair[0]) < 0 || at(pair[0]) > at(pair[1]) {
			t.Errorf("%s comes after %s; want name order", pair[0], pair[1])
		}
	}
	if _, found := page.RowLink(recordURL(w.get(t, "LOCAL").ID)); found {
		t.Errorf("a disabled party is listed by default")
	}

	all := b.Get(t, base+"?include_disabled=true")
	if row, _ := all.RowLink(recordURL(w.get(t, "LOCAL").ID)); !strings.Contains(row, "Disabled") {
		t.Errorf("disabled row = %q", row)
	}

	search := b.Get(t, base+"?q=Bharat")
	webtest.WantText(t, search, "BHARAT Bharat Tools")
	webtest.WantNoText(t, search, "Shree Steel Traders")
	webtest.WantText(t, b.Get(t, base+"?q=nosuchparty"), "No results.")

	csv := b.Get(t, base+"/export.csv")
	webtest.WantStatus(t, csv, http.StatusOK)
	if !strings.Contains(csv.Body, "SHREE") || strings.Contains(csv.Body, "LOCAL") {
		t.Errorf("export = %s", csv.Body)
	}
}

// Disable, enable and delete from the record page, with the delete confirm on
// it; a delete takes the party's own addresses and contacts (MST-046).
func TestScreens_PartyDisableEnableDelete(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	p := w.create(t, party.Input{Code: "C-TATA", Name: "Tata Projects", IsCustomer: true, GSTCategory: gstin.Unregistered})
	if _, err := w.addresses.Create(context.Background(), w.h.Env.Kavya, kolkata(p.ID)); err != nil {
		t.Fatal(err)
	}
	c := w.addContact(t, p.ID, "Sunil", "Mehta", true)
	before := w.tally(t)

	off := b.Post(t, recordURL(p.ID)+"/disable", url.Values{"version": {strconv.Itoa(p.Version)}})
	webtest.WantRedirect(t, off, recordURL(p.ID))
	webtest.WantBanner(t, b.Get(t, off.Location), "Party disabled.")
	if !w.get(t, "C-TATA").Disabled {
		t.Fatalf("party not disabled")
	}

	p = w.get(t, "C-TATA")
	on := b.Post(t, recordURL(p.ID)+"/enable", url.Values{"version": {strconv.Itoa(p.Version)}})
	webtest.WantRedirect(t, on, recordURL(p.ID))
	webtest.WantBanner(t, b.Get(t, on.Location), "Party enabled.")

	p = w.get(t, "C-TATA")
	stale := b.Post(t, recordURL(p.ID)+"/disable", url.Values{"version": {strconv.Itoa(p.Version - 1)}})
	webtest.WantStatus(t, stale, http.StatusConflict)

	webtest.WantText(t, b.Get(t, recordURL(p.ID)+"?delete"), "This can't be undone.")
	gone := b.Post(t, recordURL(p.ID)+"/delete", url.Values{"version": {strconv.Itoa(p.Version)}})
	webtest.WantRedirect(t, gone, base)
	webtest.WantBanner(t, b.Get(t, gone.Location), "Party deleted.")
	if _, ok := w.byCode(t, "C-TATA"); ok {
		t.Errorf("party still there")
	}
	if got := w.tally(t); got.parties != before.parties-1 || got.addresses != before.addresses-1 || got.contacts != before.contacts-1 {
		t.Errorf("tally = %+v; before %+v", got, before)
	}
	if got := w.count(t, `SELECT count(*) FROM contact WHERE id = ?`, c); got != 0 {
		t.Errorf("the contact is still there")
	}
}

// A used party can't be deleted: the service's text, on the record page.
func TestScreens_PartyDeleteRefusedWhenUsed(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	p := w.create(t, party.Input{Code: "C-ONLY", Name: "Only Customer Ltd", IsCustomer: true})
	if _, err := project.New(w.h.Env.Kit).Create(context.Background(), w.h.Env.Kavya, project.Input{
		Code: "PRJ-MIX", Name: "Mixed Project", BranchID: w.m.Branches["PUNE"], CustomerID: p.ID,
	}); err != nil {
		t.Fatal(err)
	}
	r := b.Post(t, recordURL(p.ID)+"/delete", url.Values{"version": {strconv.Itoa(p.Version)}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, r, "Cannot delete Party C-ONLY: it is used in Project PRJ-MIX. Disable it instead.")
	webtest.WantText(t, r, "C-ONLY Only Customer Ltd")
	if _, ok := w.byCode(t, "C-ONLY"); !ok {
		t.Errorf("the party went")
	}
}

// pickTexts is /pick/Party's rows for the query, as Kavya.
func (w *world) pickTexts(t *testing.T, query string) []string {
	t.Helper()
	r := w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party"+query)
	webtest.WantStatus(t, r, http.StatusOK)
	var out []string
	for _, row := range r.PickRows() {
		out = append(out, row.Text)
	}
	return out
}

func wantPick(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("picker rows = %q; want %q", got, want)
	}
}

// CD-218: role=customer lists only customers and role=vendor only vendors,
// disabled parties hidden; no role lists both; in name order, with the search.
func TestScreens_PartyPickerRole(t *testing.T) {
	w := newWorld(t)
	w.create(t, party.Input{Code: "C-ONLY", Name: "Only Customer Ltd", IsCustomer: true})
	w.create(t, party.Input{Code: "B-MIX", Name: "Mixed Party Ltd", IsVendor: true, IsCustomer: true})
	w.create(t, party.Input{Code: "C-OFF", Name: "Closed Customer Ltd", IsCustomer: true})
	w.disable(t, "C-OFF")
	w.disable(t, "RAJKOT")

	const (
		bharat  = "BHARAT Bharat Tools"
		corom   = "COROM Coromandel Cables"
		local   = "LOCAL Ganesh Hardware (unregistered)"
		mix     = "B-MIX Mixed Party Ltd"
		only    = "C-ONLY Only Customer Ltd"
		shreeRn = "SHREE Shree Steel Traders"
	)
	wantPick(t, w.pickTexts(t, "?role=customer"), mix, only)
	wantPick(t, w.pickTexts(t, "?role=vendor"), bharat, corom, local, mix, shreeRn)
	wantPick(t, w.pickTexts(t, ""), bharat, corom, local, mix, only, shreeRn)
	wantPick(t, w.pickTexts(t, "?q=Mixed&role=vendor"), mix)
	wantPick(t, w.pickTexts(t, "?q=Shree&role=customer"))
	wantPick(t, w.pickTexts(t, "?q=Only"), only)
}

// The production wiring mounts the wizard on the session manager WP3 passes,
// the record's routes, and the role picker Project's form searches.
func TestScreens_PartyMountedByAllmasters(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) {
			allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm, Files: (*files.Store)(nil)})
		}
	})
	if _, err := party.New(h.Env.Kit).Create(context.Background(), h.Env.Kavya, party.Input{Code: "C-ONLY", Name: "Only Customer Ltd", IsCustomer: true}); err != nil {
		t.Fatal(err)
	}
	b := h.Browser(h.Env.Kavya)
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	value(t, b.Get(t, stepDetails), "code", "MEHTA")
	pick := h.GetHTMX(t, h.Env.Kavya, "/pick/Party?role=vendor")
	if rows := pick.PickRows(); len(rows) != 0 {
		t.Errorf("role=vendor rows = %+v; want none", rows)
	}
	pick = h.GetHTMX(t, h.Env.Kavya, "/pick/Party?role=customer")
	if rows := pick.PickRows(); len(rows) != 1 || !strings.Contains(rows[0].Text, "C-ONLY") {
		t.Errorf("role=customer rows = %+v", rows)
	}
}
