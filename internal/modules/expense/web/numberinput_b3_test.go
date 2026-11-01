package expenseweb_test

import "testing"

// CD-336 (2), CD-355: live grouping keeps a typed minus only on a signed
// field. Expense has no user-entered signed money field (gst_amount is
// NonNegative, amount Positive), so neither input on the line page carries
// data-signed. That a plain Money field does get it is docscreen's
// TestFieldInput_SignedNumbers.
func TestExpLine_NumberInputsUnsigned(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	form := s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new"))
	for _, name := range []string{"gst_amount", "amount"} {
		in := form.Find(`input[name="` + name + `"]`)
		if in.Length() != 1 {
			t.Fatalf("%d inputs named %s, want 1", in.Length(), name)
		}
		if _, ok := in.Attr("data-signed"); ok {
			t.Errorf("%s has data-signed; it is never negative", name)
		}
	}
}
