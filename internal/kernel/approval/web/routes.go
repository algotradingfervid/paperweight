// Package web holds the approval screens: the inbox ("Waiting for my
// approval"), My requests, the inbox count badge, the approval-rule admin
// screens (WP1-APR Task 11) and Stuck approvals (Task 16). It is built from
// internal/ui components and adds nothing to internal/ui (CD-46).
package web

import (
	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/approval"
)

// Deps are what the screens need; Layout and DocURL are wired in Wave 3.
type Deps struct {
	Approvals *approval.Approvals
	Layout    func(title string, body templ.Component) templ.Component // internal/ui page shell
	DocURL    func(doctype string, id int64) string                    // DOC form URL
}

// Routes registers the approval screens.
func Routes(d Deps) func(r chi.Router) {
	h := &handlers{d: d}
	return func(r chi.Router) {
		r.Get("/approvals", h.inbox)
		r.Get("/approvals/count", h.count)
		r.Get("/approvals/mine", h.mine)
		r.Get("/admin/approvals/stuck", h.stuck)
		r.Get("/admin/approval-rules", h.rules)
		r.Get("/admin/approval-rules/new", h.newRule)
		r.Post("/admin/approval-rules", h.createRule)
		r.Get("/admin/approval-rules/{id}", h.editRule)
		r.Post("/admin/approval-rules/{id}", h.updateRule)
		r.Post("/admin/approval-rules/{id}/enable", h.setEnabled(true))
		r.Post("/admin/approval-rules/{id}/disable", h.setEnabled(false))
		r.Post("/admin/approval-rules/{id}/delete", h.deleteRule)
	}
}
