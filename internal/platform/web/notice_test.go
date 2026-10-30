package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/web"
)

// Design 2026-10-31-document-attachments §6 change 3 (CD-241): one exported
// helper writes the htmx notice trigger, with the ASCII escaping web.Error uses.
func TestWeb_SetNoticeExactJSON(t *testing.T) {
	// A quote, a BMP rune (₹, U+20B9) and one above U+FFFF (😀, a surrogate pair).
	const text = "Amount \"₹5,000\" is over the limit 😀"
	const want = `{"notice":{"level":"warning","text":"Amount \"\u20b95,000\" is over the limit \ud83d\ude00"}}`

	h := http.Header{}
	web.SetNotice(h, "warning", text)

	if got := h.Values("HX-Trigger"); len(got) != 1 || got[0] != want {
		t.Errorf("HX-Trigger = %q, want exactly [%q]", got, want)
	}
}

// SetNotice replaces an earlier HX-Trigger rather than adding a second one.
func TestWeb_SetNoticeReplacesTrigger(t *testing.T) {
	h := http.Header{}
	h.Set("HX-Trigger", "stale")
	web.SetNotice(h, "error", "File not found.")
	const want = `{"notice":{"level":"error","text":"File not found."}}`
	if got := h.Values("HX-Trigger"); len(got) != 1 || got[0] != want {
		t.Errorf("HX-Trigger = %q, want exactly [%q]", got, want)
	}
}

// web.Error's htmx notice is the one SetNotice writes (web.Error calls it), for
// a text with a quote, HTML characters and non-ASCII runes.
func TestWeb_ErrorNoticeMatchesSetNotice(t *testing.T) {
	for _, text := range []string{
		"File type .रसीद is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV.",
		`Say "yes" <now> & ₹5,000 😀`,
	} {
		rec := httptest.NewRecorder()
		web.Error(rec, htmxRequest(), http.StatusForbidden, text)

		want := http.Header{}
		web.SetNotice(want, "error", text)

		if got, w := rec.Header().Get("HX-Trigger"), want.Get("HX-Trigger"); got != w || w == "" {
			t.Errorf("text %q: web.Error HX-Trigger = %q, SetNotice gives %q; want equal and non-empty", text, got, w)
		}
	}
}
