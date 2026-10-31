package expenseweb

import "net/http"

// E8 (Reports and CSV) replaces this file.

// exportCSV answers a report's CSV, after checking export (CD-145).
func (h *handlers) exportCSV(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "This page isn't built yet.", http.StatusNotImplemented)
}
