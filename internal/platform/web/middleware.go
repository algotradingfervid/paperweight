package web

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5/middleware"
)

const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'"

type loggerKey struct{}

// accessLog logs one line per request and makes the logger available to
// ErrorFrom and the recoverer.
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			ctx := context.WithValue(r.Context(), loggerKey{}, logger)
			next.ServeHTTP(ww, r.WithContext(ctx))
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			logger.InfoContext(ctx, "request",
				"id", middleware.GetReqID(ctx),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start),
			)
		})
	}
}

func loggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// recoverer turns a panic into the 500 page. http.ErrAbortHandler is re-raised
// so net/http can abort the response as intended.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if p == http.ErrAbortHandler {
				panic(p)
			}
			loggerFrom(r.Context()).ErrorContext(r.Context(), "panic", "id", middleware.GetReqID(r.Context()), "panic", p)
			Error(w, r, http.StatusInternalServerError, internalText)
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// loadSession loads and saves the session and records the manager in the
// context so Flash and TakeFlash can reach it.
func loadSession(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithSessions(r.Context(), sm)))
		}))
	}
}
