package purchase_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers for P12's tests (MOD-PO P12, Copy; PO spec §24.1). Documents are
// written through the builders and the engine; the SQL here only reads.

// p12AUTH099 is AUTH-099's text for a copier whose sites are PUNE-WH and
// METRO-SITE (CD-149: the codes sorted).
const p12AUTH099 = "You can only use Sites METRO-SITE, PUNE-WH. Choose one of your allowed sites."

// DOC's disabled-link texts (kernel/doc msgLinkDisabled: "{code} {name}",
// the field label lower-cased) for the default PO's project and site.
const (
	p12MsgProjectOff = "Project PRJ-METRO Pune Metro Depot is disabled. Pick an active project, or ask an admin to enable it."
	p12MsgSiteOff    = "Deliver to METRO-SITE METRO-SITE is disabled. Pick an active deliver to, or ask an admin to enable it."
)

// p12First is the first copy note for a source shown as display (spec §24.1).
func p12First(display string) string {
	return "Copied from " + display + ". Rates are as on that order; check them before you submit."
}

// p12Blank is the note for a header link left blank (spec §24.1).
func p12Blank(label, code string) string {
	return label + " " + code + " is disabled, so it was left blank."
}

// p12Dropped is the note for a source line left out (spec §24.1). The label
// is mid-sentence, so callers pass it lower-cased as DOC writes a label there
// (doc/validate.go, strings.ToLower(f.Label)): "item", "uom", "tax code".
func p12Dropped(n int, display, label, code string) string {
	return "Line " + strconv.Itoa(n) + " of " + display + " was left out: " + label + " " + code + " is disabled."
}

// p12Header are the header columns read back from a copy.
var p12Header = []string{
	"vendor_id", "vendor_address_id", "site_id", "project_id", "payment_terms_id", "terms", "reverse_charge",
	"number", "order_date", "required_by", "vendor_quote_ref", "remarks", "earlier_po_no", "earlier_po_date",
	"copied_from_no", "copied_from_id", "copy_notes", "created_by", "state", "version",
}

// p12Copied are the header columns a copy carries over (spec §24.1).
var p12Copied = []string{"vendor_id", "vendor_address_id", "site_id", "project_id", "payment_terms_id", "terms", "reverse_charge"}

// p12LineCopied are the line columns a copy carries over (spec §24.1).
var p12LineCopied = []string{"item_id", "description", "uom_id", "qty", "price", "discount_pct", "tax_code_id", "hsn_sac"}

// p12LineCols are the line columns read back from a copy.
var p12LineCols = append(append([]string{}, p12LineCopied...), "already_received", "request_line_id")

// p12Terms creates payment terms code (100% in 30 days) and returns its id.
func p12Terms(t *testing.T, env *purchasetest.Env, code, name string) string {
	t.Helper()
	rows, err := paymentterms.ParseRows([]paymentterms.RawRow{{Portion: "100", Days: "30"}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := paymentterms.New(p2Kit(env)).Create(t.Context(), env.Admin, paymentterms.Input{Code: code, Name: name, Rows: rows})
	if err != nil {
		t.Fatalf("create terms %s: %v", code, err)
	}
	return strconv.FormatInt(saved.ID, 10)
}

// p12Disable disables the master with code in table through its service.
func p12Disable(t *testing.T, env *purchasetest.Env, table, code string) {
	t.Helper()
	kit := p2Kit(env)
	var svc interface {
		SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error
	}
	switch table {
	case "party":
		svc = party.New(kit)
	case "payment_terms":
		svc = paymentterms.New(kit)
	case "item":
		svc = item.New(kit)
	case "uom":
		svc = uom.New(kit)
	case "tax_code":
		svc = taxcode.New(kit)
	case "site":
		svc = site.New(kit)
	case "project":
		svc = project.New(kit)
	default:
		t.Fatalf("p12Disable: no service for %s", table)
	}
	ctx := t.Context()
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		var id int64
		var version int
		if err := tx.SQL().QueryRowContext(ctx, `SELECT id, version FROM `+table+` WHERE code = ?`, code).Scan(&id, &version); err != nil { //nolint:gosec // G202: fixed table names
			return err
		}
		return svc.SetDisabledTx(ctx, tx, env.Admin, id, version, true)
	}); err != nil {
		t.Fatalf("disable %s %s: %v", table, code, err)
	}
}

