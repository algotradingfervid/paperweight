package docscreen_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// reasonPages are the reject, cancel and close pages (design-system §8.5).
var reasonPages = []struct {
	action string
	state  doctype.State
	label  string
	maxLen int
	button string
	danger bool
}{
	{"reject", doctype.StatePending, "Reason for rejecting", 1000, "Reject and send back", false},
	{"cancel", doctype.StateApproved, "Reason for cancelling", 500, "Cancel sample order", true},
	{"close", doctype.StateApproved, "Reason for closing", 500, "Close sample order", false},
}

func TestReasonPage_EmptyReason422(t *testing.T) {
	for _, p := range reasonPages {
		t.Run(p.action, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(p.state)

			get := e.get(t, doc42+"/"+p.action)
			if get.Code != http.StatusOK {
				t.Fatalf("GET status = %d, body %.300s", get.Code, get.Body)
			}

			rec := e.post(t, doc42+"/"+p.action, url.Values{"reason": {"  \n\t "}})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			if calls := e.docs.Calls(); len(calls) != 0 {
				t.Errorf("engine called with an empty reason: %+v", calls)
			}
			page := uitest.Doc(t, rec)
			area := page.Find(`textarea[name="reason"]`)
			if area.Length() != 1 {
				t.Fatal("no reason textarea")
			}
			id, _ := area.Attr("id")
			if got := norm(page.Find(`label[for="` + id + `"]`).Text()); got != p.label {
				t.Errorf("label = %q, want %q", got, p.label)
			}
			if ml, _ := area.Attr("maxlength"); ml != strconv.Itoa(p.maxLen) {
				t.Errorf("maxlength = %q, want %d", ml, p.maxLen)
			}
			if inv, _ := area.Attr("aria-invalid"); inv != "true" {
				t.Error("textarea not marked aria-invalid")
			}
			if got := norm(page.Find("#" + id + "-msg").Text()); got != "Enter a reason." {
				t.Errorf("field error = %q, want \"Enter a reason.\"", got)
			}
			btn := buttonsNamed(page.Selection, p.button)
			if btn.Length() != 1 {
				t.Fatalf("no %q button", p.button)
			}
			if btn.HasClass("btn--danger") != p.danger {
				t.Errorf("%q danger = %v, want %v", p.button, btn.HasClass("btn--danger"), p.danger)
			}
			form := formOf(page, btn)
			if a, _ := form.Attr("action"); a != doc42+"/"+p.action {
				t.Errorf("form action = %q", a)
			}
			if form.Find(`input[name="csrf_token"][value="`+csrfToken+`"]`).Length() != 1 ||
				form.Find(`input[name="version"][value="3"]`).Length() != 1 {
				t.Error("reason form lacks csrf_token or the posted version")
			}
		})
	}
}

func TestReasonPage_PassesReasonTrimmedToEngine(t *testing.T) {
	for _, p := range reasonPages {
		t.Run(p.action, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(p.state)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(p.action), doc.Result{ID: 42, Version: 4}, nil)
			rec := e.post(t, doc42+"/"+p.action, url.Values{"reason": {"  Wrong GSTIN  "}, "version": {"5"}})
			if got := location(t, rec); got != doc42 {
				t.Errorf("303 to %q, want %s", got, doc42)
			}
			calls := e.docs.Calls()
			if len(calls) != 1 {
				t.Fatalf("engine calls = %+v, want 1", calls)
			}
			c := calls[0]
			if c.Action != doctype.Action(p.action) || c.ID != 42 || c.By != priya.ID {
				t.Errorf("call = %+v", c)
			}
			if c.Input.Reason != "Wrong GSTIN" {
				t.Errorf("Reason = %q, want \"Wrong GSTIN\"", c.Input.Reason)
			}
			if c.Input.Version != 5 {
				t.Errorf("Version = %d, want the posted 5 (CD-94)", c.Input.Version)
			}
		})
	}
}
