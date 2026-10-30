package companyweb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	filesweb "github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

const (
	logoPath   = "/m/company/1/logo"
	removePath = logoPath + "/remove"
	uploadID   = "company-logo"
	uploadArea = "company-logo-area"
	msgNoFile  = "Choose a file to upload."

	msgLogoChanged = "The logo changed after you opened this page. Reload it and make your changes again."
)

// LogoRoutes mounts the company logo page at /m/company/1/logo, its upload
// and its remove (CD-140, CD-198, CD-209). Every route needs the company's
// write right.
//
// st must serve the Company with files.LogoPolicy under Policies and with Info
// behind Config.Info, or it refuses the logo.
func LogoRoutes(svc *company.Service, env mstkit.Env, st *files.Store) func(chi.Router) {
	h := &logoHandlers{svc: svc, env: env, store: st}
	return func(r chi.Router) {
		r.Get(logoPath, h.handle(h.page))
		r.Post(logoPath, h.handle(h.upload))
		r.Post(removePath, h.handle(h.remove))
	}
}

// LogoURL is company.Letterhead.LogoURL (CD-140, CD-209): the logo's
// /files/{id} URL (CD-58), or "" when there is none, so the brand logo falls
// back. Any signed-in user gets it, read in tx.
func LogoURL(svc *company.Service, st *files.Store) func(ctx context.Context, tx *db.Tx) (string, error) {
	return func(ctx context.Context, tx *db.Tx) (string, error) {
		c, ok, err := svc.Company(ctx, tx)
		if err != nil || !ok {
			return "", err
		}
		return st.LatestURL(ctx, tx, *companyRef(c.ID))
	}
}

// Info is next with the Company answered. doc.Engine serves documents only,
// and files.Store asks Info for every parent, so the store needs this in front
// of the engine to attach anything to the Company.
func Info(svc *company.Service, next doctype.InfoSource) doctype.InfoSource {
	return companyInfo{svc: svc, next: next}
}

type companyInfo struct {
	svc  *company.Service
	next doctype.InfoSource
}

func (i companyInfo) Info(ctx context.Context, tx *db.Tx, dt string, id int64) (doctype.Info, error) {
	if dt != company.CompanyDocType {
		return i.next.Info(ctx, tx, dt, id)
	}
	c, err := companyService{svc: i.svc}.Get(ctx, tx, id)
	if err != nil {
		return doctype.Info{}, err
	}
	return doctype.Info{DocType: dt, DocLabel: company.CompanyKind.Label, ID: c.ID, Display: c.LegalName}, nil
}

// logoLink is Screen.Extra: the link to the logo page. Extra is given no
// principal, so the right is checked on the render context.
func logoLink(env mstkit.Env) func(company.Company) templ.Component {
	return func(c company.Company) templ.Component {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			p, _ := auth.PrincipalFrom(ctx)
			if env.Auth.Can(ctx, p, company.CompanyDocType, auth.RightWrite, companyRef(c.ID)) != nil {
				return nil
			}
			return components.Button(components.ButtonProps{Label: "Company logo", Href: logoPath, Variant: components.Secondary}).Render(ctx, w)
		})
	}
}

type logoHandlers struct {
	svc   *company.Service
	env   mstkit.Env
	store *files.Store
}

// handle runs fn as the signed-in user, who needs the company's write right.
func (h *logoHandlers) handle(fn func(http.ResponseWriter, *http.Request, auth.Principal) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFrom(r.Context())
		err := h.env.Auth.Can(r.Context(), p, company.CompanyDocType, auth.RightWrite, companyRef(companyID))
		if err == nil {
			err = fn(w, r, p)
		}
		if err != nil {
			h.fail(w, r, err)
		}
	}
}

// fail renders what show can't: as mstweb's handlers do, a page with the
// producing package's message, and web.ErrorFrom for htmx and the rest.
func (h *logoHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	if pr, ok := docscreen.Classify(err); ok && !ui.IsHTMX(r) {
		ui.Render(w, r, pr.Status, layout.Page(layout.PageOpts{Title: pr.Message, Error: true, Back: back()}), nil)
		return
	}
	web.ErrorFrom(w, r, err)
}

