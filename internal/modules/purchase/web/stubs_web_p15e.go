package purchaseweb

import (
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// P15e's stubs (module pages). P15e deletes this file and adds its own.

func (h *handlers) home(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

func (h *handlers) queue(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

func (h *handlers) handoverPage(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

func (h *handlers) sendPage(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

func (h *handlers) expectedPage(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

func (h *handlers) pendingReport(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

func (h *handlers) pendingFilter(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

// The page redraws P10, P11 and P18's POSTs call for a refusal other than
// 403 and 404, with the HTTPError's status and field errors. Each page
// reloads its own view from the path id and reads the typed values from
// r.PostForm.
var (
	renderHandover = func(w http.ResponseWriter, _ *http.Request, status int, errs []doctype.FieldError) { //nolint:unused // called by P10, built by P15e
		plainErrors(w, status, errs)
	}
	renderSend = func(w http.ResponseWriter, _ *http.Request, status int, errs []doctype.FieldError) { //nolint:unused // called by P11, built by P15e
		plainErrors(w, status, errs)
	}
	renderExpected = func(w http.ResponseWriter, _ *http.Request, status int, errs []doctype.FieldError) { //nolint:unused // called by P18, built by P15e
		plainErrors(w, status, errs)
	}
)
