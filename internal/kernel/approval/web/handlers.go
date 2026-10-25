package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

const (
	pageSize  = 25
	rulesPath = "/admin/approval-rules"
	// badFormText is AUTH's errBadForm wording for a form missing its version (CD-146).
	badFormText = "The form could not be read. Reload the page and try again."
)

type handlers struct{ d Deps }

// principal is the signed-in user; AUTH's middleware answers 401 before a
// handler runs, so a missing one is a 403 here.
func (h *handlers) principal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		web.ErrorFrom(w, r, auth.ErrForbidden)
	}
	return p, ok
}

func (h *handlers) render(w http.ResponseWriter, r *http.Request, status int, title string, body templ.Component) {
	ui.Render(w, r, status, h.d.Layout(title, body), nil)
}

// pageNumber is ?page=, 1 when missing or not a positive number.
func pageNumber(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func pageInfo(path, noun string, page, total int) components.PageInfo {
	pi := components.PageInfo{
		Page: page, From: (page-1)*pageSize + 1, To: min(page*pageSize, total), Total: total, Noun: noun,
		NextURL: path + "?page=" + strconv.Itoa(page+1),
	}
	if page > 1 {
		pi.PrevURL = path + "?page=" + strconv.Itoa(page-1)
	}
	return pi
}

func (h *handlers) inbox(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	page := pageNumber(r)
	res, err := h.d.Approvals.Inbox(r.Context(), p, types.Page{Number: page, Size: pageSize})
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "Waiting for my approval",
		inboxPage(res.Rows, h.d.DocURL, pageInfo("/approvals", "requests", page, res.Total)))
}

func (h *handlers) count(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	res, err := h.d.Approvals.Inbox(r.Context(), p, types.Page{Number: 1, Size: 1})
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	ui.Render(w, r, http.StatusOK, badge(res.Total), nil)
}

func (h *handlers) mine(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	page := pageNumber(r)
	res, err := h.d.Approvals.MyRequests(r.Context(), p, types.Page{Number: page, Size: pageSize})
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "My requests",
		minePage(res.Rows, h.d.DocURL, pageInfo("/approvals/mine", "requests", page, res.Total)))
}

func (h *handlers) rules(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	rules, err := h.d.Approvals.Rules(r.Context(), p)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "Approval rules", rulesPage(rules, h.labels()))
}

func (h *handlers) newRule(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	if _, err := h.d.Approvals.Rules(r.Context(), p); err != nil { // Administrators only
		web.ErrorFrom(w, r, err)
		return
	}
	h.renderForm(w, r, http.StatusOK, ruleForm{ApproverType: string(approval.ApproverRole)})
}

func (h *handlers) editRule(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	rule, err := h.findRule(r, p)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	h.renderForm(w, r, http.StatusOK, formOf(rule))
}

func (h *handlers) createRule(w http.ResponseWriter, r *http.Request) {
	h.saveRule(w, r, 0)
}

func (h *handlers) updateRule(w http.ResponseWriter, r *http.Request) {
	id, err := ruleID(r)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	h.saveRule(w, r, id)
}