// p12DisableAddress disables party's address in city through the address
// service.
func p12DisableAddress(t *testing.T, env *purchasetest.Env, party, city string) {
	t.Helper()
	ctx := t.Context()
	id := p2Int(t, p2AddressID(t, env, party, city))
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		var version int
		if err := tx.SQL().QueryRowContext(ctx, `SELECT version FROM address WHERE id = ?`, id).Scan(&version); err != nil {
			return err
		}
		return address.New(p2Kit(env)).SetDisabledTx(ctx, tx, env.Admin, id, version, true)
	}); err != nil {
		t.Fatalf("disable %s's %s address: %v", party, city, err)
	}
}

// p12Copy is env.Service.Copy as p, failing the test on an error.
func p12Copy(t *testing.T, svc *purchase.Service, p auth.Principal, id int64) int64 {
	t.Helper()
	newID, err := svc.Copy(t.Context(), p, id)
	if err != nil {
		t.Fatalf("Copy(%s, %d): %v", p.Name, id, err)
	}
	if newID == 0 || newID == id {
		t.Fatalf("Copy(%s, %d) = new id %d, want a new PO", p.Name, id, newID)
	}
	return newID
}

// p12Notes is order id's copy_notes.
func p12Notes(t *testing.T, env *purchasetest.Env, id int64) string {
	t.Helper()
	return p2Snap(t, env, id, "copy_notes")["copy_notes"]
}

// p12WantRefused asserts err is an HTTP error with status and text, that no
// id was returned and that no order or line was written.
func p12WantRefused(t *testing.T, what string, newID int64, err error, status int, text string, before p2Counts, env *purchasetest.Env) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != status || err.Error() != text {
		t.Errorf("%s: err = %v, want %d %q", what, err, status, text)
	}
	if newID != 0 {
		t.Errorf("%s: new id %d, want 0", what, newID)
	}
	p2SameCounts(t, what, before, env)
}

