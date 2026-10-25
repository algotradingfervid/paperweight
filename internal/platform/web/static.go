package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

//go:embed static/htmx.min.js static/alpine.min.js
var staticFS embed.FS

// WP0 addition: the hashed URLs of htmx and Alpine (CSP build). The shell uses
// these and never hard-codes a path.
var HTMXURL, AlpineURL string

// staticFiles maps a hashed name (without "/static/") to its bytes.
var staticFiles = map[string][]byte{}

func init() {
	HTMXURL = addStatic("htmx.min.js", "htmx", "js")
	AlpineURL = addStatic("alpine.min.js", "alpine-csp", "js")
}

// addStatic registers an embedded file under <base>.<first 8 hex of SHA-256>.<ext>
// and returns its URL.
func addStatic(file, base, ext string) string {
	b, err := staticFS.ReadFile("static/" + file)
	if err != nil {
		panic("web: embedded static/" + file + " is missing: " + err.Error())
	}
	sum := sha256.Sum256(b)
	name := base + "." + hex.EncodeToString(sum[:])[:8] + "." + ext
	staticFiles[name] = b
	return "/static/" + name
}

// Static serves the embedded files under /static/ by hashed name only, with
// the immutable cache header. Unhashed or unknown names are 404 (CD-47).
func Static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Static files can only be fetched with GET or HEAD.", http.StatusMethodNotAllowed)
			return
		}
		name, ok := strings.CutPrefix(r.URL.Path, "/static/")
		b, found := staticFiles[name]
		if !ok || !found {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
	})
}
