package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// remove deletes one attachment inside db.Write and answers 204.
func (h *handlers) remove(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		signedOut(w, r)
		return
	}
	id, ok := parseID(chi.URLParam(r, "id"))
	if !ok {
		refuse(w, r, files.MsgNotFound, http.StatusNotFound)
		return
	}
	err := h.cfg.DB.Write(r.Context(), func(tx *db.Tx) error {
		return h.cfg.Store.Remove(r.Context(), tx, p, id)
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	refresh(w, r)
	w.WriteHeader(http.StatusNoContent)
}
