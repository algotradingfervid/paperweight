package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	internalText    = "Something went wrong. Try again, and if it keeps happening tell the administrator."
	crossOriginText = "This request came from another site, so it was blocked. Go back to the page in this app and try again."
)

// statusError is any error that carries its own HTTP status; doctype.HTTPError
// satisfies it, so kernel errors map without importing doctype (CD-90).
type statusError interface {
	error
	Status() int
}

// ErrorFrom writes the response for err: an error with a Status method gives
// that status and its message, db.ErrBusy gives 503 with its text (CD-76), and
// anything else gives the 500 page.
func ErrorFrom(w http.ResponseWriter, r *http.Request, err error) {
	var se statusError
	switch {
	case errors.As(err, &se):
		Error(w, r, se.Status(), se.Error())
	case errors.Is(err, db.ErrBusy):
		Error(w, r, http.StatusServiceUnavailable, db.ErrBusy.Error())
	default:
		loggerFrom(r.Context()).ErrorContext(r.Context(), "unhandled error", "path", r.URL.Path, "err", err)
		Error(w, r, http.StatusInternalServerError, internalText)
	}
}

var errorPage = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Error</title></head>
<body>
<div role="alert">{{.}}</div>
</body>
</html>
`))

// Error writes an error response; errors are never toasts (CD-47).
//   - Full-page request: a plain HTML page with the message in a role="alert" div.
//   - htmx request with 409 or 422: an escaped role="alert" fragment, which the
//     UI's htmx config swaps.
//   - Any other htmx request: an empty body and an HX-Trigger "notice" event,
//     which the UI shows in its persistent #alerts region.
func Error(w http.ResponseWriter, r *http.Request, status int, message string) {
	h := w.Header()
	if r.Header.Get("HX-Request") != "true" {
		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_ = errorPage.Execute(w, message)
		return
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`<div class="error" role="alert">` + template.HTMLEscapeString(message) + `</div>`))
		return
	}
	SetNotice(h, "error", message)
	w.WriteHeader(status)
}

// SetNotice sets h's HX-Trigger to a "notice" event with level and text,
// replacing any earlier HX-Trigger; the UI shows it in its #alerts region.
func SetNotice(h http.Header, level, text string) {
	trigger, _ := json.Marshal(map[string]Notice{"notice": {Level: level, Text: text}})
	h.Set("HX-Trigger", asciiJSON(trigger))
}

// asciiJSON escapes every non-ASCII rune in b as \uXXXX (a surrogate pair above
// U+FFFF). Browsers read header bytes as Latin-1, so a raw UTF-8 header would
// reach htmx garbled; JSON.parse of the escaped form gives the exact text.
func asciiJSON(b []byte) string {
	var sb strings.Builder
	for _, r := range string(b) {
		if r <= 0x7F {
			sb.WriteRune(r)
			continue
		}
		if r1, r2 := utf16.EncodeRune(r); r1 != utf8.RuneError {
			fmt.Fprintf(&sb, `\u%04x\u%04x`, r1, r2)
			continue
		}
		fmt.Fprintf(&sb, `\u%04x`, r)
	}
	return sb.String()
}
