//go:build e2e

package gallery

import (
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// TestFieldMsgBrowser_IconHoldsOneLineWhileSettling (CD-287, HOOK-049): while
// an htmx swap settles, the swapped-in message wears the old message's class,
// often plain field__msg. A date echo with its check icon then still fills
// exactly the one reserved line, as it does with its own echo class and with
// the error class, so nothing below it moves under a pressed Save.
func TestFieldMsgBrowser_IconHoldsOneLineWhileSettling(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	const msg = `document.getElementById("f-date-msg")`
	for _, w := range []int64{320, 360, 390} {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
			t.Fatal(err)
		}
		if !value[bool](t, ctx, msg+`.querySelector(":scope > svg.icon") !== null && `+msg+`.textContent.trim() === "Saturday, 24 October 2026"`) {
			t.Fatalf("at %d px: the date's message doesn't hold the check icon and the echo", w)
		}
		line := value[float64](t, ctx, `parseFloat(getComputedStyle(`+msg+`).lineHeight)`)
		for _, class := range []string{"field__msg", "field__msg field__msg--echo", "field__msg field__msg--error"} {
			h := value[float64](t, ctx, `(() => { const m = `+msg+`; const c = m.className; m.className = "`+class+`"; const h = m.getBoundingClientRect().height; m.className = c; return h; })()`)
			if !samePx(h, line) {
				t.Errorf("at %d px with class %q: the echo with its icon is %.1f px tall, want one line (%.1f px)", w, class, h, line)
			}
		}
	}
}

// samePx reports whether two lengths are within 0.01 px.
func samePx(a, b float64) bool { return a-b < 0.01 && b-a < 0.01 }
