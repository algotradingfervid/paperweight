package authweb_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// AUTH-048
func TestSession_CookieAttributes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   []webtest.Option
		cookie string
		secure bool
	}{
		{"https", []webtest.Option{webtest.WithTLS()}, "__Host-session", true},
		{"http", nil, "session", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := authtest.NewWorld(t)
			h, _ := t10Harness(t, w, tc.opts...)
			c := signedIn(t, h, t10Priya)
			ck := c.Cookie()
			if ck == nil {
				t.Fatal("no session cookie after sign-in")
			}
			if ck.Name != tc.cookie {
				t.Errorf("Name = %q, want %q", ck.Name, tc.cookie)
			}
			if ck.Value == "" {
				t.Error("Value is empty")
			}
			if ck.Secure != tc.secure {
				t.Errorf("Secure = %v, want %v", ck.Secure, tc.secure)
			}
			if !ck.HttpOnly {
				t.Error("HttpOnly = false, want true")
			}
			if ck.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", ck.SameSite)
			}
			if ck.Path != "/" {
				t.Errorf("Path = %q, want /", ck.Path)
			}
			if ck.Domain != "" {
				t.Errorf("Domain = %q, want none", ck.Domain)
			}
			if !ck.Expires.IsZero() || ck.RawExpires != "" || ck.MaxAge != 0 {
				t.Errorf("Expires %v (raw %q), MaxAge %d; want a browser-session cookie (CD-14)", ck.Expires, ck.RawExpires, ck.MaxAge)
			}
		})
	}
}
