package app_test

import (
	"net/http"
	"testing"
)

// No case ID: UI v2 C1b, §5.5 item 3 on the setup company form. PAN goes in a
// closed More details at the end of the form, opened by a typed PAN or an
// error on it. State code is required, so it stays outside.
func TestApp_SetupCompanyMoreDetails(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	r := b.get("/setup/company")
	if r.status != http.StatusOK {
		t.Fatalf("GET /setup/company = %d, want 200", r.status)
	}
	d := r.doc.Find("form details.disclosure")
	if d.Length() != 1 || d.Find(`[name="pan"]`).Length() != 1 {
		t.Fatalf("%d More details groups; PAN inside = %d", d.Length(), d.Find(`[name="pan"]`).Length())
	}
	if _, open := d.Attr("open"); open {
		t.Error("More details is open with nothing typed")
	}
	if d.Find(`[name="state_code"]`).Length() != 0 || r.doc.Find(`[name="state_code"]`).Length() != 1 {
		t.Error("State code is inside More details")
	}
	if d.NextAll().Find("input").Length() != 0 {
		t.Error("an input comes after More details")
	}
	// Each post is refused, so the form comes back: a blank legal name, a PAN
	// that isn't one, or no PAN and no GSTIN to take it from.
	for name, c := range map[string]struct {
		legal, gstin, pan string
		open              bool
	}{
		"refused elsewhere, no pan": {"", "27AAFCV2449G1Z7", "", false},
		"refused elsewhere, pan":    {"", "27AAFCV2449G1Z7", "AAFCV2449G", true},
		"error on typed pan":        {"Vardhan Infra Projects Pvt Ltd", "27AAFCV2449G1Z7", "ABC", true},
		"error on blank pan":        {"Vardhan Infra Projects Pvt Ltd", "", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			form := goodSetup()
			form.Set("legal_name", c.legal)
			form.Set("gstin", c.gstin)
			form.Set("pan", c.pan)
			r := b.post("/setup/company", form)
			if r.status != http.StatusUnprocessableEntity {
				t.Fatalf("POST = %d, want 422", r.status)
			}
			if c.legal != "" && r.doc.Find("#f-pan-msg").Text() == "" {
				t.Fatal("no error on PAN")
			}
			if _, open := r.doc.Find("form details.disclosure").Attr("open"); open != c.open {
				t.Errorf("open = %v; want %v (%s)", open, c.open, r.pageText())
			}
		})
	}
}
