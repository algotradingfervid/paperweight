package modkitweb

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

const (
	settingsPath = "/settings/business"
	pageTitle    = "Business settings"
	lockHint     = "GRNs, expense claims and employee advances dated on or before this date can't be submitted, approved or cancelled; closing stays allowed. Purchase orders aren't locked; leave it blank to turn the lock off."
	termsHint    = "Filled in on each new purchase order."
	termsMaxLen  = 2000
	msgSaved     = "Business settings saved."
)

var lockID = components.FieldID("locked_up_to")

// settingsView is the page's values as the form shows them.
type settingsView struct {
	Version, LockedUpTo, Terms string
	Today, Yesterday           string
	Problem, LockErr           string
}

func (v settingsView) summary() []components.FieldErr {
	if v.LockErr == "" {
		return nil
	}
	return []components.FieldErr{{ID: lockID, Message: v.LockErr}}
}

func (h *handlers) kit() *modkit.Kit {
	return modkit.New(modkit.Deps{DB: h.d.DB, Auth: h.d.Auth, Audit: h.d.Audit, Clock: h.d.Clock})
}

// settingsGet shows the stored settings to an Administrator; anyone else
// gets the 403.
func (h *handlers) settingsGet(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	s, err := h.kit().GetSettings(r.Context(), p)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := settingsView{Version: strconv.Itoa(s.Version), Terms: s.DefaultPOTerms}
	if s.LockedUpTo.Valid {
		v.LockedUpTo = types.FormatDate(s.LockedUpTo.V)
	}
	h.render(w, r, http.StatusOK, v)
}

// settingsPost saves the posted settings at the posted version (CD-94) and
// goes back to the page with a success banner. A bad date (422) or a stale
// version (409) shows the form again with what was typed.
func (h *handlers) settingsPost(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	// The right goes before the form's checks: a non-Administrator gets the
	// 403 whatever was posted. SaveSettings checks it again in its write.
	if _, err := h.kit().GetSettings(r.Context(), p); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrBadForm)
		return
	}
	v := settingsView{Version: r.PostFormValue("version"), LockedUpTo: r.PostFormValue("locked_up_to"), Terms: r.PostFormValue("default_po_terms")}
	in := modkit.SettingsInput{DefaultPOTerms: v.Terms}
	if v.LockedUpTo != "" {
		d, err := types.ParseDate(v.LockedUpTo)
		if err != nil {
			v.LockErr = err.Error()
			h.render(w, r, http.StatusUnprocessableEntity, v)
			return
		}
		in.LockedUpTo = sql.Null[types.Date]{V: d, Valid: true}
	}
	err = h.kit().SaveSettings(r.Context(), p, version, in)
	if doc.IsStale(err) {
		v.Problem = err.Error()
		h.render(w, r, http.StatusConflict, v)
		return
	}
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	web.Flash(r.Context(), "success", msgSaved)
	http.Redirect(w, r, settingsPath, http.StatusSeeOther)
}

// render writes the page with today and yesterday for the date's buttons.
func (h *handlers) render(w http.ResponseWriter, r *http.Request, status int, v settingsView) {
	today := types.Today(h.d.Clock)
	v.Today, v.Yesterday = types.FormatDate(today), types.FormatDate(types.AddDays(today, -1))
	ui.Render(w, r, status, settingsPage(v), nil)
}
