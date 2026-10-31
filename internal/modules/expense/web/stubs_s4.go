package expenseweb

import "net/http"

// S4 (Report pages) replaces this file.

// report is the open claims, expense lines or advances report page.
func (h *handlers) report(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "This page isn't built yet.", http.StatusNotImplemented)
}
