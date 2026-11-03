package purchase_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers and tests for P13 (MOD-PO P13, create PO from a request; PO spec
// §20.4 item 1). Every document row is written through Engine.Do or the
// purchasetest builders; the SQL here only reads. All names carry p13.

// The P13 texts, copied exactly (PO spec §20.4, DOC-069's stale wording,
// party.CheckRole's vendor text).
const (
	p13PickVendor = "Pick a vendor."
	p13Nothing    = "Everything on %s is already on an order." // request display
	p13NotVendor  = "Acme Builders is not marked as a vendor. Set the party to Vendor or Both first."
	p13NeededBy   = "2026-11-07"
)

func p13Stale(display string) string {
	return "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
}

// p13Row is one request row of qty of item in unit, with a description.
func p13Row(t *testing.T, env *purchasetest.Env, item, unit, qty, description string) map[string]string {
	t.Helper()
	return map[string]string{
		"item_id": masterID(t, env.DB, "item", item), "uom_id": masterID(t, env.DB, "uom", unit),
		"qty": qty, "description": description,
	}
}

// p13Request is Suresh's submitted request (METRO-SITE / PRJ-METRO, needed by
// 07-11-2026) with rows, built through Engine.Do.
func p13Request(t *testing.T, env *purchasetest.Env, rows ...map[string]string) doc.Doc {
	t.Helper()
	return purchasetest.NewRequest(t, env,
		purchasetest.RequestHeader(map[string]string{"required_by": "07-11-2026"}),
		purchasetest.RequestLines(rows...))
}

// p13Hold is Priya's SHREE PO, stopped at stop, with one line of qty of item
// in unit that links request row rowID (request_line_id), so it holds or
// orders that row (PO spec §20.2).
func p13Hold(t *testing.T, env *purchasetest.Env, stop func(*purchasetest.Spec), rowID int64, item, unit, hsn, qty string) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, env, stop, purchasetest.OrderLines(map[string]string{
		"item_id": masterID(t, env.DB, "item", item), "uom_id": masterID(t, env.DB, "uom", unit), "hsn_sac": hsn,
		"qty": qty, "request_line_id": fmt.Sprint(rowID),
	}))
}

// p13Create runs CreateFromRequest as p for the vendor with code vendor ("":
// no vendor, 0) at version.
func p13Create(t *testing.T, env *purchasetest.Env, p auth.Principal, requestID int64, version int, vendor string) (int64, error) {
	t.Helper()
	var vendorID int64
	if vendor != "" {
		vendorID = p5ID(t, masterID(t, env.DB, "party", vendor))
	}
	return env.Service.CreateFromRequest(t.Context(), p, requestID, version, vendorID)
}

// p13Status is err's HTTP status; 0 when it carries none.
func p13Status(err error) int {
	var he doctype.HTTPError
	if errors.As(err, &he) {
		return he.Status()
	}
	return 0
}

// p13Msgs are the messages a refusal shows: its field errors' messages, or
// its own text when it has none (as the handler passes it to the redraw).
func p13Msgs(err error) []string {
	var he doctype.HTTPError
	if errors.As(err, &he) && len(he.FieldErrors()) > 0 {
		var out []string
		for _, fe := range he.FieldErrors() {
			out = append(out, fe.Message)
		}
		return out
	}
	if err == nil {
		return nil
	}
	return []string{err.Error()}
}

// p13WantRefused asserts err has status and exactly the one message msg.
func p13WantRefused(t *testing.T, what string, err error, status int, msg string) {
	t.Helper()
	if got := p13Msgs(err); p13Status(err) != status || len(got) != 1 || got[0] != msg {
		t.Errorf("%s: err = %v (status %d, messages %q), want %d with exactly %q", what, err, p13Status(err), got, status, msg)
	}
}

// p13Same is a stored value compared in its plain form.
func p13Same(t *testing.T, what string, got any, want string) {
	t.Helper()
	if p2Plain(got) != want {
		t.Errorf("%s = %s, want %s", what, p2Plain(got), want)
	}
}

// p13Qty, p13Rate and p13Pct are a literal's stored plain form.
func p13Qty(t *testing.T, s string) string {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatalf("qty %q: %v", s, err)
	}
	return p2Plain(q)
}

func p13Rate(t *testing.T, s string) string {
	t.Helper()
	return p2Plain(p7Rate(t, s))
}

func p13Pct(t *testing.T, s string) string {
	t.Helper()
	return p2Plain(p7Pct(t, s))
}

