package components

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/vardhan/internaltools/internal/types"
)

// ParseFunc checks one typed value: the formatted value to put back in the
// input, an echo to show under it, or the parser's error.
type ParseFunc func(in string) (formatted, echo string, err error)

func numParser[T any](parse func(string) (T, error), format func(T) string) ParseFunc {
	return func(in string) (string, string, error) {
		v, err := parse(in)
		if err != nil {
			return "", "", err
		}
		return format(v), "", nil
	}
}

// NUMParsers returns the number and date parsers for ValidateHandler (CD-48).
// Code kinds (gstin, pan, hsn, pincode) are added by WP3.
func NUMParsers() map[string]ParseFunc {
	return map[string]ParseFunc{
		"money": numParser(types.ParseMoney, types.FormatMoney),
		"qty":   numParser(types.ParseQty, types.FormatQty),
		"rate":  numParser(types.ParseRate, types.FormatRate),
		"percent": numParser(types.ParsePercent, func(p types.Percent) string {
			return strings.TrimSuffix(types.FormatPercent(p), "%")
		}),
		"date": func(in string) (string, string, error) {
			d, err := types.ParseDate(in)
			if err != nil {
				return "", "", err
			}
			echo := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Format("Monday, 2 January 2006")
			return types.FormatDate(d), echo, nil
		},
	}
}

// ValidateHandler serves POST /ui/validate/{kind}. It parses the value of the
// field named by _name and answers with the field's message region for _id,
// plus an HX-Trigger "ui:field" event that field.js uses to write the
// formatted value back. An empty value is not checked here (required fields
// are checked on save). An unknown kind is 404.
func ValidateHandler(parsers map[string]ParseFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ui/validate/{kind}", func(w http.ResponseWriter, r *http.Request) {
		parse, ok := parsers[r.PathValue("kind")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		id := r.PostFormValue("_id")
		in := r.PostFormValue(r.PostFormValue("_name"))
		value, echo, msg := in, "", ""
		if strings.TrimSpace(in) != "" {
			f, e, err := parse(in)
			if err != nil {
				msg = err.Error()
			} else {
				value, echo = f, e
			}
		}
		w.Header().Set("HX-Trigger", fieldTrigger(id, value, msg != ""))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := fieldMsg(id, msg, echo).Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	return mux
}

// fieldTrigger is the HX-Trigger JSON, ASCII only: browsers read header
// values as Latin-1, so other characters travel as \u escapes.
func fieldTrigger(id, value string, invalid bool) string {
	type field struct {
		ID      string `json:"id"`
		Value   string `json:"value"`
		Invalid bool   `json:"invalid"`
	}
	b, _ := json.Marshal(map[string]field{"ui:field": {id, value, invalid}}) //nolint:errchkjson // plain strings always marshal
	var s strings.Builder
	for _, c := range string(b) {
		switch {
		case c < 0x80:
			s.WriteRune(c)
		case c > 0xFFFF:
			hi, lo := utf16.EncodeRune(c)
			fmt.Fprintf(&s, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&s, `\u%04x`, c)
		}
	}
	return s.String()
}