func back() *components.Link {
	return &components.Link{Label: "Company", Href: "/m/company/1"}
}

func (h *logoHandlers) page(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	return h.show(w, r, p, http.StatusOK, "")
}

// show renders the page, or the upload alone for htmx, with msg on the upload.
// FileUpload and Attachments post no field of ours, so the version and the
// logo's id travel in their URLs. A refused POST keeps the version it posted,
// as mstweb's forms do: only a reload moves it on, so a 409 shows msg in a
// banner with the Reload link. The upload's forms swap uploadArea, not the
// upload alone, so that banner reaches htmx too and the next swap replaces it.
func (h *logoHandlers) show(w http.ResponseWriter, r *http.Request, p auth.Principal, status int, msg string) error {
	ctx := r.Context()
	var c company.Company
	var logo files.Meta
	var hasLogo bool
	err := h.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if c, err = (companyService{svc: h.svc}).Get(ctx, tx, companyID); err != nil {
			return err
		}
		logo, hasLogo, err = h.store.Latest(ctx, tx, *companyRef(companyID))
		return err
	})
	if err != nil {
		return err
	}
	posted := c.Version
	if r.Method == http.MethodPost {
		posted, _ = postedVersion(r) // every POST checked it before it could be refused
	}
	version := "?version=" + strconv.Itoa(posted)
	props := components.UploadProps{
		ID: uploadID, Label: "Company logo", Action: logoPath + version, Hint: "PNG or JPG, up to 1 MB",
		Accept: ".png,.jpg,.jpeg", NoCamera: true, Target: "#" + uploadArea,
	}
	if hasLogo {
		id := strconv.FormatInt(logo.ID, 10)
		props.Files = []components.Attachment{{
			ID: logo.ID, Name: logo.Name, Size: files.HumanSize(logo.Size), UploadedBy: "Uploaded", UploadedAt: types.FormatIST(logo.UploadedAt),
			URL: "/files/" + id, RemoveURL: removePath + version + "&logo=" + id, Kind: "image",
		}}
	}
	var banner templ.Component = templ.NopComponent
	var errs []components.FieldErr
	switch {
	case status == http.StatusConflict:
		banner = components.Banner(components.BannerProps{
			Kind: components.Danger, Message: msg, Link: &components.Link{Label: "Reload", Href: logoPath},
		})
	case msg != "":
		props.Error = msg
		errs = []components.FieldErr{{ID: components.UploadInputID(uploadID), Message: msg}}
	}
	area := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<div id="`+uploadArea+`">`); err != nil {
			return err
		}
		if err := templ.Join(banner, components.FileUpload(props)).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, `</div>`)
		return err
	})
	body := templ.Join(components.ErrorSummary(errs), area)
	full := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		opts := layout.PageOpts{Title: "Company logo", Error: msg != "", Width: layout.Form, Back: back()}
		return layout.Page(opts).Render(templ.WithChildren(ctx, body), w)
	})
	ui.Render(w, r, status, full, area)
	return nil
}

// upload takes the one "file" part, spooled outside the transaction (CD-29),
// and swaps it for the stored logo in one db.Write. Over the limit the
// refusal is LogoPolicy's: Spool's own message is the 10 MB one, and an over-
// long body never reaches Attach. The body may already be parsed by AUTH's
// CSRF check, so this reads it as parsed, not as a stream.
func (h *logoHandlers) upload(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	version, err := postedVersion(r)
	if err != nil {
		return err
	}
	limit := files.LogoPolicy.MaxBytes + filesweb.MultipartOverhead
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	err = r.ParseMultipartForm(limit) //nolint:gosec // G120: the MaxBytesReader above bounds it
	var over *http.MaxBytesError
	if errors.As(err, &over) {
		return h.show(w, r, p, http.StatusUnprocessableEntity, files.LogoPolicy.SizeMessage)
	}
	if err != nil {
		return h.show(w, r, p, http.StatusUnprocessableEntity, msgNoFile)
	}
	if len(r.MultipartForm.File["file"]) > 1 {
		return h.show(w, r, p, http.StatusUnprocessableEntity, filesweb.MsgTwoFileParts)
	}
	f, head, err := r.FormFile("file")
	if err != nil {
		return h.show(w, r, p, http.StatusUnprocessableEntity, msgNoFile)
	}
	defer func() { _ = f.Close() }()
	sp, err := h.store.Spool(f, files.LogoPolicy.MaxBytes)
	if files.KindOf(err) == files.KindTooLarge {
		return h.show(w, r, p, http.StatusUnprocessableEntity, files.LogoPolicy.SizeMessage)
	}
	if err != nil {
		return err
	}
	defer func() { _ = sp.Cleanup() }()
	ctx := r.Context()
	err = h.env.DB.Write(ctx, func(tx *db.Tx) error {
		if err := checkVersion(ctx, tx, version); err != nil {
			return err
		}
		if err := h.clear(ctx, tx, p); err != nil {
			return err
		}
		_, err := h.store.Attach(ctx, tx, p, *companyRef(companyID), head.Filename, sp)
		return err
	})
	return h.done(w, r, p, err, "Logo saved.")
}

// remove takes the logo the page showed off the company. If another logo has
// taken its place, that one stays and the user is told to reload.
func (h *logoHandlers) remove(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	version, err := postedVersion(r)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("logo"), 10, 64)
	if err != nil {
		return auth.ErrBadForm
	}
	ctx := r.Context()
	err = h.env.DB.Write(ctx, func(tx *db.Tx) error {
		if err := checkVersion(ctx, tx, version); err != nil {
			return err
		}
		logo, ok, err := h.store.Latest(ctx, tx, *companyRef(companyID))
		if err != nil {
			return err
		}
		if !ok || logo.ID != id {
			return &auth.Problem{Code: http.StatusConflict, Msg: msgLogoChanged}
		}
		return h.clear(ctx, tx, p)
	})
	return h.done(w, r, p, err, "Logo removed.")
}

// checkVersion refuses a write at another company version than the page's
// (CD-94). An attachment has no version of its own and a logo change doesn't
// move the company's: this catches an edit of the company, and remove compares
// the logo's id for the rest. A replace is last write wins, and one logo stays.
func checkVersion(ctx context.Context, tx *db.Tx, version int) error {
	return mstkit.CheckVersion(ctx, tx, company.CompanyKind, companyID, version)
}

// clear removes every attachment of the company, in tx. files/web's generic
// upload route can add more than the one this page keeps.
func (h *logoHandlers) clear(ctx context.Context, tx *db.Tx, p auth.Principal) error {
	for {
		logo, ok, err := h.store.Latest(ctx, tx, *companyRef(companyID))
		if err != nil || !ok {
			return err
		}
		if err := h.store.Remove(ctx, tx, p, logo.ID); err != nil {
			return err
		}
	}
}

// done ends a write: the page again for a refusal it can show (stale 409,
// LogoPolicy's 422), else a success banner on the logo page.
func (h *logoHandlers) done(w http.ResponseWriter, r *http.Request, p auth.Principal, err error, msg string) error {
	var he doctype.HTTPError
	if errors.As(err, &he) && (he.Status() == http.StatusConflict || he.Status() == http.StatusUnprocessableEntity) {
		return h.show(w, r, p, he.Status(), he.Error())
	}
	if err != nil {
		return err
	}
	web.Flash(r.Context(), "success", msg)
	if ui.IsHTMX(r) {
		w.Header().Set("HX-Redirect", logoPath)
		return nil
	}
	http.Redirect(w, r, logoPath, http.StatusSeeOther)
	return nil
}

// postedVersion is the version in the form's URL; a missing or non-numeric
// one is AUTH's 400 form error (CD-146, CD-183).
func postedVersion(r *http.Request) (int, error) {
	v, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil {
		return 0, auth.ErrBadForm
	}
	return v, nil
}
