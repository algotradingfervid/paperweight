package authweb_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// Every probe route declares its permission (WP1-AUTH Task 14, Step 4).
func TestProbe_RoutesDeclarePermission(t *testing.T) {
	for name, opts := range map[string][]webtest.Option{
		"plain":       {webtest.WithProbe()},
		"tls":         {webtest.WithProbe(), webtest.WithTLS()},
		"trust proxy": {webtest.WithProbe(), webtest.WithTrustProxy()},
	} {
		t.Run(name, func(t *testing.T) {
			h := webtest.New(t, authtest.NewWorld(t), opts...)
			if got := authweb.CheckRoutes(h.Router); len(got) != 0 {
				t.Fatalf("CheckRoutes(probe router) = %q, want none", got)
			}
		})
	}
}
