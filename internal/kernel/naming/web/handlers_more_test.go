package namingweb

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// exhaustGRN makes the GRN series GR/{FY}/{###} with GR/26-27/ at 999, so
// today's next number does not exist.
func (e *screenEnv) exhaustGRN() {
	e.t.Helper()
	ctx := context.Background()
	if err := e.nm.SetTemplate(ctx, testAnil, testGRN, "GR/{FY}/{###}"); err != nil {
		e.t.Fatalf("SetTemplate(anil, GRN, GR/{FY}/{###}): %v", err)
	}
	if err := e.nm.SetCounter(ctx, testAnil, testGRN, "GR/26-27/", 999); err != nil {
		e.t.Fatalf("SetCounter(anil, GRN, GR/26-27/, 999): %v", err)
	}
}

const msgExhaustedGRN = "Series GR/26-27/ has used all 999 numbers. Ask an Administrator to add a digit to the series."

func TestNumberingScreen_Exhausted(t *testing.T) {
	t.Run("list_shows_message", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		e.exhaustGRN()
		r := e.get("anil", "/settings/numbering")
		wantStatus(t, "admin GET /settings/numbering (GRN exhausted)", r, http.StatusOK)
		row := r.doc.Find(`a[href="` + testEditPath(testGRN) + `"]`).First().Closest("li")
		const want = "GRN · GR/{FY}/{###} · Next: " + msgExhaustedGRN
		if got := testNorm(row.Text()); got != want {
			t.Errorf("GRN row text %q, want %q", got, want)
		}
		if txt := testPageText(r); !strings.Contains(txt, "Next: PO/26-27/00001") {
			t.Errorf("other rows missing; page text %q", txt)
		}
	})

	t.Run("edit_prefix_blank", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		e.exhaustGRN()
		r := e.get("anil", testEditPath(testGRN))
		wantStatus(t, "admin GET GRN edit page (exhausted)", r, http.StatusOK)
		in := r.doc.Find(`input[name="prefix"]`)
		if in.Length() != 1 {
			t.Fatalf("%d prefix inputs, want 1", in.Length())
		}
		if got := in.AttrOr("value", ""); got != "" {
			t.Errorf("Prefix field value = %q, want blank", got)
		}
	})
}

// After a refused series the counter form's Prefix is today's prefix, as on
// a plain GET.
func TestNumberingScreen_TemplateInvalidKeepsPrefix(t *testing.T) {
	e := newScreenEnv(t, testNow)
	r := e.post("anil", testEditPath(testPO)+"/template", url.Values{"template": {"PO/{FY}/{##}"}, "csrf_token": {testCSRF}})
	wantStatus(t, "admin POST template PO/{FY}/{##}", r, http.StatusUnprocessableEntity)
	if got, _ := r.doc.Find(`input[name="prefix"]`).Attr("value"); got != "PO/26-27/" {
		t.Errorf("Prefix field value = %q, want today's prefix %q", got, "PO/26-27/")
	}
}
