package allmasters_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-233 on Party's screens, real auth.Service: Payment Terms is the one Link
// these forms draw, on the wizard's first step and the terms section. Address
// and Contact forms draw no Link: their owner comes from the URL, so a posted
// party_id or branch_id reads nowhere.

const (
	lpTermsField = "default_payment_terms_id"
	lpOwnTerms   = "MINE-1 Own Terms"
	lpOwnParty   = "MINE-1 Own Party"

	lpDetails = "/m/party/new/details"
	lpAddress = "/m/party/new/addresses"
	lpContact = "/m/party/new/contacts"
	lpCheck   = "/m/party/new/check"
)

// lpScreens is the production screens and a user with own scope on Party,
// Payment Terms, Address, Contact and Branch.
func lpScreens(t *testing.T) (*nsWorld, *webtest.Harness, auth.Principal) {
	t.Helper()
	w, h := pgScreens(t)
	own := w.lrScoped(t, lrLink{source: party.Kind, target: paymentterms.Kind})
	for _, docType := range []string{address.DocType, address.ContactDocType, company.DocType} {
		if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, lrRole, docType, pgOwnRights()); err != nil {
			t.Fatalf("SetRights(%s): %v", docType, err)
		}
	}
	return w, h, own
}

// lpBigIDs makes table's next ids long, so an id can be told from every other
// number on a page.
func lpBigIDs(t *testing.T, w *nsWorld, table string) {
	t.Helper()
	err := w.DB.Write(t.Context(), func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(t.Context(), `UPDATE sqlite_sequence SET seq = 7770000 WHERE name = ?`, table)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 1 {
			return err
		}
		_, err = tx.SQL().ExecContext(t.Context(), `INSERT INTO sqlite_sequence (name, seq) VALUES (?, 7770000)`, table)
		return err
	})
	if err != nil {
		t.Fatalf("sqlite_sequence: %v", err)
	}
}

// lpOwnRecord is a party own created, with the own terms as its stored link.
func lpOwnRecord(t *testing.T, w *nsWorld, own auth.Principal, terms int64) mstkit.Saved {
	t.Helper()
	return w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
		in := nsPartyInput(w, "SRC-1", "Row")
		in.DefaultPaymentTermsID = terms
		return w.svc.Party.Create(ctx, own, in)
	})
}

// TestCD233_PartySectionHidesTheLabel: the terms section's redraw, for a hidden
// id, a hidden disabled id and an unknown id; then a readable posted id and the
// stored link, which keeps its label after its target stops being readable.
func TestCD233_PartySectionHidesTheLabel(t *testing.T) {
	w, h, own := lpScreens(t)
	lpBigIDs(t, w, paymentterms.Kind.Table)
	terms := lrMaster(t, paymentterms.DocType).ops(w)
	secret := pgCreate(t, terms, w.Kavya, llHiddenCode, llHiddenName)
	off := pgCreate(t, terms, w.Kavya, "HIDE-2", "Secret Off")
	lrDisable(t, w, paymentterms.Kind, w.Kavya, off)
	mine := pgCreate(t, terms, own, "MINE-1", "Own Terms")
	other := pgCreate(t, terms, own, "MINE-2", "Other Terms")
	saved := lpOwnRecord(t, w, own, mine.ID)
	record := pgRecord("/m/party", saved)
	path := record + "/edit/terms"

	posted := func(id int64, version int) url.Values {
		return url.Values{"version": {strconv.Itoa(version)}, lpTermsField: {strconv.FormatInt(id, 10)}}
	}
	refuse := func(id int64) string {
		t.Helper()
		r := h.Post(t, own, path, posted(id, saved.Version))
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantText(t, r, lrTermsGone)
		return llPage(r, id)
	}
	before := w.pgCount(t, "party")
	unknown := refuse(lrUnknown)
	llWantHidden(t, "section", refuse(secret.ID), unknown)
	llWantHidden(t, "section, hidden and disabled", refuse(off.ID), unknown)
	if after := w.pgCount(t, "party"); after != before {
		t.Errorf("a refused save wrote: %+v, was %+v", after, before)
	}

	// A readable id keeps its label on a stale save, which is no field's fault.
	stale := h.Post(t, own, path, posted(other.ID, saved.Version-1))
	webtest.WantStatus(t, stale, http.StatusConflict)
	webtest.WantText(t, stale, "MINE-2 Other Terms")

	if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, lrRole, paymentterms.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	for name, r := range map[string]webtest.Response{"record": h.Get(t, own, record), "section": h.Get(t, own, path)} {
		webtest.WantStatus(t, r, http.StatusOK)
		if !strings.Contains(r.Text(), lpOwnTerms) {
			t.Errorf("%s: the stored link lost its label: %s", name, r.Text())
		}
	}
	redraw := h.Post(t, own, path, posted(mine.ID, saved.Version))
	webtest.WantStatus(t, redraw, http.StatusUnprocessableEntity)
	webtest.WantText(t, redraw, lpOwnTerms)
	// With no read on Payment Terms at all, a posted id is still only an id.
	llWantHidden(t, "section, no read right", refuse(secret.ID), unknown)
}

