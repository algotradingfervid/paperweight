package expenseweb

import "net/http"

// S2 (Expense line page and receipt route) replaces this file.

// receipt uploads one receipt for a claim line (CD-133) and answers the
// fragment with its thumbnail.
func (h *handlers) receipt(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "This page isn't built yet.", http.StatusNotImplemented)
}
