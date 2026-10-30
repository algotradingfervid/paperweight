package web

import (
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
)

type uploaded struct {
	ID          int64  `json:"id"`
	FileName    string `json:"file_name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// upload streams the one "file" part to a temp file outside any transaction
// (CD-29), then attaches it inside db.Write and answers 201 with its details.
func (h *handlers) upload(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		signedOut(w, r)
		return
	}
	id, ok := parseID(chi.URLParam(r, "id"))
	if !ok {
		http.Error(w, files.MsgNotFound, http.StatusNotFound)
		return
	}
	limit := files.MaxFileBytes + MultipartOverhead
	if r.ContentLength > limit {
		http.Error(w, files.MsgTooLarge, http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, msgNoFilePart, http.StatusUnprocessableEntity)
		return
	}
	part, err := nextFilePart(mr)
	if err != nil {
		h.badPart(w, r, err)
		return
	}
	name := part.FileName()
	f, err := h.cfg.Store.Spool(part, files.MaxFileBytes)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer func() { _ = f.Cleanup() }()
	switch _, err := nextFilePart(mr); {
	case err == nil:
		http.Error(w, MsgTwoFileParts, http.StatusUnprocessableEntity)
		return
	case !errors.Is(err, io.EOF):
		h.badPart(w, r, err)
		return
	}

	parent := auth.RecordRef{DocType: chi.URLParam(r, "doctype"), ID: id}
	var m files.Meta
	err = h.cfg.DB.Write(r.Context(), func(tx *db.Tx) error {
		attID, err := h.cfg.Store.Attach(r.Context(), tx, p, parent, name, f)
		if err != nil {
			return err
		}
		m, err = h.cfg.Store.Meta(r.Context(), tx, attID)
		return err
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(uploaded{ID: m.ID, FileName: m.Name, Size: m.Size, ContentType: m.MIME})
}

// nextFilePart returns the next part named "file", or io.EOF when there is none.
func nextFilePart(mr *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := mr.NextPart()
		if err != nil {
			return nil, err
		}
		if part.FormName() == "file" {
			return part, nil
		}
	}
}

// badPart answers a multipart read error: an over-long body as 413, anything
// else (malformed body, client gone) as 422 with no error log.
func (h *handlers) badPart(w http.ResponseWriter, r *http.Request, err error) {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		h.fail(w, r, err)
		return
	}
	http.Error(w, msgNoFilePart, http.StatusUnprocessableEntity)
}