// lpDetailsForm is the wizard's first step with terms chosen.
func lpDetailsForm(id int64) url.Values {
	return url.Values{
		"code": {"SRC-9"}, "name": {"Row"}, "is_vendor": {"1"}, "gst_category": {"Unregistered"},
		lpTermsField: {strconv.FormatInt(id, 10)},
	}
}

// lpWizardPages walks one draft with terms id and returns every page that draws
// it: a step's refusal, Back, the return from Check, Check itself, and the last
// response, Save's.
func lpWizardPages(t *testing.T, h *webtest.Harness, own auth.Principal, id int64) (map[string]webtest.Response, webtest.Response) {
	t.Helper()
	b := h.Browser(own)
	good := lpDetailsForm(id)
	bad := lpDetailsForm(id)
	bad.Set("pan", "x")
	pages := map[string]webtest.Response{"step refusal": b.Post(t, lpDetails, bad)}
	webtest.WantStatus(t, pages["step refusal"], http.StatusUnprocessableEntity)

	webtest.WantRedirect(t, b.Post(t, lpDetails, good), lpAddress)
	webtest.WantRedirect(t, b.Post(t, lpAddress, url.Values{"nav": {"back"}}), lpDetails)
	pages["back"] = b.Get(t, lpDetails)

	webtest.WantRedirect(t, b.Post(t, lpDetails, good), lpAddress)
	webtest.WantRedirect(t, b.Post(t, lpAddress, url.Values{}), lpContact)
	webtest.WantRedirect(t, b.Post(t, lpContact, url.Values{}), lpCheck)
	pages["check"] = b.Get(t, lpCheck)
	pages["return from check"] = b.Get(t, lpDetails+"?return=check")
	return pages, b.Post(t, lpCheck, url.Values{})
}

// TestCD233_PartyWizardHidesTheLabel: every page of the draft, and Save, which
// writes nothing for a hidden id and goes back to the step with the not-found
// text. A readable id keeps its label and saves.
func TestCD233_PartyWizardHidesTheLabel(t *testing.T) {
	w, h, own := lpScreens(t)
	lpBigIDs(t, w, paymentterms.Kind.Table)
	terms := lrMaster(t, paymentterms.DocType).ops(w)
	secret := pgCreate(t, terms, w.Kavya, llHiddenCode, llHiddenName)
	off := pgCreate(t, terms, w.Kavya, "HIDE-2", "Secret Off")
	lrDisable(t, w, paymentterms.Kind, w.Kavya, off)
	mine := pgCreate(t, terms, own, "MINE-1", "Own Terms")

	before := w.pgCount(t, "party")
	unknown, unknownSave := lpWizardPages(t, h, own, lrUnknown)
	webtest.WantStatus(t, unknownSave, http.StatusUnprocessableEntity)
	for _, hidden := range []mstkit.Saved{secret, off} {
		pages, save := lpWizardPages(t, h, own, hidden.ID)
		for name, r := range pages {
			llWantHidden(t, name, llPage(r, hidden.ID), llPage(unknown[name], lrUnknown))
		}
		webtest.WantStatus(t, save, http.StatusUnprocessableEntity)
		webtest.WantText(t, save, lrTermsGone)
		llWantHidden(t, "save", llPage(save, hidden.ID), llPage(unknownSave, lrUnknown))
	}
	if after := w.pgCount(t, "party"); after != before {
		t.Errorf("a refused save wrote: %+v, was %+v", after, before)
	}

	pages, save := lpWizardPages(t, h, own, mine.ID)
	for name, r := range pages {
		if !strings.Contains(r.Text(), lpOwnTerms) {
			t.Errorf("%s: a readable id lost its label: %s", name, r.Text())
		}
	}
	webtest.WantStatus(t, save, http.StatusSeeOther)
	webtest.WantText(t, h.Get(t, own, save.Location), lpOwnTerms)
}

