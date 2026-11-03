package companyweb_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// UI v2 R10 (shell H5): Branch State is a Select of the states a branch can be
// in, by name, and a valid GSTIN gives it: the input names the Select it fills,
// and a save with no state takes the GSTIN's.
func TestBranchState_SelectOfStates(t *testing.T) {
	w := newWorld(t)
	r := w.h.Get(t, w.h.Env.Kavya, "/m/branch/new")
	webtest.WantStatus(t, r, http.StatusOK)
	if r.Doc.Find(`select[name="state_code"]`).Length() != 1 {
		t.Fatalf("State is not a Select\nbody: %s", webtest.Squash(r.Body))
	}
	labels := map[string]string{}
	for _, c := range r.Choices("state_code") {
		labels[c.Value] = c.Label
	}
	if labels["27"] != "Maharashtra (27)" {
		t.Errorf("option 27 = %q", labels["27"])
	}
	if label, ok := labels["96"]; ok {
		t.Errorf("Other Countries is offered: %q; no branch can be there", label)
	}
	if got := r.Doc.Find(`input[name="gstin"]`).AttrOr("data-fills", ""); got != "f-state_code" {
		t.Errorf("GSTIN data-fills = %q; want the State Select", got)
	}
	gstin := r.Doc.Find("#f-gstin")
	if gstin.Closest("section.card").Find("#f-state_code").Length() != 1 {
		t.Error("State isn't in GSTIN's card")
	}
}

func branchForm(state, gstin string) url.Values {
	return url.Values{"code": {"NASIK"}, "name": {"Nashik Store"}, "state_code": {state}, "gstin": {gstin}}
}

func TestBranchState_FromGSTIN(t *testing.T) {
	w := newWorld(t)
	const mumbai = "27AAFCV2449G2Z6"
	r := w.h.Post(t, w.h.Env.Kavya, "/m/branch/new", branchForm("", mumbai))
	webtest.WantStatus(t, r, http.StatusSeeOther)
	if b := w.mustBranch(t, "NASIK"); b.StateCode != "27" {
		t.Errorf("state = %q; want the GSTIN's 27", b.StateCode)
	}
}

// A state that isn't in the Select is refused, with the same words as none,
// which fit a Select; the form keeps its Select.
func TestBranchState_UnknownRefused(t *testing.T) {
	for _, state := range []string{"", "Maharashtra", "96", "99"} {
		t.Run(state, func(t *testing.T) {
			w := newWorld(t)
			r := w.h.Post(t, w.h.Env.Kavya, "/m/branch/new", branchForm(state, ""))
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldError(t, r, "state_code", msgState)
			if _, ok := w.branch(t, "NASIK"); ok {
				t.Error("the branch was saved")
			}
		})
	}
}

// A GSTIN for another state than the one chosen stays refused on GSTIN.
func TestBranchState_MismatchRefused(t *testing.T) {
	w := newWorld(t)
	r := w.h.Post(t, w.h.Env.Kavya, "/m/branch/new", branchForm("29", "27AAFCV2449G2Z6"))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "gstin", "GSTIN 27AAFCV2449G2Z6 is for Maharashtra (27) but the branch state is Karnataka (29). Fix the GSTIN or the state.")
}