// saveRule creates (id 0) or updates rule id from the posted form, using the
// version the page posted (CD-94).
func (h *handlers) saveRule(w http.ResponseWriter, r *http.Request, id int64) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	f := ruleForm{
		ID:           id,
		DocType:      r.PostFormValue("doctype"),
		FromAmount:   r.PostFormValue("from_amount"),
		ApproverType: r.PostFormValue("approver_type"),
		Role:         r.PostFormValue("role"),
		FallbackRole: r.PostFormValue("fallback_role"),
		AllowSelf:    r.PostFormValue("allow_self_approval") == "yes",
	}
	if id != 0 { // an edit saves against the version the page posted (CD-94, CD-146)
		v, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
		if err != nil {
			web.Error(w, r, http.StatusBadRequest, badFormText)
			return
		}
		f.Version = v
	}
	amount, perr := types.ParseMoney(f.FromAmount)
	if perr != nil {
		// The parse error is answered here, before the service sees the rule, so
		// the admin check runs first: a non-admin gets 403, never a form that
		// reveals what the rule page would accept.
		if _, err := h.d.Approvals.Rules(r.Context(), p); err != nil { // Administrators only
			web.ErrorFrom(w, r, err)
			return
		}
		f.Errs = []doctype.FieldError{{Field: "from_amount", Message: amountMessage(perr)}}
		h.renderForm(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	in := approval.RuleInput{
		DocType: f.DocType, FromAmount: amount, ApproverType: approval.ApproverType(f.ApproverType),
		Role: f.Role, FallbackRole: f.FallbackRole, AllowSelfApproval: f.AllowSelf,
	}
	var err error
	if id == 0 {
		_, err = h.d.Approvals.CreateRule(r.Context(), p, in)
	} else {
		_, err = h.d.Approvals.UpdateRule(r.Context(), p, id, f.Version, in)
	}
	var he doctype.HTTPError
	if errors.As(err, &he) && len(he.FieldErrors()) > 0 {
		f.Errs = he.FieldErrors()
		h.renderForm(w, r, he.Status(), f)
		return
	}
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	http.Redirect(w, r, rulesPath, http.StatusSeeOther)
}

func (h *handlers) setEnabled(enabled bool) http.HandlerFunc {
	return h.ruleAction(func(r *http.Request, p auth.Principal, id int64) error {
		return h.d.Approvals.SetRuleEnabled(r.Context(), p, id, enabled)
	})
}

func (h *handlers) deleteRule(w http.ResponseWriter, r *http.Request) {
	h.ruleAction(func(r *http.Request, p auth.Principal, id int64) error {
		return h.d.Approvals.DeleteRule(r.Context(), p, id)
	})(w, r)
}

// ruleAction runs do on the rule in the URL and returns to the rules list.
func (h *handlers) ruleAction(do func(*http.Request, auth.Principal, int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.principal(w, r)
		if !ok {
			return
		}
		id, err := ruleID(r)
		if err == nil {
			err = do(r, p, id)
		}
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		http.Redirect(w, r, rulesPath, http.StatusSeeOther)
	}
}

func (h *handlers) renderForm(w http.ResponseWriter, r *http.Request, status int, f ruleForm) {
	title := "New approval rule"
	if f.ID != 0 {
		title = "Edit approval rule"
	}
	h.render(w, r, status, title, rulePage(f, h.docTypeOptions()))
}

// findRule is the rule named in the URL; an unknown id is doctype.ErrNotFound.
func (h *handlers) findRule(r *http.Request, p auth.Principal) (approval.Rule, error) {
	id, err := ruleID(r)
	if err != nil {
		return approval.Rule{}, err
	}
	rules, err := h.d.Approvals.Rules(r.Context(), p)
	if err != nil {
		return approval.Rule{}, err
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return approval.Rule{}, doctype.ErrNotFound
}

func ruleID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		return 0, doctype.ErrNotFound
	}
	return id, nil
}

// amountMessage is NUM's parse message for the from amount (design-system §9);
// a blank one is types.ErrEmpty as DOC's "<Label> is required.".
func amountMessage(err error) string {
	var ie *types.InputError
	if errors.As(err, &ie) {
		return ie.Msg
	}
	return "From amount is required."
}

func (h *handlers) labels() map[string]string {
	out := map[string]string{}
	for _, dt := range h.d.Approvals.ApprovableDocTypes() {
		out[dt.Name] = dt.Label
	}
	return out
}

func (h *handlers) docTypeOptions() []components.Option {
	opts := []components.Option{{Value: "", Label: "Choose a document type"}}
	for _, dt := range h.d.Approvals.ApprovableDocTypes() {
		opts = append(opts, components.Option{Value: dt.Name, Label: dt.Label})
	}
	return opts
}

// ruleForm is the rule form's values as posted, with any field errors.
type ruleForm struct {
	ID, Version                                           int64
	DocType, FromAmount, ApproverType, Role, FallbackRole string
	AllowSelf                                             bool
	Errs                                                  []doctype.FieldError
}

func formOf(r approval.Rule) ruleForm {
	return ruleForm{
		ID: r.ID, Version: r.Version, DocType: r.DocType, FromAmount: types.FormatMoney(r.FromAmount),
		ApproverType: string(r.ApproverType), Role: r.Role, FallbackRole: r.FallbackRole, AllowSelf: r.AllowSelfApproval,
	}
}

// errFor is the message on field, "" when none.
func (f ruleForm) errFor(field string) string {
	for _, e := range f.Errs {
		if e.Field == field {
			return e.Message
		}
	}
	return ""
}

func (f ruleForm) summary() []components.FieldErr {
	out := make([]components.FieldErr, 0, len(f.Errs))
	for _, e := range f.Errs {
		out = append(out, components.FieldErr{ID: components.FieldID(e.Field), Message: e.Message})
	}
	return out
}

func (f ruleForm) action() string {
	if f.ID == 0 {
		return rulesPath
	}
	return rulesPath + "/" + strconv.FormatInt(f.ID, 10)
}