// lpRowKind is what differs between an address and a contact here.
type lpRowKind struct {
	slug, table string
	valid, bad  url.Values
	create      func(t *testing.T, w *nsWorld, p auth.Principal, partyID, branchID int64) mstkit.Saved
}

func lpRowKinds() []lpRowKind {
	return []lpRowKind{
		{
			slug: "addresses", table: "address",
			valid: url.Values{
				"title": {"Row"}, "line1": {"1 Main Road"}, "city": {"Mumbai"}, "state_code": {"27"},
				"country": {"India"}, "pincode": {"400001"}, "is_shipping": {"1"},
			},
			bad: url.Values{"title": {"Row"}, "city": {"Mumbai"}, "state_code": {"27"}, "country": {"India"}, "pincode": {"400001"}, "is_shipping": {"1"}},
			create: func(t *testing.T, w *nsWorld, p auth.Principal, partyID, branchID int64) mstkit.Saved {
				t.Helper()
				in := nsAddressInput(w, "", "Row")
				in.PartyID, in.BranchID = partyID, branchID
				return w.save(t, func(ctx context.Context) (mstkit.Saved, error) { return w.svc.Address.Create(ctx, p, in) })
			},
		},
		{
			slug: "contacts", table: "contact",
			valid: url.Values{"first_name": {"Row"}},
			bad:   url.Values{"last_name": {""}},
			create: func(t *testing.T, w *nsWorld, p auth.Principal, partyID, branchID int64) mstkit.Saved {
				t.Helper()
				in := nsContactInput(w, "", "Row")
				in.PartyID, in.BranchID = partyID, branchID
				return w.save(t, func(ctx context.Context) (mstkit.Saved, error) { return w.svc.Address.CreateContact(ctx, p, in) })
			},
		},
	}
}

// lpOwnerForm is form with a posted owner.
func lpOwnerForm(form url.Values, partyID, branchID int64, version int) url.Values {
	out := url.Values{"party_id": {strconv.FormatInt(partyID, 10)}, "branch_id": {strconv.FormatInt(branchID, 10)}, "version": {strconv.Itoa(version)}, "kind": {"shipping"}}
	for k, v := range form {
		out[k] = v
	}
	return out
}

// TestCD233_RowOwnerInTheURL: every address and contact route under a hidden
// party answers as under an unknown one, and writes nothing.
func TestCD233_RowOwnerInTheURL(t *testing.T) {
	w, h, own := lpScreens(t)
	lpBigIDs(t, w, party.Kind.Table)
	parties := lrMaster(t, party.DocType).ops(w)
	hidden := pgCreate(t, parties, w.Kavya, llHiddenCode, llHiddenName)

	for _, k := range lpRowKinds() {
		t.Run(k.slug, func(t *testing.T) {
			row := k.create(t, w, w.Kavya, hidden.ID, 0)
			before := w.pgCount(t, k.table)
			for _, c := range []struct {
				name, method, tail string
			}{
				{"new page", http.MethodGet, "/new"},
				{"create", http.MethodPost, "/new"},
				{"edit", http.MethodGet, "/" + strconv.FormatInt(row.ID, 10)},
				{"delete step", http.MethodGet, "/" + strconv.FormatInt(row.ID, 10) + "?delete"},
				{"save", http.MethodPost, "/" + strconv.FormatInt(row.ID, 10)},
				{"disable", http.MethodPost, "/" + strconv.FormatInt(row.ID, 10) + "/disable"},
				{"delete", http.MethodPost, "/" + strconv.FormatInt(row.ID, 10) + "/delete"},
				{"set primary", http.MethodPost, "/" + strconv.FormatInt(row.ID, 10) + "/set-primary"},
			} {
				at := func(id int64) string {
					t.Helper()
					target := "/m/party/" + strconv.FormatInt(id, 10) + "/" + k.slug + c.tail
					var r webtest.Response
					if c.method == http.MethodGet {
						r = h.Get(t, own, target)
					} else {
						r = h.Post(t, own, target, lpOwnerForm(k.valid, id, 0, row.Version))
					}
					webtest.WantStatus(t, r, http.StatusNotFound)
					webtest.WantText(t, r, auth.MsgNotFound)
					return llPage(r, id)
				}
				llWantHidden(t, c.name, at(hidden.ID), at(lrUnknown))
			}
			if after := w.pgCount(t, k.table); after != before {
				t.Errorf("a refused post wrote: %+v, was %+v", after, before)
			}
		})
	}
}

