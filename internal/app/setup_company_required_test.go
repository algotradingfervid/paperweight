package app_test

import (
	"net/http"
	"strings"
	"testing"
)

// No case ID: UI v2 B1b, A-9 on the setup company form. Legal name, State
// code and Head office code are required (SetupTx refuses them blank); GSTIN,
// PAN and Head office name are optional. Each required control has
// aria-required and its label a hidden "(required)"; no label says
// "(optional)". The form starts with the required note.
func TestApp_SetupCompanyMarksRequired(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	r := w.signedIn(adminEmail).get("/setup/company")
	if r.status != http.StatusOK {
		t.Fatalf("GET /setup/company = %d, want 200", r.status)
	}
	if !strings.Contains(strings.Join(strings.Fields(r.doc.Find("form").Text()), " "), "Fields marked * are required.") {
		t.Error("the form doesn't start with the required note")
	}
	for name, required := range map[string]bool{
		"legal_name": true, "state_code": true, "branch_code": true,
		"gstin": false, "pan": false, "branch_name": false,
	} {
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