// p12ArjunTech is an approved TECH-SITE / PRJ-TECHPARK PO by Arjun (no
// restrictions), which Priya (PRJ-METRO only) can't read.
func p12ArjunTech(t *testing.T, env *purchasetest.Env) int64 {
	t.Helper()
	res := p2Must(t, "Arjun saves a PRJ-TECHPARK PO")(p2Create(t, env, env.Arjun,
		p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5")}))
	if res = p2Must(t, "Arjun submits it")(p2Do(t, env, env.Arjun, res.ID, doc.ActionSubmit, "")); res.State != doctype.StateApproved {
		t.Fatalf("Arjun's PRJ-TECHPARK PO is %s, want approved", res.State)
	}
	return res.ID
}

// p12Rich is an approved PO by Priya that sets every column of spec §24.1,
// copied or not: SHREE's Thane address (not its default), NET30, terms,
// reverse charge, required by, quote ref, remarks, earlier PO fields, ordered
// 20-10-2026 (the clock is 24-10-2026), and the reference lines with a
// description, Already received and a request row on line 1, a discount, an
// HSN/SAC and GST12 on line 2 (none the item's defaults), plus line 3 of 3 BOX
// of BOLT-M12 (not its stock UoM). It also has a comment and a Sent record.
func p12Rich(t *testing.T, env *purchasetest.Env) doc.Doc {
	t.Helper()
	req := purchasetest.NewRequest(t, env, purchasetest.RequestLines(map[string]string{"qty": "1200"}))
	lines := append(p2RefLines(t, env), p2Line(t, env, "BOLT-M12", "BOX", "3", "850"))
	lines[0]["description"] = "MS Angle 50x50x5, 6 m lengths"
	lines[0]["already_received"] = "200"
	lines[0]["request_line_id"] = strconv.FormatInt(req.Lines[0].ID, 10)
	lines[1]["discount_pct"] = "5"
	lines[1]["hsn_sac"] = "72171010"
	lines[1]["tax_code_id"] = masterID(t, env.DB, "tax_code", "GST12")
	po := purchasetest.NewOrder(t, env, purchasetest.OrderLines(lines...), purchasetest.OrderHeader(map[string]string{
		"vendor_address_id": p2AddressID(t, env, "SHREE", "Thane"),
		"payment_terms_id":  p12Terms(t, env, "NET30", "Net 30 days"),
		"order_date":        "20-10-2026",
		"required_by":       "30-10-2026",
		"vendor_quote_ref":  "SST/Q/778",
		"terms":             "Delivery within 7 days of the order.",
		"reverse_charge":    "true",
		"remarks":           "Call Mr Shah before delivery.",
		"earlier_po_no":     "OLD/145",
		"earlier_po_date":   "15-10-2026",
	}))
	if _, err := env.Comments.AddComment(t.Context(), env.Priya, purchase.DocType, po.ID, "Rates agreed on the phone."); err != nil {
		t.Fatalf("comment on the source: %v", err)
	}
	if err := env.Service.MarkSent(t.Context(), env.Priya, po.ID, 0, purchase.SentInput{SentOn: types.Today(env.Clock), SentVia: "email"}); err != nil {
		t.Fatalf("mark the source sent: %v", err)
	}
	return po
}

// PO-064 (PO spec §24.1, §26; CD-245, CD-247 (7), CD-280 D7 (a), CD-299;
// plan P12): Copy creates a draft owned by the copier through Engine.Do with
// only §24.1's copied columns, copied_from_no and copied_from_id, then writes
// copy_notes once with UPDATE … WHERE id=? AND version=1.
func TestPO_Copy(t *testing.T) {
	t.Run("copied and not-copied columns, owned by the copier", func(t *testing.T) {
		env := purchasetest.New(t)
		src := p12Rich(t, env)
		display := naming.Label(src.Number, src.ID)
		if src.Number == "" {
			t.Fatalf("source has no number; want an approved PO")
		}
		srcHead := p2Snap(t, env, src.ID, p12Header...)
		for _, c := range append(append([]string{}, p12Copied...), "required_by", "vendor_quote_ref", "remarks", "earlier_po_no", "earlier_po_date") {
			if srcHead[c] == "" || srcHead[c] == "0" {
				t.Fatalf("source %s = %q, want it set so the read-back proves something", c, srcHead[c])
			}
		}
		srcLines := p2LineSnap(t, env, src.ID, p12LineCols...)
		if len(srcLines) != 3 || srcLines[0]["already_received"] == "0" || srcLines[0]["request_line_id"] == "" || srcLines[1]["discount_pct"] == "0" {
			t.Fatalf("source lines %v, want three with Already received and a request row on line 1, a discount on line 2", srcLines)
		}
		before := countRows(t, env.DB, "purchase_order", "1 = 1")

		newID := p12Copy(t, env.Service, env.Arjun, src.ID)

		if got := countRows(t, env.DB, "purchase_order", "1 = 1"); got != before+1 {
			t.Errorf("orders after Copy = %d, want %d (one new draft)", got, before+1)
		}
		got := p2Snap(t, env, newID, p12Header...)
		for _, c := range p12Copied {
			if got[c] != srcHead[c] {
				t.Errorf("copy %s = %q, want the source's %q", c, got[c], srcHead[c])
			}
		}
		want := map[string]string{
			"number": "", "order_date": p2OrderDate, "required_by": "", "vendor_quote_ref": "", "remarks": "",
			"earlier_po_no": "", "earlier_po_date": "",
			"copied_from_no": display, "copied_from_id": strconv.FormatInt(src.ID, 10),
			"copy_notes": p12First(display),
			"created_by": strconv.FormatInt(int64(env.Arjun.ID), 10), "state": string(doctype.StateDraft), "version": "1",
		}
		for c, w := range want {
			if got[c] != w {
				t.Errorf("copy %s = %q, want %q", c, got[c], w)
			}
		}
		lines := p2LineSnap(t, env, newID, p12LineCols...)
		if len(lines) != len(srcLines) {
			t.Fatalf("copy has %d lines %v, want %d", len(lines), lines, len(srcLines))
		}
		for i, l := range lines {
			for _, c := range p12LineCopied {
				if l[c] != srcLines[i][c] {
					t.Errorf("copy line %d %s = %q, want the source's %q", i+1, c, l[c], srcLines[i][c])
				}
			}
			if l["already_received"] != "0" || l["request_line_id"] != "" {
				t.Errorf("copy line %d already_received %q request_line_id %q, want 0 and blank (not copied)", i+1, l["already_received"], l["request_line_id"])
			}
		}
		if n := countRows(t, env.DB, "module_comment", "doctype = ? AND record_id = ?", purchase.DocType, newID); n != 0 {
			t.Errorf("copy has %d comments, want none (not copied)", n)
		}
		if n := countRows(t, env.DB, "purchase_order_sent", "po_id = ?", newID); n != 0 {
			t.Errorf("copy has %d Sent rows, want none (not copied)", n)
		}
		if n := countRows(t, env.DB, "module_comment", "doctype = ? AND record_id = ?", purchase.DocType, src.ID); n != 1 {
			t.Errorf("source has %d comments after Copy, want its 1", n)
		}

		// The copier owns the draft: Arjun saves it, Priya (the source's owner) can't.
		p2Must(t, "Arjun saves his copy")(p2Save(t, env, env.Arjun, newID, map[string]string{"remarks": "Mine now."}, nil))
		if _, err := p2Save(t, env, env.Priya, newID, map[string]string{"remarks": "Hers?"}, nil); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("Priya saves Arjun's copy: err = %v, want AUTH's 403 (own-only write)", err)
		}
	})

	t.Run("disabled payment terms and item: left blank, line left out, notes", func(t *testing.T) {
		env := purchasetest.New(t)
		src := p12Rich(t, env)
		display := naming.Label(src.Number, src.ID)
		srcLines := p2LineSnap(t, env, src.ID, p12LineCopied...)
		p12Disable(t, env, "payment_terms", "NET30")
		p12Disable(t, env, "item", "WIRE-18G")

		newID := p12Copy(t, env.Service, env.Arjun, src.ID)

		if got := p2Snap(t, env, newID, "payment_terms_id", "vendor_id")["payment_terms_id"]; got != "" {
			t.Errorf("copy payment_terms_id = %q, want blank (NET30 is disabled)", got)
		}
		lines := p2LineSnap(t, env, newID, p12LineCopied...)
		if len(lines) != 2 || !mapsEqual(lines[0], srcLines[0]) || !mapsEqual(lines[1], srcLines[2]) {
			t.Errorf("copy lines = %v, want the source's lines 1 and 3 %v %v (line 2's item is disabled)", lines, srcLines[0], srcLines[2])
		}
		want := strings.Join([]string{p12First(display), p12Blank("Payment terms", "NET30"), p12Dropped(2, display, "item", "WIRE-18G")}, "\n")
		if got := p12Notes(t, env, newID); got != want {
			t.Errorf("copy_notes =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("disabled UoM and tax code: their lines left out, in line order", func(t *testing.T) {
		env := purchasetest.New(t)
		src := purchasetest.NewOrder(t, env, purchasetest.OrderLines(
			p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "62.5"),
			p2Line(t, env, "BOLT-M12", "NOS", "40", "12"),
			p2Line(t, env, "WIRE-18G", "KGS", "50", "96"),
			p2Line(t, env, "SVC-CRANE", "HRS", "8", "1500", "GST12"),
		))
		display := naming.Label(src.Number, src.ID)
		srcLines := p2LineSnap(t, env, src.ID, p12LineCopied...)
		p12Disable(t, env, "uom", "NOS")
		p12Disable(t, env, "tax_code", "GST12")

		newID := p12Copy(t, env.Service, env.Arjun, src.ID)

		lines := p2LineSnap(t, env, newID, p12LineCopied...)
		if len(lines) != 2 || !mapsEqual(lines[0], srcLines[0]) || !mapsEqual(lines[1], srcLines[2]) {
			t.Errorf("copy lines = %v, want the source's lines 1 and 3 %v %v", lines, srcLines[0], srcLines[2])
		}
		want := strings.Join([]string{
			p12First(display), p12Dropped(2, display, "uom", "NOS"), p12Dropped(4, display, "tax code", "GST12"),
		}, "\n")
		if got := p12Notes(t, env, newID); got != want {
			t.Errorf("copy_notes =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("disabled vendor is left blank", func(t *testing.T) {
		env := purchasetest.New(t)
		src := purchasetest.NewOrder(t, env)
		display := naming.Label(src.Number, src.ID)
		p12Disable(t, env, "party", "SHREE")

		newID := p12Copy(t, env.Service, env.Arjun, src.ID)

		if got := p2Snap(t, env, newID, "vendor_id")["vendor_id"]; got != "" {
			t.Errorf("copy vendor_id = %q, want blank (SHREE is disabled)", got)
		}
		// CD-302: with the vendor blank, its address and the vendor the
		// address was defaulted for are blank too.
		if got := p2Snap(t, env, newID, "vendor_address_id", "snap_vendor_id"); got["vendor_address_id"] != "" || got["snap_vendor_id"] != "" {
			t.Errorf("copy vendor_address_id %q snap_vendor_id %q, want both blank (CD-302)", got["vendor_address_id"], got["snap_vendor_id"])
		}
		if n := countRows(t, env.DB, "purchase_order_line", "parent_id = ?", newID); n != 1 {
			t.Errorf("copy has %d lines, want the source's 1", n)
		}
		want := p12First(display) + "\n" + p12Blank("Vendor", "SHREE")
		if got := p12Notes(t, env, newID); got != want {
			t.Errorf("copy_notes =\n%q\nwant\n%q", got, want)
		}
	})

	// AD-13 ruling (b), plan P12 fix round 1: an address has no code, so the
	// note names it as DOC does (kernel/doc/validate.go: the name alone),
	// never "Vendor address  is disabled". Validate then gives the copy the
	// vendor's default address (§24.3).
	t.Run("disabled vendor address: the note names it, the vendor's default comes back", func(t *testing.T) {
		env := purchasetest.New(t)
		src := p12Rich(t, env)
		display := naming.Label(src.Number, src.ID)
		thane, primary := p2AddressID(t, env, "SHREE", "Thane"), p2PrimaryBilling(t, env, "SHREE")
		if thane == primary {
			t.Fatalf("Thane is SHREE's primary billing address; want another so the default is visible")
		}
		if got := p2Snap(t, env, src.ID, "vendor_address_id")["vendor_address_id"]; got != thane {
			t.Fatalf("source vendor_address_id = %q, want Thane %s", got, thane)
		}
		p12DisableAddress(t, env, "SHREE", "Thane")

		newID := p12Copy(t, env.Service, env.Arjun, src.ID)

		want := strings.Join([]string{p12First(display), "Vendor address Thane is disabled, so it was left blank."}, "\n")
		if got := p12Notes(t, env, newID); got != want {
			t.Errorf("copy_notes =\n%q\nwant\n%q", got, want)
		}
		if got := p2Snap(t, env, newID, "vendor_id", "vendor_address_id"); got["vendor_address_id"] != primary || got["vendor_id"] != masterID(t, env.DB, "party", "SHREE") {
			t.Errorf("copy vendor_id %q vendor_address_id %q, want SHREE and its primary billing address %s (Validate's default)",
				got["vendor_id"], got["vendor_address_id"], primary)
		}
	})

	// AD-13 ruling (a), plan P12 fix round 1: Copy checks only §24.1's three
	// header links. A disabled site or project goes to create, which refuses
	// it with DOC's disabled text; Copy returns that and writes nothing.
	for _, c := range []struct{ name, table, code, field, text string }{
		{"disabled project: DOC's refusal, nothing written", "project", "PRJ-METRO", "project_id", p12MsgProjectOff},
		{"disabled site: DOC's refusal, nothing written", "site", "METRO-SITE", "site_id", p12MsgSiteOff},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := purchasetest.New(t)
			src := purchasetest.NewOrder(t, env) // METRO-SITE / PRJ-METRO
			p12Disable(t, env, c.table, c.code)
			before := p2CountAll(t, env)

			newID, err := env.Service.Copy(t.Context(), env.Arjun, src.ID)

			p2WantInvalid(t, c.name, err, doctype.FieldError{Field: c.field, Message: c.text})
			if newID != 0 {
				t.Errorf("%s: new id %d, want 0", c.name, newID)
			}
			p2SameCounts(t, c.name, before, env)
		})
	}

	t.Run("a draft source, and the copy never carries copy_notes", func(t *testing.T) {
		env := purchasetest.New(t)
		src := purchasetest.NewOrder(t, env)
		first := p12Copy(t, env.Service, env.Arjun, src.ID)
		firstNotes := p12Notes(t, env, first)
		if firstNotes != p12First(src.Number) {
			t.Fatalf("first copy's notes %q, want %q", firstNotes, p12First(src.Number))
		}

		second := p12Copy(t, env.Service, env.Priya, first) // a draft, by someone who can read it
		draft := naming.Label("", first)
		got := p2Snap(t, env, second, "copied_from_no", "copied_from_id", "copy_notes", "created_by")
		want := map[string]string{
			"copied_from_no": draft, "copied_from_id": strconv.FormatInt(first, 10), "copy_notes": p12First(draft),
			"created_by": strconv.FormatInt(int64(env.Priya.ID), 10),
		}
		if !maps.Equal(got, want) {
			t.Errorf("copy of a draft = %v, want %v", got, want)
		}
	})

	t.Run("amend never carries copy_notes", func(t *testing.T) {
		env := purchasetest.New(t)
		src := purchasetest.NewOrder(t, env) // ₹7,375.00: approved on submit
		cp := p12Copy(t, env.Service, env.Arjun, src.ID)
		if res := p2Must(t, "Arjun submits his copy")(p2Do(t, env, env.Arjun, cp, doc.ActionSubmit, "")); res.State != doctype.StateApproved {
			t.Fatalf("submitted copy is %s, want approved", res.State)
		}
		p2Must(t, "Ravi cancels the copy")(p2Do(t, env, env.Ravi, cp, doc.ActionCancel, "Ordered twice by mistake."))
		res := p2Must(t, "Arjun amends the copy")(p2Do(t, env, env.Arjun, cp, doc.ActionAmend, ""))
		if res.ID == cp {
			t.Fatalf("amend = %+v, want a new draft", res)
		}
		if got := p12Notes(t, env, res.ID); got != "" {
			t.Errorf("amended draft copy_notes = %q, want blank", got)
		}
	})

	t.Run("copying one PO never changes another draft's notes", func(t *testing.T) {
		env := purchasetest.New(t)
		a := purchasetest.NewOrder(t, env)
		b := purchasetest.NewOrder(t, env, purchasetest.OrderLines(p2RefLines(t, env)...), purchasetest.StopAtDraft())
		plain := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		copyA := p12Copy(t, env.Service, env.Arjun, a.ID)
		if p2Version(t, env, copyA) != 1 || p2Version(t, env, plain.ID) != 1 {
			t.Fatalf("drafts at versions %d and %d, want both at 1 so a loose WHERE would reach them",
				p2Version(t, env, copyA), p2Version(t, env, plain.ID))
		}

		copyB := p12Copy(t, env.Service, env.Arjun, b.ID)

		if got := p12Notes(t, env, copyA); got != p12First(a.Number) {
			t.Errorf("first copy's notes after another Copy = %q, want its own %q", got, p12First(a.Number))
		}
		if got := p12Notes(t, env, plain.ID); got != "" {
			t.Errorf("an unrelated draft's notes after Copy = %q, want blank", got)
		}
		if got := p12Notes(t, env, copyB); got != p12First(naming.Label("", b.ID)) {
			t.Errorf("second copy's notes = %q, want %q", got, p12First(naming.Label("", b.ID)))
		}
	})

	t.Run("notes skipped when the draft changed before the notes write", func(t *testing.T) {
		env := purchasetest.New(t)
		src := purchasetest.NewOrder(t, env)
		hook := &purchasetest.CreateHook{Engine: env.Engine}
		hook.After = func(_ context.Context, p auth.Principal, res doc.Result) {
			p2Must(t, "save the new draft before Copy writes its notes")(
				p2Save(t, env, p, res.ID, map[string]string{"remarks": "Changed first."}, nil))
		}
		svc := env.ServiceWith(hook)

		newID := p12Copy(t, svc, env.Arjun, src.ID)

		got := p2Snap(t, env, newID, "copy_notes", "remarks", "version", "copied_from_no")
		want := map[string]string{"copy_notes": "", "remarks": "Changed first.", "version": "2", "copied_from_no": src.Number}
		if !maps.Equal(got, want) {
			t.Errorf("copy changed before its notes = %v, want %v (the notes skipped)", got, want)
		}
	})

	t.Run("a restricted copier is refused with AUTH-099's text", func(t *testing.T) {
		env := purchasetest.New(t)
		src := p12ArjunTech(t, env)
		// Arjun reads the source; his sites are cut to PUNE-WH and METRO-SITE
		// after Copy's read and before its create, the only way the create's
		// restriction check can refuse a PO the copier could read.
		// The counts are taken after the restriction change, whose own User
		// audit entry isn't Copy's.
		var before p2Counts
		hook := &purchasetest.CreateHook{Engine: env.Engine}
		hook.Before = func(ctx context.Context, p auth.Principal) {
			ids := []int64{p2Int(t, masterID(t, env.DB, "site", "PUNE-WH")), p2Int(t, masterID(t, env.DB, "site", "METRO-SITE"))}
			if err := env.Auth.ChangeRestrictions(ctx, env.Admin, p.ID, auth.DimSite, ids, nil, nil); err != nil {
				t.Fatalf("restrict %s: %v", p.Name, err)
			}
			before = p2CountAll(t, env)
		}
		svc := env.ServiceWith(hook)

		newID, err := svc.Copy(t.Context(), env.Arjun, src)

		p12WantRefused(t, "restricted copier", newID, err, http.StatusForbidden, p12AUTH099, before, env)
	})

	t.Run("unreadable, missing and no-create refusals", func(t *testing.T) {
		env := purchasetest.New(t)
		po := purchasetest.NewOrder(t, env)
		hidden := p12ArjunTech(t, env)
		deniedCreate := (&auth.DeniedError{Right: auth.RightCreate, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
		for _, c := range []struct {
			name   string
			p      auth.Principal
			id     int64
			status int
			text   string
		}{
			{"Priya on a PRJ-TECHPARK PO", env.Priya, hidden, http.StatusNotFound, auth.MsgNotFound},
			{"a missing PO", env.Arjun, 999_999, http.StatusNotFound, auth.MsgNotFound},
			{"Ravi has no create", env.Ravi, po.ID, http.StatusForbidden, deniedCreate},
			{"Suresh has no create", env.Suresh, po.ID, http.StatusForbidden, deniedCreate},
		} {
			t.Run(c.name, func(t *testing.T) {
				before := p2CountAll(t, env)
				newID, err := env.Service.Copy(t.Context(), c.p, c.id)
				p12WantRefused(t, c.name, newID, err, c.status, c.text, before, env)
			})
		}
		t.Run("Deepak can't read POs", func(t *testing.T) {
			before := p2CountAll(t, env)
			newID, err := env.Service.Copy(t.Context(), env.Deepak, po.ID)
			if !errors.Is(err, auth.ErrForbidden) && !errors.Is(err, auth.ErrNotFound) {
				t.Errorf("Deepak: err = %v, want AUTH's 403 or 404", err)
			}
			if newID != 0 {
				t.Errorf("Deepak: new id %d, want 0", newID)
			}
			p2SameCounts(t, "Deepak", before, env)
		})
	})
}