// PO-051 (PO spec §20.4 item 1, §20.2; plan P13; CD-245, CD-247 (7), CD-94):
// CreateFromRequest checks rights, then version, then vendor (CD-259): a
// user without create on Purchase Order is AUTH's 403 even with a stale
// version or a blank vendor; then the request's version (DOC-069's 409), then
// a blank vendor (`Pick a vendor.`). It takes only the rows with pending to
// order above 0 (§20.4's text when none) and creates the PO draft through
// Engine.Do as the buyer, with the vendor, the request's site, project and
// needed-by date, and one line per pending row at that quantity, in the
// request row's unit, with request_line_id. The price is defaultPrice's for
// that vendor. A missing or hidden request is AUTH's 404; create's own
// refusals pass through. A refusal writes nothing.
//
// The subtests marked "waits on P6" reach PendingToOrder (P6), so they pass
// once P6's PendingToOrder exists and P13 is built on it.
func TestRequest_CreatePO(t *testing.T) {
	t.Run("draft with the request's site, project, needed-by and only its pending rows (waits on P6)", func(t *testing.T) {
		env := purchasetest.New(t)
		req := p13Request(t, env,
			p13Row(t, env, "MS-ANGLE-50", "KGS", "100", "For the north columns"),
			p13Row(t, env, "BOLT-M12", "NOS", "50", "M12 for base plates"),
			p13Row(t, env, "WIRE-18G", "KGS", "20", "Binding wire, bay 3"),
		)
		rows := req.Lines
		p13Hold(t, env, purchasetest.StopAtDraft(), rows[0].ID, "MS-ANGLE-50", "KGS", "7216", "40") // holds 40 of 100
		p13Hold(t, env, purchasetest.StopAtApproved(), rows[2].ID, "WIRE-18G", "KGS", "7217", "20") // orders all 20
		orders := countRows(t, env.DB, "purchase_order", "1 = 1")

		id, err := p13Create(t, env, env.Priya, req.ID, p5Version(t, env, req.ID), "SHREE")
		if err != nil {
			t.Fatalf("CreateFromRequest as Priya: %v %+v", err, p5Fields(err))
		}
		if got := countRows(t, env.DB, "purchase_order", "1 = 1"); got != orders+1 {
			t.Errorf("orders after the create = %d, want %d (one new draft)", got, orders+1)
		}
		po := p5Load(t, env, purchase.DocType, id)
		if po.State != doctype.StateDraft || po.CreatedBy != env.Priya.ID {
			t.Errorf("PO %d is %s owned by %d, want a draft owned by Priya (%d)", id, po.State, po.CreatedBy, env.Priya.ID)
		}
		p13Same(t, "vendor_id", po.Header["vendor_id"], masterID(t, env.DB, "party", "SHREE"))
		p13Same(t, "site_id", po.Header["site_id"], p2Plain(req.Header["site_id"]))
		p13Same(t, "project_id", po.Header["project_id"], p2Plain(req.Header["project_id"]))
		if got := p5Plain(po.Header["required_by"]); got != p13NeededBy {
			t.Errorf("required_by = %s, want the request's needed-by %s", got, p13NeededBy)
		}

		want := []struct {
			row                          int
			item, unit, qty, description string
		}{
			{0, "MS-ANGLE-50", "KGS", "60", "For the north columns"}, // 100 less the 40 held
			{1, "BOLT-M12", "NOS", "50", "M12 for base plates"},
		}
		if len(po.Lines) != len(want) {
			t.Fatalf("PO lines = %d, want %d (the fully ordered WIRE-18G row is left out)", len(po.Lines), len(want))
		}
		for i, w := range want {
			l := po.Lines[i].Values
			what := fmt.Sprintf("line %d", i+1)
			p13Same(t, what+" request_line_id", l["request_line_id"], fmt.Sprint(rows[w.row].ID))
			p13Same(t, what+" item_id", l["item_id"], masterID(t, env.DB, "item", w.item))
			p13Same(t, what+" uom_id", l["uom_id"], masterID(t, env.DB, "uom", w.unit))
			p13Same(t, what+" qty", l["qty"], p13Qty(t, w.qty))
			p13Same(t, what+" description", l["description"], w.description)
		}
	})

	t.Run("the price is the last price for the posted vendor (waits on P6)", func(t *testing.T) {
		env := purchasetest.New(t)
		line := func(price, discount string) map[string]string {
			return map[string]string{"price": price, "discount_pct": discount}
		}
		// SHREE's last price is older than BHARAT's, so only a per-vendor
		// lookup gives 58 less 5% for SHREE.
		p7Priya(t, env, "SHREE", "22-10-2026", purchasetest.StopAtApproved(), line("58", "5"))
		p7Priya(t, env, "BHARAT", "23-10-2026", purchasetest.StopAtApproved(), line("64", "0"))

		for _, c := range []struct{ vendor, price, discount string }{
			{"SHREE", "58", "5"},
			{"BHARAT", "64", "0"},
		} {
			req := p13Request(t, env, p13Row(t, env, "MS-ANGLE-50", "KGS", "100", ""))
			id, err := p13Create(t, env, env.Priya, req.ID, p5Version(t, env, req.ID), c.vendor)
			if err != nil {
				t.Fatalf("CreateFromRequest for %s: %v %+v", c.vendor, err, p5Fields(err))
			}
			po := p5Load(t, env, purchase.DocType, id)
			p13Same(t, c.vendor+" PO vendor_id", po.Header["vendor_id"], masterID(t, env.DB, "party", c.vendor))
			if len(po.Lines) != 1 {
				t.Fatalf("%s PO lines = %d, want 1", c.vendor, len(po.Lines))
			}
			p13Same(t, c.vendor+" PO price", po.Lines[0].Values["price"], p13Rate(t, c.price))
			p13Same(t, c.vendor+" PO discount_pct", po.Lines[0].Values["discount_pct"], p13Pct(t, c.discount))
		}
	})

	t.Run("nothing pending is §20.4's text and writes nothing (waits on P6)", func(t *testing.T) {
		env := purchasetest.New(t)
		req := p13Request(t, env,
			p13Row(t, env, "MS-ANGLE-50", "KGS", "100", ""),
			p13Row(t, env, "BOLT-M12", "NOS", "50", ""),
		)
		p13Hold(t, env, purchasetest.StopAtDraft(), req.Lines[0].ID, "MS-ANGLE-50", "KGS", "7216", "100") // held
		p13Hold(t, env, purchasetest.StopAtApproved(), req.Lines[1].ID, "BOLT-M12", "NOS", "7318", "50")  // ordered
		before := p5CountAll(t, env)
		_, err := p13Create(t, env, env.Priya, req.ID, p5Version(t, env, req.ID), "SHREE")
		p13WantRefused(t, "nothing pending", err, http.StatusUnprocessableEntity, fmt.Sprintf(p13Nothing, naming.Label(req.Number, req.ID)))
		p5SameCounts(t, "nothing pending", before, env)
	})

	// Rights, then version, then vendor: the stale version is checked after
	// the rights (the Suresh subtest below) and before the vendor.
	t.Run("a stale version is DOC's 409 naming the request, after the rights and before the vendor", func(t *testing.T) {
		env := purchasetest.New(t)
		req := p13Request(t, env, p13Row(t, env, "MS-ANGLE-50", "KGS", "100", ""))
		v := p5Version(t, env, req.ID)
		stale := p13Stale(naming.Label(req.Number, req.ID))
		for _, c := range []struct {
			name    string
			version int
			vendor  string
		}{
			{"older version", v - 1, "SHREE"},
			{"newer version", v + 1, "SHREE"},
			{"older version and no vendor", v - 1, ""},
		} {
			before := p5CountAll(t, env)
			_, err := p13Create(t, env, env.Priya, req.ID, c.version, c.vendor)
			p13WantRefused(t, c.name, err, http.StatusConflict, stale)
			p5SameCounts(t, c.name, before, env)
		}
	})

	t.Run("rights come before the version and the vendor: Suresh is AUTH's 403 (CD-259)", func(t *testing.T) {
		env := purchasetest.New(t)
		// Suresh (Stores) reads his own approved request but has no create
		// on Purchase Order.
		req := p13Request(t, env, p13Row(t, env, "MS-ANGLE-50", "KGS", "100", ""))
		v := p5Version(t, env, req.ID)
		want := &auth.DeniedError{Right: auth.RightCreate, Label: "Purchase Order", Plural: "Purchase Orders"}
		for _, c := range []struct {
			name    string
			version int
			vendor  string
		}{
			{"stale version", v - 1, "SHREE"},
			{"blank vendor", v, ""},
		} {
			before := p5CountAll(t, env)
			_, err := p13Create(t, env, env.Suresh, req.ID, c.version, c.vendor)
			var de *auth.DeniedError
			if !errors.As(err, &de) || de.Right != want.Right || de.Label != want.Label {
				t.Errorf("Suresh, %s: err = %v (status %d), want *auth.DeniedError for create on Purchase Order", c.name, err, p13Status(err))
			}
			p5WantDenied(t, "Suresh, "+c.name, err, want.Error())
			p5SameCounts(t, "Suresh, "+c.name, before, env)
		}
	})

	t.Run("the line keeps the request row's unit when it isn't the stock unit (§20.4; waits on P6)", func(t *testing.T) {
		env := purchasetest.New(t)
		// BOLT-M12's stock unit is NOS; BOX (100 Nos) is a unit it allows.
		req := p13Request(t, env, p13Row(t, env, "BOLT-M12", "BOX", "2", "Two boxes for the base plates"))
		id, err := p13Create(t, env, env.Priya, req.ID, p5Version(t, env, req.ID), "SHREE")
		if err != nil {
			t.Fatalf("CreateFromRequest as Priya: %v %+v", err, p5Fields(err))
		}
		po := p5Load(t, env, purchase.DocType, id)
		if len(po.Lines) != 1 {
			t.Fatalf("PO lines = %d, want 1", len(po.Lines))
		}
		box, nos := masterID(t, env.DB, "uom", "BOX"), masterID(t, env.DB, "uom", "NOS")
		if box == nos {
			t.Fatalf("BOX and NOS share id %s", box)
		}
		p13Same(t, "line 1 uom_id (the request row's BOX, not the stock NOS)", po.Lines[0].Values["uom_id"], box)
		p13Same(t, "line 1 qty", po.Lines[0].Values["qty"], p13Qty(t, "2"))
	})

	t.Run("a blank vendor is Pick a vendor. on vendor_id", func(t *testing.T) {
		env := purchasetest.New(t)
		req := p13Request(t, env, p13Row(t, env, "MS-ANGLE-50", "KGS", "100", ""))
		before := p5CountAll(t, env)
		_, err := p13Create(t, env, env.Priya, req.ID, p5Version(t, env, req.ID), "")
		p13WantRefused(t, "blank vendor", err, http.StatusUnprocessableEntity, p13PickVendor)
		if fe := p5Fields(err); len(fe) != 1 || fe[0].Field != "vendor_id" {
			t.Errorf("blank vendor: field errors %+v, want one on vendor_id", fe)
		}
		p5SameCounts(t, "blank vendor", before, env)
	})

	t.Run("a missing or hidden request is AUTH's 404", func(t *testing.T) {
		env := purchasetest.New(t)
		// Ravi's TECH-SITE / PRJ-TECHPARK request, which Priya (PRJ-METRO
		// only) can't read.
		res := p5Must(t, "Ravi's TECH-SITE request")(p5Create(t, env, env.Ravi, p5Header(t, env, "TECH-SITE", "PRJ-TECHPARK"), p5Lines(t, env)))
		p5Must(t, "submit Ravi's request")(p5Do(t, env, env.Ravi, res.ID, doctype.ActionSubmit, ""))
		v := p5Version(t, env, res.ID)

		before := p5CountAll(t, env)
		_, err := p13Create(t, env, env.Priya, res.ID, v, "SHREE")
		p5WantNotFound(t, "hidden request", err)
		_, err = p13Create(t, env, env.Priya, 999_999, 1, "SHREE")
		p5WantNotFound(t, "missing request", err)
		p5SameCounts(t, "missing or hidden request", before, env)
	})

	t.Run("create's own refusals pass through and write nothing (waits on P6)", func(t *testing.T) {
		env := purchasetest.New(t)
		if _, err := party.New(p2Kit(env)).Create(t.Context(), env.Admin, party.Input{Code: "ACME", Name: "Acme Builders", IsCustomer: true}); err != nil {
			t.Fatalf("create customer ACME: %v", err)
		}
		req := p13Request(t, env, p13Row(t, env, "MS-ANGLE-50", "KGS", "100", ""))
		v := p5Version(t, env, req.ID)

		before := p5CountAll(t, env)
		_, err := p13Create(t, env, env.Priya, req.ID, v, "ACME")
		p5WantInvalid(t, "a party that isn't a vendor", err, doctype.FieldError{Field: "vendor_id", Message: p13NotVendor})
		p5SameCounts(t, "a party that isn't a vendor", before, env)

		// Suresh reads his own request but can't create a PO (Stores).
		_, err = p13Create(t, env, env.Suresh, req.ID, v, "SHREE")
		p5WantDenied(t, "Suresh creates", err, (&auth.DeniedError{Right: auth.RightCreate, Label: "Purchase Order", Plural: "Purchase Orders"}).Error())
		p5SameCounts(t, "Suresh creates", before, env)
	})
}
