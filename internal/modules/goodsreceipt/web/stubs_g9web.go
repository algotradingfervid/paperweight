package grnweb

import "net/http"

// G9's stub (the reports' CSV export, which checks export in the handler).
// G9 deletes this file and adds reports_csv.go.

func (h *handlers) exportCSV(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