// TestCD233_RowFormsTakeNoPostedOwner: an address or contact form draws no link,
// so a posted party_id or branch_id neither shows nor lands. The row stays under
// the party in the URL, and a hidden id's page equals an unknown id's.
func TestCD233_RowFormsTakeNoPostedOwner(t *testing.T) {
	w, h, own := lpScreens(t)
	lpBigIDs(t, w, party.Kind.Table)
	lpBigIDs(t, w, company.Kind.Table)
	hiddenParty := pgCreate(t, lrMaster(t, party.DocType).ops(w), w.Kavya, llHiddenCode, llHiddenName)
	hiddenBranch := pgCreate(t, lrMaster(t, company.DocType).ops(w), w.Kavya, llHiddenCode, llHiddenName)
	mine := pgCreate(t, lrMaster(t, party.DocType).ops(w), own, "MINE-1", "Own Party")
	base := pgRecord("/m/party", mine)

	for _, k := range lpRowKinds() {
		t.Run(k.slug, func(t *testing.T) {
			row := k.create(t, w, own, mine.ID, 0)
			rowPath := base + "/" + k.slug + "/" + strconv.FormatInt(row.ID, 10)
			hide, unknown := lpOwnerForm(k.bad, hiddenParty.ID, hiddenBranch.ID, row.Version), lpOwnerForm(k.bad, lrUnknown, lrUnknown, row.Version)
			for name, path := range map[string]string{"create": base + "/" + k.slug + "/new", "save": rowPath} {
				refuse := func(form url.Values) string {
					t.Helper()
					r := h.Post(t, own, path, form)
					webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
					webtest.WantText(t, r, lpOwnParty)
					return r.Body
				}
				llWantHidden(t, name, llTokens.ReplaceAllString(refuse(hide), ""), llTokens.ReplaceAllString(refuse(unknown), ""))
			}

			owners := `SELECT count(*) FROM ` + k.table + ` WHERE party_id = ? AND branch_id IS NULL`
			webtest.WantRedirect(t, h.Post(t, own, base+"/"+k.slug+"/new", lpOwnerForm(k.valid, hiddenParty.ID, hiddenBranch.ID, 0)), base)
			webtest.WantRedirect(t, h.Post(t, own, rowPath, lpOwnerForm(k.valid, hiddenParty.ID, hiddenBranch.ID, row.Version)), base)
			if n := lpCount(t, w, owners, mine.ID); n != 2 {
				t.Errorf("rows under the URL's party = %d, want 2", n)
			}
			if n := lpCount(t, w, owners, hiddenParty.ID); n != 0 {
				t.Errorf("rows under the posted party = %d", n)
			}
		})
	}
}

func lpCount(t *testing.T, w *nsWorld, query string, args ...any) int {
	t.Helper()
	var n int
	read(t, w.Env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&n)
	})
	return n
}

