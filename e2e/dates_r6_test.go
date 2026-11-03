//go:build e2e

package e2e

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// r6Midnight is just after IST midnight: 25 October in India, still 24
// October in UTC. Its echoes are the two days' weekdays.
var r6Midnight = time.Date(2026, 10, 24, 18, 31, 0, 0, time.UTC)

const (
	r6Today     = "25-10-2026"
	r6Yesterday = "24-10-2026"
)

// r6MasterForms are the pages holding the masters' date fields.
var r6MasterForms = map[string]string{employee.DocType: "/m/employee/new", project.DocType: "/m/project/new"}

// TestE2E_MasterDateChips (shell review H1, A-15): on every date field of
// every master, Today and Yesterday write their day and the server echoes it,
// and the chip shows as pressed. A master that gains a date field fails until
// it is listed here.
func TestE2E_MasterDateChips(t *testing.T) {
	h := Start(t)
	h.Clock.Set(r6Midnight)
	s := h.SignIn(admin, phone390)
	fields := 0
	for _, dt := range doctype.All() {
		if !dt.IsMaster() {
			continue
		}
		var dates []string
		for _, f := range dt.Fields {
			if f.Type == doctype.Date {
				dates = append(dates, f.Name)
			}
		}
		if len(dates) == 0 {
			continue
		}
		form, ok := r6MasterForms[dt.Name]
		if !ok {
			t.Errorf("master %s has date fields %v and no form here", dt.Name, dates)
			continue
		}
		s.Open(form)
		for _, name := range dates {
			fields++
			id := components.FieldID(name)
			box := `document.getElementById("` + id + `")`
			s.Until(name+" on "+form, `!!`+box)
			Eval[bool](s, box+`.closest("details")?.setAttribute("open", ""), true`)
			for _, c := range []struct{ label, day, echo string }{
				{"Today", r6Today, "Sunday, 25 October 2026"},
				{"Yesterday", r6Yesterday, "Saturday, 24 October 2026"},
			} {
				chip := `[...document.querySelectorAll('[data-set-date][aria-controls="` + id + `"]')].find((b) => b.textContent.trim() === "` + c.label + `")`
				s.Until(c.label+" chip on "+name, `!!`+chip)
				s.Do("press "+c.label+" on "+name, chromedp.Evaluate[chromedp.Void](`(`+chip+`).click()`))
				if got := Eval[string](s, box+`.value`); got != c.day {
					t.Errorf("%s: %s chip wrote %q, want %s", name, c.label, got, c.day)
				}
				if got := Eval[string](s, chip+`.getAttribute("aria-pressed")`); got != "true" {
					t.Errorf("%s: %s chip aria-pressed = %q after the tap", name, c.label, got)
				}
				s.Until(name+" echo "+c.echo, `document.getElementById("`+id+`-msg").textContent.trim() === `+strconv.Quote(c.echo))
			}
		}
	}
	if fields < 5 {
		t.Errorf("checked %d master date fields, want the 5 of Employee and Project at least", fields)
	}
}

// TestE2E_NewDocumentDatedToday (PO spec §3.2, expense UX §5.10): a new order, and
// a new advance, open on today's IST date from the app's clock, and the date
// saves as it shows.
func TestE2E_NewDocumentDatedToday(t *testing.T) {
	h := Start(t)
	h.Clock.Set(r6Midnight)
	box := func(name string) string { return `document.getElementById("` + components.FieldID(name) + `").value` }

	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + purchase.DocType + "/new")
	if got := Eval[string](p, box("order_date")); got != r6Today {
		t.Errorf("new order's Order date = %q, want %s", got, r6Today)
	}

	s := h.SignIn(suresh, phone390)
	s.Open("/doc/" + goodsreceipt.DocType + "/new")
	if got := Eval[string](s, `document.querySelector('[data-date-picker="`+components.FieldID("receipt_date")+`"]')?.max ?? "no date"`); got != "2026-10-25" {
		t.Errorf("receipt date's calendar max = %q, want 2026-10-25", got)
	}

	d := h.SignIn(deepak, phone390)
	d.Open("/doc/" + expense.AdvanceDocType + "/new")
	if got := Eval[string](d, box("advance_date")); got != r6Today {
		t.Errorf("new advance's date = %q, want %s", got, r6Today)
	}
	d.Fill("purpose", "Nagpur site visit")
	d.Press(saveContinue)
	d.Until("the draft", `/^\/doc\/[^/]+\/[0-9]+/.test(location.pathname)`)
	if got := h.load(expense.AdvanceDocType, d.DocID()).Values["advance_date"]; got != "2026-10-25" {
		t.Errorf("stored advance_date = %v, want 2026-10-25", got)
	}
}

