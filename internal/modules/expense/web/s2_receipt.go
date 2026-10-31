package expenseweb

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	filesweb "github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// multipartMemory is how much of an upload ParseMultipartForm keeps in
// memory; the rest of the file goes to a temp file.
const multipartMemory = 1 << 20

// receipt uploads one receipt for a claim line (CD-133): one "file" part,
// spooled outside any transaction (CD-29), then attached in
// Service.AttachReceipt's db.Write with the version in the action's query
// (CD-183; FileUpload posts no field of ours). It answers #receipt-files with
// the thumbnail and, out of band, the line form's hidden receipt hash, so no
// field being typed in is redrawn (UX §5.5). The action's line (0 or none
// for a new line) leaves out the same-bytes note when that line already
// holds the file. The body may already be parsed by AUTH's CSRF check, so it
// is read as parsed, as companyweb's logo does.
func (h *handlers) receipt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		receiptFail(w, r, auth.ErrNotFound)
		return
	}
	version, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil {
		receiptFail(w, r, auth.ErrBadForm)
		return
	}
	limit := expense.ClaimPolicy.MaxBytes + filesweb.MultipartOverhead
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	err = r.ParseMultipartForm(multipartMemory) //nolint:gosec // G120: the MaxBytesReader above bounds it
	var over *http.MaxBytesError
	switch {
	case errors.As(err, &over):
		refuseUpload(w, http.StatusRequestEntityTooLarge, files.MsgTooLarge)
		return
	case err != nil:
		refuseUpload(w, http.StatusUnprocessableEntity, msgNoFile)
		return
	case len(r.MultipartForm.File["file"]) > 1:
		refuseUpload(w, http.StatusUnprocessableEntity, filesweb.MsgTwoFileParts)
		return
	}
	f, head, err := r.FormFile("file")
	if err != nil {
		refuseUpload(w, http.StatusUnprocessableEntity, msgNoFile)
		return
	}
	defer func() { _ = f.Close() }()
	sp, err := h.d.Files.Spool(f, expense.ClaimPolicy.MaxBytes)
	if err != nil {
		receiptFail(w, r, err)
		return
	}
	defer func() { _ = sp.Cleanup() }()
	p, _ := auth.PrincipalFrom(r.Context())
	got, err := h.d.Service.AttachReceipt(r.Context(), p, id, version, head.Filename, sp)
	if err != nil {
		receiptFail(w, r, err)
		return
	}
	line, _ := strconv.Atoi(r.URL.Query().Get("line"))
	note := ""
	if got.Reused && !slices.Contains(got.Lines, line) {
		note = msgReceiptOnce
		if len(got.Lines) > 0 {
			note = fmt.Sprintf(msgReceiptReused, got.Lines[0])
		}
	}
	answer := receiptAnswer([]components.Attachment{receiptAttachment(got.File)}, note, got.File.SHA256)
	ui.Render(w, r, http.StatusOK, answer, answer)
}

// receiptFail answers err as FILE's upload route does (CD-251): an error with
// a status (a *files.Error too) gives its status and text, and anything else
// the plain internal error.
func receiptFail(w http.ResponseWriter, r *http.Request, err error) {
	var se interface{ Status() int }
	switch {
	case errors.As(err, &se):
		refuseUpload(w, se.Status(), err.Error())
	default:
		web.ErrorFrom(w, r, err)
	}
}

// refuseUpload is files/web's htmx refusal (CD-251), which it doesn't
// export: an empty body, HX-Reswap none and an error notice, so the upload
// shows msg and the typed fields stay. FileUpload posts only through htmx.
func refuseUpload(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("HX-Reswap", "none")
	web.SetNotice(w.Header(), "error", msg)
	w.WriteHeader(status)
}
