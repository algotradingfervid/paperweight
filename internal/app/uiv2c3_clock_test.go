package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/platform/clock"
)

// UI v2 C3 (§5.6): the app gives wizard.Respond its own clock, so a step
// save's HX-Trigger saved names the app clock's time in IST. 18:31 UTC is
// 00:01 IST.
func TestWizardSave_CueUsesAppClock(t *testing.T) {
	w := newWorld(t, func(c *app.Config) {
		c.Clock = clock.NewFixed(time.Date(2026, 10, 24, 18, 31, 0, 0, time.UTC))
	})
	w.seedUsers()
	b := w.signedIn(adminEmail)
	r := b.postHeaders(setupStep1, stepForm(goodSetup(), 0), map[string]string{"HX-Request": "true"})
	if r.status != http.StatusOK {
		t.Fatalf("htmx Continue = %d (%q), want 200", r.status, r.pageText())
	}
	if got, want := r.header.Get("HX-Trigger"), `{"saved":"Saved 00:01"}`; got != want {
		t.Errorf("HX-Trigger = %q, want %q", got, want)
	}
}