// TestCD233_RowUnderTheWrongOwner: a row of a branch at a party's URL is a row
// that isn't there, on the page and on set-primary, though the user created it.
func TestCD233_RowUnderTheWrongOwner(t *testing.T) {
	w, h, own := lpScreens(t)
	branch := pgCreate(t, lrMaster(t, company.DocType).ops(w), own, "MINE-2", "Own Branch")
	mine := pgCreate(t, lrMaster(t, party.DocType).ops(w), own, "MINE-1", "Own Party")
	base := pgRecord("/m/party", mine)

	for _, k := range lpRowKinds() {
		t.Run(k.slug, func(t *testing.T) {
			lpBigIDs(t, w, k.table)
			row := k.create(t, w, own, 0, branch.ID)
			before := w.pgCount(t, k.table)
			at := func(id int64, tail string) string {
				t.Helper()
				target := base + "/" + k.slug + "/" + strconv.FormatInt(id, 10) + tail
				var r webtest.Response
				if tail == "" {
					r = h.Get(t, own, target)
				} else {
					r = h.Post(t, own, target, lpOwnerForm(k.valid, mine.ID, 0, row.Version))
				}
				webtest.WantStatus(t, r, http.StatusNotFound)
				return llPage(r, id)
			}
			llWantHidden(t, "page", at(row.ID, ""), at(lrUnknown, ""))
			if k.slug == "addresses" { // a contact's own refusal is worded: reviewfix_test.go in partyweb
				llWantHidden(t, "set primary", at(row.ID, "/set-primary"), at(lrUnknown, "/set-primary"))
			}
			if after := w.pgCount(t, k.table); after != before {
				t.Errorf("a refused post wrote: %+v, was %+v", after, before)
			}
		})
	}
}

// TestCD233_WizardRowsTakeNoPostedOwner: a wizard row's posted party_id or
// branch_id is no field of the step, so the rows go to the new party.
func TestCD233_WizardRowsTakeNoPostedOwner(t *testing.T) {
	w, h, own := lpScreens(t)
	lpBigIDs(t, w, party.Kind.Table)
	lpBigIDs(t, w, company.Kind.Table)
	hiddenParty := pgCreate(t, lrMaster(t, party.DocType).ops(w), w.Kavya, llHiddenCode, llHiddenName)
	hiddenBranch := pgCreate(t, lrMaster(t, company.DocType).ops(w), w.Kavya, llHiddenCode, llHiddenName)

	rowsOf := func(form url.Values, partyID, branchID int64) url.Values {
		out := url.Values{
			"lines[1].party_id":  {strconv.FormatInt(partyID, 10)},
			"lines[1].branch_id": {strconv.FormatInt(branchID, 10)},
		}
		for k, v := range form {
			out["lines[1]."+k] = v
		}
		return out
	}
	kinds := lpRowKinds()
	draft := func(partyID, branchID int64) (string, *webtest.Browser) {
		t.Helper()
		b := h.Browser(own)
		details := lpDetailsForm(0)
		details.Del(lpTermsField)
		webtest.WantRedirect(t, b.Post(t, lpDetails, details), lpAddress)
		webtest.WantRedirect(t, b.Post(t, lpAddress, rowsOf(kinds[0].valid, partyID, branchID)), lpContact)
		webtest.WantRedirect(t, b.Post(t, lpContact, rowsOf(kinds[1].valid, partyID, branchID)), lpCheck)
		check := b.Get(t, lpCheck)
		webtest.WantStatus(t, check, http.StatusOK)
		return llTokens.ReplaceAllString(check.Body, ""), b
	}
	unknown, _ := draft(lrUnknown, lrUnknown)
	hidden, b := draft(hiddenParty.ID, hiddenBranch.ID)
	llWantHidden(t, "check", hidden, unknown)

	saved := b.Post(t, lpCheck, url.Values{})
	webtest.WantStatus(t, saved, http.StatusSeeOther)
	id, err := strconv.ParseInt(strings.TrimPrefix(saved.Location, "/m/party/"), 10, 64)
	if err != nil {
		t.Fatalf("saved at %q: %v", saved.Location, err)
	}
	for _, table := range []string{"address", "contact"} {
		owners := `SELECT count(*) FROM ` + table + ` WHERE party_id = ? AND branch_id IS NULL`
		if n := lpCount(t, w, owners, id); n != 1 {
			t.Errorf("%s rows under the new party = %d, want 1", table, n)
		}
		if n := lpCount(t, w, owners, hiddenParty.ID); n != 0 {
			t.Errorf("%s rows under the posted party = %d", table, n)
		}
	}
}
