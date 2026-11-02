package mstweb_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
)

// UIV2-C3b (UI v2 §5.6): every create wizard step's × goes back to the
// master's list, through "Leave without saving?", on the first step, a later
// one and Check, in focus mode.
func TestWizardClose_Master(t *testing.T) {
	h := wizardHarness(t, func(context.Context, auth.Principal, stubDraft) (mstkit.Saved, error) { return mstkit.Saved{}, nil })
	b := h.Browser(h.Env.Kavya)
	toWizardCheck(t, b)
	for _, target := range []string{wizardFirst, base + "/new/more", wizardCheck} {
		r := b.Get(t, target)
		webtest.WantStatus(t, r, http.StatusOK)
		x := r.Doc.Find(".wizard-head > a.wizard-close")
		if x.Length() != 1 || x.AttrOr("href", "") != base || x.AttrOr("data-open-dialog", "") != "leave-wizard" {
			t.Errorf("%s: ×: %d, href %q, opens %q; want %s through leave-wizard", target, x.Length(), x.AttrOr("href", ""), x.AttrOr("data-open-dialog", ""), base)
		}
		if r.Doc.Find("dialog#leave-wizard a[href='"+base+"']").Length() != 1 {
			t.Errorf("%s: no Leave link to %s in the dialog", target, base)
		}
		if r.Doc.Find("body.focus-mode").Length() != 1 {
			t.Errorf("%s: not in focus mode", target)
		}
	}
}
