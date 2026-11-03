package app_test

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// csKeep is, by module DocType, the Required values the draft keeps: the
// purchase order and line a goods receipt's line comes from.
var csKeep = map[string][]string{goodsreceipt.DocType: {"purchase_order_id", "po_line_id"}}

// RR1c 2: for every module DocType (purchase order and request, goods
// receipt, expense claim and advance), a draft with its Required header and
// line values blank (each line keeps a link, so it is a line) shows on Check
// every refusal Submit gives that draft, in Submit's own text: DOC's missing
// Required values on the lines too ("Line 1: Type is required."), not only
// the header's.
func TestCheckListsWhatSubmitRefuses_BlankRequired(t *testing.T) {
	for _, name := range []string{purchase.DocType, purchase.RequestDocType, goodsreceipt.DocType, expense.ClaimDocType, expense.AdvanceDocType} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := newRSEnv(t).doc[name]
			values, line := d.valid(t)
			for _, f := range d.dt.Fields {
				switch {
				case !f.Required || slices.Contains(csKeep[name], f.Name):
				case f.OnLine && f.Type != doctype.Link:
					delete(line, f.Name)
				case !f.OnLine:
					delete(values, f.Name)
				}
			}
			delete(line, "rejected_qty") // no more rejected than a blank received
			refused := csCompare(t, d, values, line, nil)
			if !slices.ContainsFunc(refused, func(m string) bool { return strings.HasPrefix(m, "Line 1: ") }) {
				t.Errorf("Submit refused %q: no line problem, so the line half of the check saw nothing", refused)
			}
		})
	}
}

// RR1c 2 and 3: a module rule Submit applies beyond DOC's Required values
// is listed on Check in Submit's text too: a purchase order whose vendor
// address has no state and no GSTIN (Validate), and an expense claim line
// with neither a receipt nor a reason (BeforeSubmit).
func TestCheckListsWhatSubmitRefuses_ModuleRules(t *testing.T) {
	cases := []struct {
		name, want string
		edit       func(values, line map[string]string)
		prep       func(t *testing.T, e *rsEnv, id int64)
	}{
		{name: purchase.DocType, want: "Set the state on the supplier's address so GST can be worked out.",
			prep: func(t *testing.T, e *rsEnv, id int64) {
				t.Helper()
				for _, q := range []string{
					`UPDATE address SET gstin = '', state_code = NULL WHERE party_id = (SELECT vendor_id FROM purchase_order WHERE id = ?)`,
					`UPDATE purchase_order SET snap_vendor_state = '' WHERE id = ?`,
				} {
					if _, err := e.w.app.DB().SQL().ExecContext(t.Context(), q, id); err != nil {
						t.Fatal(err)
					}
				}
			}},
		{name: expense.ClaimDocType, want: "Line 1: Add a receipt or say why there isn't one.",
			edit: func(_, line map[string]string) { delete(line, "no_receipt_reason") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := newRSEnv(t).doc[c.name]
			values, line := d.valid(t)
			if c.edit != nil {
				c.edit(values, line)
			}
			if refused := csCompare(t, d, values, line, c.prep); !slices.Contains(refused, c.want) {
				t.Errorf("Submit refused %q, want %q among them", refused, c.want)
			}
		})
	}
}

// csCompare saves values and line as a draft, runs prep on it (nil: none),
// then checks that every refusal Submit gives it is listed on its Check
// page; it returns Submit's refusals, at least one.
func csCompare(t *testing.T, d *rsDoc, values, line map[string]string, prep func(t *testing.T, e *rsEnv, id int64)) []string {
	t.Helper()
	e := d.e
	var lines []map[string]string
	if d.dt.LinesTable != "" {
		lines = []map[string]string{maps.Clone(line)}
	}
	res, err := e.w.app.Engine().Do(t.Context(), e.who[rsAdmin], d.dt.Name, 0, doc.ActionSave, doc.ActionInput{Values: values, Lines: lines})
	if err != nil {
		pr, _ := docscreen.Classify(err)
		t.Fatalf("saving the draft: %v %v", err, pr.Fields)
	}
	if prep != nil {
		prep(t, e, res.ID)
	}
	check := e.do(t, "plain"+rsShow, rsAdmin, http.MethodGet, d.stepURL(res.ID, len(d.steps)-1), nil, false).want(t, "GET check", http.StatusOK)
	listed := check.page.Find("main li").Map(func(_ int, s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") })
	_, err = e.w.app.Engine().Do(t.Context(), e.who[rsAdmin], d.dt.Name, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err == nil {
		t.Fatal("Submit accepted the draft: nothing to compare")
	}
	pr, ok := docscreen.Classify(err)
	if !ok {
		t.Fatalf("Submit: %v", err)
	}
	var refused []string
	for _, f := range pr.Fields {
		refused = append(refused, f.Message)
	}
	if len(pr.Fields) == 0 {
		refused = append(refused, pr.Message)
	}
	for _, m := range refused {
		if !slices.Contains(listed, m) {
			t.Errorf("Submit refuses %q, which Check doesn't list (Check lists %q)", m, listed)
		}
	}
	return refused
}