// r6Tap is a finger's tap on the input find finds, in its left third, where
// the review's 10 became 100. The tap gesture refuses a point past headless
// Chrome's 600 px window, whatever height is emulated, so the page gets a
// viewport the input can centre in.
func r6Tap(s *Session, find string) {
	s.t.Helper()
	s.Do("a short phone", chromedp.EmulateViewport(s.width, 560, chromedp.EmulateMobile, chromedp.EmulateTouch))
	at := Eval[[]float64](s, `(() => { const e = `+find+`; e.scrollIntoView({block: "center", behavior: "instant"}); const r = e.getBoundingClientRect(); return [r.left + r.width / 3, r.top + r.height / 2]; })()`)
	s.Do("tap", chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, input.SynthesizeTapGesture, input.SynthesizeTapGestureParams{X: at[0], Y: at[1]})
		return err
	}))
}

// r6Replace taps the prefilled quantity find finds, types 10 and checks the
// digits replaced the old value (GRN review H1).
func r6Replace(s *Session, what, find string) {
	s.t.Helper()
	s.Until(what, `!!`+find)
	r6Tap(s, find)
	s.Until(what+" selected", `(() => { const e = `+find+`; return document.activeElement === e && e.value !== "" && e.selectionStart === 0 && e.selectionEnd === e.value.length; })()`)
	s.Keys("10")
	if got := Eval[string](s, find+`.value`); got != "10" {
		s.t.Errorf("%s: typing 10 over the value gave %q", what, got)
	}
}

// TestE2E_TapSelectsEveryQuantity (CD-387 (8)): at each phone width with touch
// emulation, a tap on a prefilled field selects it, on the order line's
// Quantity and Price, the prefilled Order date, the request line, and Received
// now and Rejected on Quantities; Rejected starts blank with its placeholder. already_received draws through the same
// docscreen input as the line's Quantity. An editable Qty field not named here
// fails, so a new one can't miss this.
func TestE2E_TapSelectsEveryQuantity(t *testing.T) {
	typed := []string{"qty", "already_received", "received_qty", "rejected_qty"}
	for _, name := range []string{purchase.DocType, purchase.RequestDocType, goodsreceipt.DocType, expense.ClaimDocType, expense.AdvanceDocType} {
		dt, _ := doctype.Lookup(name)
		for _, f := range dt.Fields {
			if f.Type == doctype.Qty && !f.ReadOnly && !f.System && !slices.Contains(typed, f.Name) {
				t.Errorf("quantity field %s %s isn't in this test", name, f.Name)
			}
		}
	}
	for _, w := range []int64{phone320, phone390} {
		t.Run(strconv.FormatInt(w, 10), func(t *testing.T) {
			h := Start(t)
			po := h.metroDraft(priya)
			p := h.SignIn(priya, w)
			qty := `document.getElementById("` + components.FieldID("qty") + `")`
			p.Open(DocPath(purchase.DocType, po) + "/lines/1")
			r6Replace(p, "the order line's Quantity", qty)
			r6Replace(p, "the order line's Price", `document.getElementById("`+components.FieldID("price")+`")`)
			p.Open("/doc/" + purchase.DocType + "/new")
			date := `document.getElementById("` + components.FieldID("order_date") + `")`
			r6Tap(p, date)
			p.Until("Order date selected", `(() => { const e = `+date+`; return e.value !== "" && e.selectionEnd - e.selectionStart === e.value.length; })()`)
			p.Keys("05112026")
			if got := Eval[string](p, date+`.value`); got != "05-11-2026" {
				t.Errorf("typing over the prefilled date gave %q", got)
			}

			r := h.Do(priya, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
				Values: map[string]string{"site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "request_date": plainDate},
				Lines:  []map[string]string{{"item_id": h.idOf("item", "BOLT-M12"), "uom_id": h.idOf("uom", "NOS"), "qty": "25"}},
			})
			p.Open(DocPath(purchase.RequestDocType, r.ID) + "/lines/1")
			r6Replace(p, "the request line's Quantity", qty)

			h.Do(priya, purchase.DocType, po, doctype.ActionSubmit, doc.ActionInput{Version: h.load(purchase.DocType, po).Version})
			s := h.SignIn(suresh, w)
			s.Open("/doc/" + goodsreceipt.DocType)
			startGRN(s)
			grnDelivery(s, grnChallan)
			const item = "Binding Wire 18G"
			rejected, received := cardInput("Rejected, line 1, "+item), cardInput("Received now, line 1, "+item)
			if got := Eval[string](s, rejected+`.value + "|" + `+rejected+`.placeholder`); got != "|e.g. 10" {
				t.Errorf("Rejected = %q, want blank with its placeholder", got)
			}
			r6Replace(s, "Received now", received)
			rejectOn(s, 1, item, "1", grnRejectReason)
			r6Replace(s, "Rejected", rejected)
		})
	}
}
