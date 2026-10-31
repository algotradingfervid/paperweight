package expenseweb

import "net/http"

// E9 (Mark as paid) replaces this file.

// markPaid posts Mark as paid; it checks cancel on the advance and redraws a
// refusal through renderPaidPage.
func (h *handlers) markPaid(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "This page isn't built yet.", http.StatusNotImplemented)
}

// undoPaid posts Undo paid; it checks cancel on the advance and redraws a
// refusal through renderPaidPage.
func (h *handlers) undoPaid(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "This page isn't built yet.", http.StatusNotImplemented)
}
