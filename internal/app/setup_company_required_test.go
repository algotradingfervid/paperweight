package app_test

import (
	"net/http"
	"strings"
	"testing"
)

// No case ID: UI v2 B1b, A-9 on the setup wizard's two input steps. Legal
// name, Head office code, Address line 1 and City are required (SetupTx and
// the address refuse them blank); GSTIN, State code and PAN (each needed only
// without the other, CD-165, UI v2 §5.4), Head office name and Pincode are
// optional. Each required control has aria-required and its label a hidden
// "(required)"; no label says "(optional)". Each step starts with the
// required note.
func TestApp_SetupCompanyMarksRequired(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	b.toCheck(goodSetup())
	for i, path := range []string{setupStep1, setupStep2} {
		r := b.get(path)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, r.status)
		}
		if !strings.Contains(strings.Join(strings.Fields(r.doc.Find("form").Text()), " "), "Fields marked * are required.") {
			t.Errorf("%s doesn't start with the required note", path)
		}
		for _, name := range setupStepFields[i] {
			required := map[string]bool{"legal_name": true, "branch_code": true, "address_line1": true, "address_city": true}[name]
			aria := r.doc.Find(`[name="`+name+`"][aria-required="true"]`).Length() == 1
			label := r.doc.Find(`label[for="f-` + name + `"]`).Text()
			if aria != required || strings.Contains(label, "(required)") != required {
				t.Errorf("%s: aria-required %v, label %q; want required = %v", name, aria, label, required)
			}
			if strings.Contains(label, "(optional)") {
				t.Errorf("%s: label %q says (optional)", name, label)
			}
		}
	}
}
