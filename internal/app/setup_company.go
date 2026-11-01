package app

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	gstinweb "github.com/vardhan/internaltools/internal/masters/gstin/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

const (
	companyPath         = "/setup/company"
	msgCompanyAdminOnly = "Only an administrator can set up the company."
	// headOffice is what SetupTx names a head office left blank (CD-165).
	headOffice = "Head Office"
	// msgCompanySetUp is the banner after Save company (UI v2 §5.4).
	msgCompanySetUp = "Your company is set up. Next, add your users."
	// setupCheckSlug is the last step; a step opened from its Change link
	// returns there.
	setupCheckSlug = "check"
	// setupFurthestKey holds the furthest step reached among the draft's
	// values; no field has this name.
	setupFurthestKey = "_furthest"
)

// setupSteps are the wizard's steps (UI v2 §5.4, §5.6); setupFields are the
// fields each input step posts.
var (
	setupSteps = []wizard.Step{
		{Slug: "company", Label: "Company", Intro: "These details print on every purchase order and decide how GST is worked out."},
		{Slug: "office", Label: "Head office", Intro: "Orders are billed to a branch. You can add more branches later."},
		{Slug: setupCheckSlug, Label: "Check", Intro: "Nothing is saved until you press Save company."},
	}
	setupFields = [][]string{
		{"legal_name", "gstin", "state_code", "pan"},
		{"branch_code", "branch_name", "address_line1", "address_city", "address_pincode"},
	}
)

// The form's names for the fields the services report under their own.
var (
	branchFields  = map[string]string{"code": "branch_code", "name": "branch_name"}
	companyFields = map[string]string{"name": "legal_name"}
	addressFields = map[string]string{"line1": "address_line1", "city": "address_city", "pincode": "address_pincode"}
)

// companyView is one wizard step: the frame's model and bar, the draft's
// values by field name and the error on each field.
type companyView struct {
	M       wizard.Model
	Bar     wizard.Bar
	Values  map[string]string
	Fields  map[string]string
	Welcome bool   // the first visit: the draft is empty
	Echo    string // the GSTIN's state and PAN, as the live check shows them
	Cards   []setupCard
}

// setupCard is one input step's summary on Check.
type setupCard struct {
	Title, Change string
	Pairs         []components.Pair
}

func (v companyView) field(name, label, hint string, required bool) components.FieldProps {
	return components.FieldProps{
		ID: components.FieldID(name), Name: name, Label: label, Hint: hint, Required: required,
		Value: v.Values[name], Error: v.Fields[name],
	}
}

// more reports whether the step's More details opens: a field inside has a
// value or an error (UI v2 §5.5 item 3, A-10).
func (v companyView) more(names ...string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return v.Values[n] != "" || v.Fields[n] != "" })
}

// companyExists reports whether first-run setup has saved the company.
func (a *App) companyExists(ctx context.Context) (bool, error) {
	var exists bool
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		_, exists, err = a.masters.Company.Company(ctx, tx)
		return err
	})
	return exists, err
}

// setupSession is p's draft. The key names the user, so two administrators
// never share one.
func (a *App) setupSession(p auth.Principal) wizard.SessionValues {
	return wizard.SessionValues{SM: a.sm, Key: "setup_company:" + strconv.FormatInt(int64(p.ID), 10)}
}

// setupDraft is the draft's values and the furthest step reached.
func setupDraft(ctx context.Context, s wizard.SessionValues) (map[string]string, int) {
	vals := s.Get(ctx)
	furthest, _ := strconv.Atoi(vals[setupFurthestKey]) // absent: 0
	delete(vals, setupFurthestKey)
	return vals, furthest
}

func setupURL(i int) string { return companyPath + "/" + setupSteps[i].Slug }

// setupStart is the step the URL names, after the checks every step makes:
// an administrator (the route's guard), no company yet, a known step. The
// bare /setup/company is the first step.
func (a *App) setupStart(w http.ResponseWriter, r *http.Request) (int, bool) {
	i := 0
	if slug := chi.URLParam(r, "step"); slug != "" {
		i = slices.IndexFunc(setupSteps, func(s wizard.Step) bool { return s.Slug == slug })
	}
	exists, err := a.companyExists(r.Context())
	if err == nil && (exists || i < 0) {
		err = auth.ErrNotFound
	}
	if err != nil {
		a.srv.WriteError(w, r, err)
		return 0, false
	}
	return i, true
}

