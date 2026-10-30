package app_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
)

// dump is every column of the rows q selects, as text.
func (w *world) dump(q string, args ...any) []string {
	w.t.Helper()
	rows, err := w.app.DB().SQL().QueryContext(context.Background(), q, args...)
	if err != nil {
		w.t.Fatalf("%q: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		w.t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		vals, ptrs := make([]any, len(cols)), make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, fmt.Sprint(vals...))
	}
	if err := rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// NUM-026's DOC half (TestTotals_RecomputeIsStable proves the numbers, CD-40):
// the reference PO's totals saved again unchanged, through the real forms, keep
// every stored value and add nothing to the audit trail.
func TestNumCases_RecomputeRecordsNoAuditChange(t *testing.T) {
	c := newAuthSeed(t).open(t)
	priya := c.as(priyaEmail)
	id := priya.newOrder(c.m, "PRJ-METRO", "94164.00")
	priya.addLine(c.m, id)
	state := func() ([]string, []string, []string) {
		return c.dump(`SELECT * FROM fixture_order WHERE id = ?`, id),
			c.dump(`SELECT * FROM fixture_order_line WHERE parent_id = ? ORDER BY line_no`, id),
			c.dump(`SELECT * FROM audit_log WHERE doc_type = ? AND doc_id = ? ORDER BY id`, fixture.Order, id)
	}
	wantSame := func(what string, header, lines, audit []string) {
		t.Helper()
		h, l, a := state()
		if !slices.Equal(h, header) || !slices.Equal(l, lines) {
			t.Errorf("%s changed the stored document:\n%q %q\nwas\n%q %q", what, h, l, header, lines)
		}
		if !slices.Equal(a, audit) {
			t.Errorf("%s changed the audit trail:\n%q\nwas\n%q", what, a, audit)
		}
	}
	header, lines, audit := state()
	if len(lines) != 1 || len(audit) == 0 {
		t.Fatalf("setup: %d lines and %d audit rows, want 1 and some", len(lines), len(audit))
	}

	form := c.details(id, "")
	form.Set("amount", "94164.00")
	wantStatus(t, "save the header unchanged", priya.post(orderURL(id)+stepDetails, form), http.StatusSeeOther)
	wantSame("the header save", header, lines, audit)

	r := priya.post(orderURL(id)+"/lines/1", url.Values{
		"version": {c.version(id)}, "item_id": {idText(c.m.Item["MS-ANGLE-50"])},
		"qty": {"1200.000"}, "rate": {"62.5000"}, "uom": {"kg"},
	})
	wantStatus(t, "save the line unchanged", r, http.StatusSeeOther)
	wantSame("the line save", header, lines, audit)
}

// NUM-037's DOC half (TestGST_NoMixedTaxTypes proves CheckTaxType): a module's
// Validate hook refuses with CheckTaxType's message, and the real engine turns
// that into a 422 on the save with nothing written. Hooks are the path every
// GST document takes; the fixture's probe stands in for the module's hook.
func TestNumCases_SaveRefusesMixedTaxTypes(t *testing.T) {
	c := newAuthSeed(t).open(t)
	id := c.order(row{project: c.m.Project["PRJ-METRO"]})
	version := c.version(id)
	audit := c.count(`SELECT count(*) FROM audit_log`)
	for _, tc := range []struct {
		name             string
		interState       bool
		tax              gstcalc.TaxType
		supplier, branch string
		want             string
	}{
		{"intra-state with IGST", false, gstcalc.TaxIGST, "27", "27",
			"This bill is intra-state (supplier and branch are both in Maharashtra), so use CGST and SGST, not IGST."},
		{"inter-state with CGST", true, gstcalc.TaxCGSTSGST, "29", "27",
			"This bill is inter-state (Karnataka to Maharashtra), so use IGST, not CGST and SGST."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal := gstcalc.CheckTaxType(tc.interState, tc.tax, tc.supplier, tc.branch)
			if refusal == nil {
				t.Fatal("CheckTaxType accepted it")
			}
			ctx := fixture.WithProbe(context.Background(), &fixture.Probe{
				ValidateErrors: []doctype.FieldError{{Field: "supplier_id", Message: refusal.Error()}},
			})
			_, err := c.app.Engine().Do(ctx, c.principal(priyaID), fixture.Order, id, doctype.ActionSave, doc.ActionInput{
				Version: 1, Values: map[string]string{
					"supplier_id": idText(c.m.Party["SHREE"]), "project_id": idText(c.m.Project["PRJ-METRO"]),
					"order_date": "2026-10-24", "remarks": "changed",
				},
			})
			var he doctype.HTTPError
			if !errors.As(err, &he) || he.Status() != http.StatusUnprocessableEntity {
				t.Fatalf("save = %v, want a 422", err)
			}
			var msgs []string
			for _, f := range he.FieldErrors() {
				msgs = append(msgs, f.Message)
			}
			if got := strings.Join(msgs, "\n"); got != tc.want {
				t.Errorf("messages = %q, want %q", got, tc.want)
			}
			if got := c.version(id); got != version {
				t.Errorf("version = %s, want %s", got, version)
			}
			if got := c.text(`SELECT remarks FROM fixture_order WHERE id = ?`, id); got != "" {
				t.Errorf("remarks = %q: the refused save wrote", got)
			}
			if got := c.count(`SELECT count(*) FROM audit_log`); got != audit {
				t.Errorf("audit rows = %d, want %d", got, audit)
			}
		})
	}
}

const printWatermark = `<div class="watermark" data-status="draft">DRAFT</div>`

func printURL(id int64) string { return "/print/" + fixture.Order + "/" + idText(id) }

// Cases whose own tests stay in their packages over AUTH and DOC doubles, run
// here through the real router and real services.
func TestCrossCases_RealRouter(t *testing.T) {
	seed := newAuthSeed(t)
	for _, tc := range []struct {
		id  string
		run func(t *testing.T, c *authCase)
	}{
		{"NAM-047", func(t *testing.T, c *authCase) { // TestAmend_RequiresCancelled
			po := c.order(row{number: "PO/26-27/00007", state: "approved", project: c.m.Project["PRJ-METRO"]})
			r := c.as(raviEmail).post(orderURL(po)+"/amend", url.Values{"version": {c.version(po)}})
			wantRefused(t, "Ravi amends an approved order", r, http.StatusConflict, "Only cancelled documents can be amended. Cancel PO/26-27/00007 first.")
			if n := c.count(`SELECT count(*) FROM naming_issued WHERE root = ? AND amendment_no > 0`, "PO/26-27/00007"); n != 0 {
				t.Errorf("amendment suffixes issued = %d, want 0", n)
			}
		}},
		{"NAM-064", func(t *testing.T, c *authCase) { // TestAssign_AuditedWithTransition
			priya := c.as(priyaEmail)
			po := c.pendingOrder(c.as(adminEmail), priya, c.m)
			c.as(raviEmail).act(po, "approve", nil)
			changes := c.text(`SELECT changes FROM audit_log WHERE doc_type = ? AND doc_id = ? AND action = 'approve' AND user_id = ?`, fixture.Order, po, int64(raviID))
			for _, want := range []string{`"number"`, "PO/26-27/00001"} {
				if !strings.Contains(changes, want) {
					t.Errorf("approve audit changes %s, want %s", changes, want)
				}
			}
		}},
		{"NAM-065", func(t *testing.T, c *authCase) { // TestPrint_DraftLabel
			priya, ravi := c.as(priyaEmail), c.as(raviEmail)
			po := priya.newOrder(c.m, "PRJ-METRO", "94164.00")
			priya.addLine(c.m, po)
			label := "Draft " + idText(po)
			wantPage(t, "draft print", ravi.get(printURL(po)), []string{label, printWatermark}, nil)
			c.as(adminEmail).purchaseManagerRule()
			priya.act(po, "submit", nil)
			wantPage(t, "pending print", ravi.get(printURL(po)), []string{label, printWatermark}, nil)
			ravi.act(po, "approve", nil)
			wantPage(t, "approved print", ravi.get(printURL(po)), []string{"PO/26-27/00001"}, []string{label, printWatermark})
		}},
		{"FILE-050", func(t *testing.T, c *authCase) { // TestPrint_RequiresPrintRight
			c.setRights("Masters Manager", fixture.Order, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll})
			po := c.order(row{number: "PO/26-27/00001", state: "approved", project: c.m.Project["PRJ-TECHPARK"]})
			want404(t, "Priya, restricted to PRJ-METRO", c.as(priyaEmail).get(printURL(po)), "PO/26-27/00001")
			want403(t, "Kavya, read without print", c.as(kavyaEmail).get(printURL(po)), deny("print", "Purchase Order"), "PO/26-27/00001")
			wantPage(t, "Ravi", c.as(raviEmail).get(printURL(po)), []string{"PO/26-27/00001"}, nil)
		}},
		{"FILE-055", func(t *testing.T, c *authCase) { // TestPrint_WatermarkNotOverridable
			priya := c.as(priyaEmail)
			po := priya.newOrder(c.m, "PRJ-METRO", "1000.00")
			wantPage(t, "print with parameters", priya.get(printURL(po)+"?watermark=0&no_draft=1&status=approved"),
				[]string{"Draft " + idText(po), printWatermark}, nil)
		}},
		{"FILE-062", func(t *testing.T, c *authCase) { // TestPrint_IsReadOnly
			po := c.order(row{number: "PO/26-27/00001", state: "approved", project: c.m.Project["PRJ-METRO"]})
			before := c.dump(`SELECT * FROM fixture_order WHERE id = ?`, po)
			audit := c.count(`SELECT count(*) FROM audit_log`)
			ravi := c.as(raviEmail)
			for range 3 {
				wantStatus(t, "Ravi prints", ravi.get(printURL(po)), http.StatusOK)
			}
			if after := c.dump(`SELECT * FROM fixture_order WHERE id = ?`, po); !slices.Equal(before, after) {
				t.Errorf("printing changed the order: %q, was %q", after, before)
			}
			if got := c.count(`SELECT count(*) FROM audit_log`); got != audit {
				t.Errorf("audit rows = %d, want %d", got, audit)
			}
		}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t, seed.open(t))
		})
	}
}
