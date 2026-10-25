package namingweb

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/naming"
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

// CD-164 (1): a prefix that doesn't match the series is a field error on the
// counter form's Prefix, not an error page.
func TestNumberingScreen_CounterPrefixMismatch(t *testing.T) {
	e := newScreenEnv(t, testNow)
	e.presetPO(7)
	const call = "admin POST counter prefix=PUR/26-27/"
	r := e.post("anil", testEditPath(testPO)+"/counter", url.Values{"prefix": {"PUR/26-27/"}, "last": {"12"}, "csrf_token": {testCSRF}})
	wantStatus(t, call, r, http.StatusUnprocessableEntity)
	wantFieldError(t, call, r, "prefix", "Prefix PUR/26-27/ doesn't match series PO/{FY}/{#####}.", "PUR/26-27/")
	in := r.doc.Find(`input[name="prefix"]`)
	if got := in.AttrOr("aria-invalid", ""); got != "true" {
		t.Errorf("%s: Prefix aria-invalid = %q, want \"true\"", call, got)
	}
	if got := r.doc.Find(`input[name="last"]`).AttrOr("value", ""); got != "12" {
		t.Errorf("%s: Last number used value = %q, want the posted %q", call, got, "12")
	}
	if got := e.series(testPO).Counters["PO/26-27/"]; got != 7 {
		t.Errorf("PO/26-27/ counter = %d, want unchanged 7", got)
	}
}

// CD-164 (2): every route's guard refuses with naming.MsgForbidden.
func TestNumberingScreen_GuardUsesMsgForbidden(t *testing.T) {
	var msgs []string
	guard := Guard(func(msg string, h http.Handler) http.Handler {
		msgs = append(msgs, msg)
		return h
	})
	Mount(chi.NewRouter(), Deps{}, guard)
	if len(msgs) == 0 {
		t.Fatal("Mount registered no guarded routes")
	}
	for i, m := range msgs {
		if m != naming.MsgForbidden {
			t.Errorf("route %d guard message %q, want naming.MsgForbidden %q", i, m, naming.MsgForbidden)
		}
	}
}

// CD-164 (4): the edit page's preview region is a polite live region right
// after the "Try a series" field, and that input is not marked invalid.
func TestNumberingScreen_PreviewLiveRegion(t *testing.T) {
	e := newScreenEnv(t, testNow)
	r := e.get("anil", testEditPath(testPO))
	wantStatus(t, "admin GET edit page", r, http.StatusOK)
	in := r.doc.Find(`input[name="tpl"]`)
	if in.Length() != 1 {
		t.Fatalf("%d \"Try a series\" inputs, want 1", in.Length())
	}
	if got := in.AttrOr("hx-target", ""); got != "#numbering-preview" {
		t.Errorf("Try a series hx-target = %q, want %q", got, "#numbering-preview")
	}
	if v, ok := in.Attr("aria-invalid"); ok {
		t.Errorf("Try a series aria-invalid = %q on GET, want none", v)
	}
	region := in.Closest(".field").Next()
	if got := region.AttrOr("id", ""); got != "numbering-preview" {
		t.Errorf("element after the Try a series field has id %q, want the preview region", got)
	}
	if got := region.AttrOr("aria-live", ""); got != "polite" {
		t.Errorf("preview region aria-live = %q, want \"polite\"", got)
	}
}

// CD-164 (4): a refused preview answers only the message for the live
// region; it never redraws or marks an input.
func TestNumberingScreen_PreviewErrorFragment(t *testing.T) {
	e := newScreenEnv(t, testNow)
	path := "/settings/numbering/preview?" + url.Values{"tpl": {"PO {FY}/{##}"}}.Encode()
	r := e.do("anil", http.MethodGet, path, nil, true)
	wantStatus(t, "GET preview PO {FY}/{##}", r, http.StatusUnprocessableEntity)
	const msg = "Series can use only letters, digits, '-' and '/'."
	if got := testPageText(r); got != msg {
		t.Errorf("preview error fragment text %q, want exactly %q", got, msg)
	}
	if n := r.doc.Find("input, textarea, select, form").Length(); n != 0 {
		t.Errorf("preview error fragment has %d form controls, want 0 (it must not redraw the input)", n)
	}
	if n := r.doc.Find("[aria-invalid], [role=alert]").Length(); n != 0 {
		t.Errorf("preview error fragment has %d aria-invalid/role=alert elements, want 0", n)
	}
}

// CD-164 (4): aria-invalid comes from a save attempt: a refused series marks
// the Series input.
func TestNumberingScreen_TemplateInvalidMarksInput(t *testing.T) {
	e := newScreenEnv(t, testNow)
	r := e.post("anil", testEditPath(testPO)+"/template", url.Values{"template": {"PO/{FY}/{##}"}, "csrf_token": {testCSRF}})
	wantStatus(t, "admin POST template PO/{FY}/{##}", r, http.StatusUnprocessableEntity)
	if got := r.doc.Find(`input[name="template"]`).AttrOr("aria-invalid", ""); got != "true" {
		t.Errorf("Series aria-invalid = %q after a refused save, want \"true\"", got)
	}
}