// companyGet is the second first-run step (CD-231): a wizard step, while no
// company exists. A step past the furthest reached sends the browser there.
func (a *App) companyGet(w http.ResponseWriter, r *http.Request) {
	i, ok := a.setupStart(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	vals, furthest := setupDraft(r.Context(), a.setupSession(p))
	if i > furthest {
		http.Redirect(w, r, setupURL(furthest), http.StatusSeeOther)
		return
	}
	v := a.setupView(r, i, furthest, vals, nil)
	ui.Render(w, r, http.StatusOK, companyPage(v), companyFrame(v))
}

// companyPost is a step's Back, Continue or, on Check, Save company. What was
// typed goes into the session before the response starts (CD-136); nothing
// else is written until Save company.
func (a *App) companyPost(w http.ResponseWriter, r *http.Request) {
	i, ok := a.setupStart(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	s := a.setupSession(p)
	vals, furthest := setupDraft(ctx, s)
	if i > furthest {
		wizard.Respond(w, r, wizard.Finish, setupURL(furthest), nil)
		return
	}
	last := len(setupSteps) - 1
	back := wizard.Nav(r) == "back"
	if i == last && !back {
		a.companySave(w, r, p, s, vals)
		return
	}
	typed := map[string]string{}
	if i < last {
		for _, name := range setupFields[i] {
			typed[name] = r.PostFormValue(name)
		}
	}
	maps.Copy(vals, typed)
	if back {
		s.Merge(ctx, typed)
		a.setupRespond(w, r, wizard.Advance, a.setupView(r, max(i-1, 0), furthest, vals, nil))
		return
	}
	if errs := setupStepErrors(i, checkSetup(vals)); len(errs) > 0 {
		s.Merge(ctx, typed)
		a.setupRespond(w, r, wizard.Invalid, a.setupView(r, i, furthest, vals, errs))
		return
	}
	furthest = max(furthest, i+1)
	typed[setupFurthestKey] = strconv.Itoa(furthest)
	s.Merge(ctx, typed)
	next := i + 1
	if r.PostFormValue("return") == setupCheckSlug && furthest == last {
		next = last
	}
	a.setupRespond(w, r, wizard.Advance, a.setupView(r, next, furthest, vals, nil))
}

// companySave saves the company, its head office and the head office's
// address in one write (CD-165, CD-337 (7)). The check that none exists runs
// inside the write, so two administrators can't both save. A refusal goes to
// the first step owning a field in it, with the draft as it was.
func (a *App) companySave(w http.ResponseWriter, r *http.Request, p auth.Principal, s wizard.SessionValues, vals map[string]string) {
	ctx := r.Context()
	if errs := checkSetup(vals); len(errs) > 0 {
		a.setupRefused(w, r, vals, errs)
		return
	}
	in := setupInput(vals)
	names := companyFields
	err := a.db.Write(ctx, func(tx *db.Tx) error {
		_, exists, err := a.masters.Company.Company(ctx, tx)
		if err != nil {
			return err
		}
		if exists {
			return auth.ErrNotFound
		}
		// The branch passed checkSetup, so a "name" from SetupTx is the
		// legal name.
		if err := a.masters.Company.SetupTx(ctx, tx, p, in); err != nil {
			return err
		}
		b, _, err := a.masters.Company.ByCode(ctx, tx, in.BranchCode)
		if err != nil {
			return err
		}
		names = addressFields
		_, err = a.masters.Address.CreateTx(ctx, tx, p, addressInput(vals, b.ID, b.StateCode))
		return err
	})
	if errs := formErrors(err, names); len(errs) > 0 {
		a.setupRefused(w, r, vals, errs)
		return
	}
	if err != nil {
		a.srv.WriteError(w, r, err)
		return
	}
	s.Clear(ctx)
	web.Flash(ctx, "success", msgCompanySetUp)
	wizard.Respond(w, r, wizard.Finish, homePath, nil)
}

// setupRefused shows the first step owning an error, else Check, 422.
func (a *App) setupRefused(w http.ResponseWriter, r *http.Request, vals map[string]string, errs []doctype.FieldError) {
	last := len(setupSteps) - 1
	i := last
	for n := range setupFields {
		if own := setupStepErrors(n, errs); len(own) > 0 {
			i, errs = n, own
			break
		}
	}
	v := a.setupView(r, i, last, vals, errs)
	if i < last {
		v.M.ReturnTo = setupCheckSlug
	}
	w.Header().Set("HX-Push-Url", setupURL(i))
	a.setupRespond(w, r, wizard.Invalid, v)
}

// setupRespond answers with step v: a 303 or the fragment for Advance, the
// step again, 422, for Invalid.
func (a *App) setupRespond(w http.ResponseWriter, r *http.Request, o wizard.Outcome, v companyView) {
	wizard.Respond(w, r, o, setupURL(v.M.Current), func(fragment bool) templ.Component {
		if fragment {
			return companyFrame(v)
		}
		return companyPage(v)
	})
}

// setupView is step i on screen.
func (a *App) setupView(r *http.Request, i, furthest int, vals map[string]string, errs []doctype.FieldError) companyView {
	last := len(setupSteps) - 1
	m := wizard.Model{
		Mode: wizard.Master, Title: "Set up your company", BaseURL: companyPath, LastLabel: "Check",
		Steps: setupSteps, Current: i, Furthest: furthest,
	}
	if i < last && r.FormValue("return") == setupCheckSlug {
		m.ReturnTo = setupCheckSlug
	}
	v := companyView{
		Bar:     wizard.Bar{Primary: "Continue", Back: i > 0},
		Values:  vals,
		Fields:  map[string]string{},
		Welcome: i == 0 && len(vals) == 0,
	}
	for _, e := range errs {
		id := "step-title"
		if i < last && slices.Contains(setupFields[i], e.Field) {
			id = components.FieldID(e.Field)
			v.Fields[e.Field] = e.Message
		}
		m.Errors = append(m.Errors, components.FieldErr{ID: id, Message: e.Message})
	}
	if g := vals["gstin"]; g != "" && v.Fields["gstin"] == "" {
		if _, echo, err := gstinweb.Parsers()[string(components.GSTIN)](g); err == nil {
			v.Echo = echo
		}
	}
	if i == last {
		v.Bar.Primary = "Save company"
		v.Cards = setupCards(vals)
		m.After = setupCheckCards(v.Cards)
	}
	v.M = m
	return v
}

// setupCards is Check's summary: what Save company will write.
func setupCards(vals map[string]string) []setupCard {
	in := setupInput(vals)
	g := gstin.Normalize(in.GSTIN)
	state := strings.TrimSpace(in.StateCode)
	if g != "" {
		state = gstin.StateCode(g)
	}
	if st, ok := gstin.StateByCode(state); ok {
		state = st.Name + " (" + st.Code + ")"
	}
	pan := gstin.Normalize(in.PAN)
	if pan == "" {
		pan = gstin.PANFromGSTIN(g)
	}
	name := in.BranchName
	if strings.TrimSpace(name) == "" {
		name = headOffice
	}
	return []setupCard{
		{Title: "Company", Change: setupURL(0) + "?return=" + setupCheckSlug, Pairs: []components.Pair{
			{Label: "Legal name", Value: in.LegalName},
			{Label: "GSTIN", Value: g, Code: true},
			{Label: "State", Value: state},
			{Label: "PAN", Value: pan, Code: true},
		}},
		{Title: "Head office", Change: setupURL(1) + "?return=" + setupCheckSlug, Pairs: []components.Pair{
			{Label: "Head office code", Value: in.BranchCode, Code: true},
			{Label: "Head office name", Value: name},
			{Label: "Address", Value: vals["address_line1"]},
			{Label: "City", Value: vals["address_city"]},
			{Label: "Pincode", Value: vals["address_pincode"]},
		}},
	}
}

func setupInput(v map[string]string) company.SetupInput {
	return company.SetupInput{
		LegalName: v["legal_name"], StateCode: v["state_code"], GSTIN: v["gstin"],
		PAN: v["pan"], BranchCode: v["branch_code"], BranchName: v["branch_name"],
	}
}

// addressInput is the head office's address: billing and shipping, in the
// branch's state.
func addressInput(v map[string]string, branchID int64, state string) address.Input {
	return address.Input{
		BranchID: branchID, Line1: v["address_line1"], City: v["address_city"], Pincode: v["address_pincode"],
		StateCode: state, IsBilling: true, IsShipping: true,
	}
}

// checkSetup is Save company's rules on the draft without the database:
// the head office's (as SetupTx checks it), the company's and the address's,
// under the form's field names. A bad GSTIN gives no PAN, so it alone is
// reported then, not a missing PAN.
func checkSetup(v map[string]string) []doctype.FieldError {
	in := setupInput(v)
	name := in.BranchName
	if strings.TrimSpace(name) == "" {
		name = headOffice
	}
	branch, err := company.ValidateBranch(company.BranchInput{
		Code: in.BranchCode, Name: name, StateCode: in.StateCode, GSTIN: in.GSTIN, IsHeadOffice: true,
	}, "")
	errs := formErrors(err, branchFields)
	badGSTIN := slices.ContainsFunc(errs, func(e doctype.FieldError) bool { return e.Field == "gstin" })
	pan := gstin.Normalize(in.PAN)
	if pan == "" && !badGSTIN {
		pan = gstin.PANFromGSTIN(branch.GSTIN)
	}
	_, err = company.ValidateCompany(company.CompanyInput{LegalName: in.LegalName, PAN: pan})
	for _, e := range formErrors(err, companyFields) {
		if e.Field != "pan" || pan != "" || !badGSTIN {
			errs = append(errs, e)
		}
	}
	// The branch doesn't exist yet: any owner passes Validate's one-owner
	// rule, and the save gives the real one. The state is the branch's.
	_, err = address.Validate(addressInput(v, 1, branch.StateCode))
	for _, e := range formErrors(err, addressFields) {
		if slices.Contains(setupFields[1], e.Field) {
			errs = append(errs, e)
		}
	}
	return errs
}

// formErrors is err's field errors under the form's names.
func formErrors(err error, names map[string]string) []doctype.FieldError {
	var refused doctype.HTTPError
	if !errors.As(err, &refused) {
		return nil
	}
	var out []doctype.FieldError
	for _, f := range refused.FieldErrors() {
		if form, ok := names[f.Field]; ok {
			f.Field = form
		}
		out = append(out, f)
	}
	return out
}

// setupStepErrors keeps the errors on step i's fields.
func setupStepErrors(i int, errs []doctype.FieldError) []doctype.FieldError {
	return slices.DeleteFunc(slices.Clone(errs), func(e doctype.FieldError) bool {
		return !slices.Contains(setupFields[i], e.Field)
	})
}
