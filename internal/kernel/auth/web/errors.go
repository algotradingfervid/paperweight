package authweb

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// WriteError answers with err through web.ErrorFrom (CD-76, CD-81) and logs
// it once with the request's user ID: an Info line for a status below 500, an
// Error line for a status of 500 or more and for db.ErrBusy (web.ErrorFrom
// logs neither), and web.ErrorFrom's own line for any other error, which is
// wrapped with the user ID (errors.Is still matches).
func (s *Server) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	p, signedIn := auth.PrincipalFrom(ctx)
	var user any
	if signedIn {
		user = p.ID
	}
	var se interface{ Status() int }
	switch {
	case errors.As(err, &se) && se.Status() < http.StatusInternalServerError:
		if signedIn {
			slog.InfoContext(ctx, "request error", "user", user, "path", r.URL.Path, "err", err)
		}
	case errors.As(err, &se) || errors.Is(err, db.ErrBusy):
		slog.ErrorContext(ctx, "request error", "user", user, "path", r.URL.Path, "err", err)
	case signedIn:
		err = fmt.Errorf("user %d: %w", p.ID, err)
	}
	web.ErrorFrom(w, r, err)
}
