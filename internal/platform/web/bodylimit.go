package web

import (
	"fmt"
	"net/http"
)

// BodyCeiling limits every request body to limit bytes (CD-206). A declared
// Content-Length over the limit gets 413 with msg through Error, before the body
// is read; any other body is wrapped, so reading past the limit, chunked included,
// gives the handler *http.MaxBytesError. It belongs ahead of anything that parses
// the form, CSRF included. A limit below 1 or an empty msg panics here, when the
// chain is built. Keep the limit under net/http's 32 MiB form memory limit, or a
// form can spool up to the limit.
func BodyCeiling(limit int64, msg string) func(http.Handler) http.Handler {
	if limit < 1 {
		panic(fmt.Sprintf("web: body ceiling %d is below 1", limit))
	}
	if msg == "" {
		panic("web: body ceiling has no message")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				Error(w, r, http.StatusRequestEntityTooLarge, msg)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
