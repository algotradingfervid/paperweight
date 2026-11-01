package app

import (
	"bytes"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	approvalweb "github.com/vardhan/internaltools/internal/kernel/approval/web"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	filesweb "github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	namingweb "github.com/vardhan/internaltools/internal/kernel/naming/web"
	printweb "github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	gstinweb "github.com/vardhan/internaltools/internal/masters/gstin/web"
	"github.com/vardhan/internaltools/internal/masters/item"
	itemweb "github.com/vardhan/internaltools/internal/masters/item/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/expense"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	modkitweb "github.com/vardhan/internaltools/internal/modules/modkit/web"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	purchaseweb "github.com/vardhan/internaltools/internal/modules/purchase/web"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

const (
	// bodyLimit is the largest request body: one attachment plus its multipart
	// framing. It stays under net/http's 32 MiB form memory limit (CD-211).
	bodyLimit = files.MaxFileBytes + filesweb.MultipartOverhead

	homePath     = "/"
	setupPath    = "/setup"
	inboxPath    = "/approvals"
	searchPath   = "/search"
	newPath      = "/new"
	healthPath   = "/healthz"
	staticPrefix = "/static/"
	masterPrefix = "/m/"
	docPrefix    = "/doc/"

	noStoreValue = "private, no-store"
	hstsValue    = "max-age=31536000"

	msgMethodNotAllowed = "This page doesn't take that kind of request. Go back and try again."
)

// routes builds the router: AUTH's middleware through platform/web (CD-14,
// CD-150), the body ceiling before any route (CD-206, CD-211), then every
// area's routes, each behind its guard.
func (a *App) routes(sm *scs.SessionManager, namer *naming.Namer) chi.Router {
	r := web.NewRouter(web.Options{Logger: a.logger, Auth: a.srv.Middleware, TrustedOrigins: a.cfg.TrustedOrigins})
	r.Use(web.BodyCeiling(bodyLimit, files.MsgTooLarge))
	r.Use(a.hsts, a.firstRun, noStore)

	r.Method(http.MethodGet, healthPath, authweb.Public(http.HandlerFunc(a.healthz)))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r.Method(method, "/static/brand/*", brand.Static())
		r.Method(method, "/static/ui/*", http.StripPrefix("/static/ui/", ui.Static()))
		r.Method(method, "/static/*", web.Static())
	}

	r.Group(func(r chi.Router) {
		r.Use(ui.ShellMiddleware(a.shell), web.ProblemPages(problemPage))
		a.srv.Mount(r)
		// A path or method no route takes gets the app's own page (CD-250).
		// No route means no guard, so a guest gets it too: CD-17 is for
		// guarded routes, and an unmatched path is 404 for a guest too.
		r.NotFound(errorPage(http.StatusNotFound, auth.MsgNotFound))
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			allow(w, r)
			errorPage(http.StatusMethodNotAllowed, msgMethodNotAllowed)(w, r)
		})

		self := func(_ string, h http.Handler) http.Handler { return a.srv.Self(h) }
		r.Method(http.MethodGet, homePath, a.srv.Self(http.HandlerFunc(a.home)))
		r.Method(http.MethodGet, newPath, a.srv.Self(http.HandlerFunc(newPage)))
		for _, m := range []struct {
			method string
			fn     http.HandlerFunc
		}{{http.MethodGet, a.companyGet}, {http.MethodPost, a.companyPost}} {
			r.Method(m.method, companyPath, a.srv.Admin(msgCompanyAdminOnly, m.fn))
		}
		r.Method(http.MethodPost, "/ui/validate/{kind}", a.srv.Self(components.ValidateHandler(parsers())))

		gw := grnweb.Deps{Service: a.grn}
		pw := purchaseweb.Deps{
			Service: a.purchase, Approvals: a.approvals, Comments: a.comments, Sessions: sm,
			CSRF:        func(r *http.Request) string { return authweb.CSRFToken(r.Context()) },
			ReceiveLink: gw.ReceiveLink, // CD-283: purchase never imports goodsreceipt
			// The home asks the registered modules' Hide (CD-368 (3)); docDeps
			// is read per call, since the Modules are set below.
			ListHidden: func(r *http.Request, dt *doctype.DocType) func(string) bool { return a.docDeps().ListHidden(r, dt) },
		}
		a.receiveLink = pw.ReceiveLink
		a.docModules = purchaseweb.Modules(pw)
		a.docModules[expense.ClaimDocType] = expenseweb.ClaimModule(a.expenseWeb())
		a.docModules[expense.AdvanceDocType] = expenseweb.AdvanceModule(a.expenseWeb())
		maps.Copy(a.docModules, grnweb.Modules(gw))
		maps.Copy(a.docModules, a.cfg.Modules)
		docs := chi.NewRouter()
		docscreen.Routes(a.docDeps())(docs)
		mount(r, docs, func(route string, h http.Handler) http.Handler {
			if route == searchPath {
				return a.srv.Self(h) // outside /doc/, so never CheckedInHandler (CD-87)
			}
			return a.srv.CheckedInHandler(h)
		})
		r.Method(http.MethodGet, "/doc/{doctype}/export.csv", a.srv.CheckedInHandler(http.HandlerFunc(a.exportCSV)))

		apr := chi.NewRouter()
		approvalweb.Routes(approvalweb.Deps{Approvals: a.approvals, Layout: layout.Simple, DocURL: recordURL})(apr)
		mount(r, apr, self)

		namingweb.Mount(r, namingweb.Deps{Svc: namer, Admin: namer, Clock: a.cfg.Clock}, a.srv.Admin)

		modkit := chi.NewRouter()
		modkitweb.Routes(modkitweb.Deps{DB: a.db, Auth: a.auth, Audit: audit.Default, Clock: a.cfg.Clock, Info: a.engine, Sessions: sm})(modkit)
		mount(r, modkit, self)

		purchases := chi.NewRouter()
		purchaseweb.Routes(pw)(purchases)
		mount(r, purchases, self)
		grns := chi.NewRouter()
		grnweb.Routes(gw)(grns)
		mount(r, grns, func(route string, h http.Handler) http.Handler {
			dt, right, ok := grnweb.Guard(route)
			if !ok {
				panic("app: goodsreceipt route " + route + " has no guard")
			}
			return a.srv.Right(dt, right, h)
		})
		exp := chi.NewRouter()
		expenseweb.Routes(a.expenseWeb())(exp)
		mount(r, exp, func(route string, h http.Handler) http.Handler {
			dt, right, ok := expenseweb.Guard(route)
			if !ok {
				panic("app: expense route " + route + " has no guard")
			}
			return a.srv.Right(dt, right, h)
		})

		mount(r, printweb.Routes(printweb.Config{DB: a.db, Auth: a.auth, Info: a.engine, Letterhead: a.masters.Letterhead}), self)

		attachments := filesweb.Routes(filesweb.Config{Store: a.files, DB: a.db, Logger: a.logger})
		mount(r, attachments, self)
		// Any other path under /files/ reaches the files router's own 404.
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			r.Method(method, "/files/*", a.srv.Self(attachments))
		}

		masters := chi.NewRouter()
		a.masters.Routes(masters, allmasters.WebDeps{Sessions: sm, Files: a.files})
		a.screens = map[string]bool{}
		mount(r, masters, func(route string, h http.Handler) http.Handler {
			a.screens[route] = true
			return a.srv.Self(h)
		})
	})
	return r
}

// mount registers every route of sub on r behind guard, which receives the
// route's pattern. The area packages register plain handlers, because the
// guards are AUTH's and they don't import it (CD-46). Only the handler moves,
// so a middleware on sub would silently vanish: that stops the start instead.
func mount(r chi.Router, sub chi.Routes, guard func(route string, h http.Handler) http.Handler) {
	// The callback never fails, so neither does Walk.
	_ = chi.Walk(sub, func(method, route string, h http.Handler, mws ...func(http.Handler) http.Handler) error {
		if len(mws) != 0 {
			panic("app: " + method + " " + route + " has middleware, which mount would drop")
		}
		r.Method(method, route, guard(route, h))
		return nil
	})
}

// errorPage answers status with msg: a full-page GET gets the error page in
// the shell, as a missing document does; htmx and other methods get web.Error.
func errorPage(status int, msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || ui.IsHTMX(r) {
			web.Error(w, r, status, msg)
			return
		}
		problemPage(w, r, status, msg)
	}
}

// problemPage is web.Error's full-page GET 403 or 404: the error page in the
// shell, where ui.Render shows the problem page (UI v2 §5.14, E6b).
func problemPage(w http.ResponseWriter, r *http.Request, status int, msg string) {
	ui.Render(w, r, status, layout.Page(layout.PageOpts{Title: msg, Error: true}), nil)
}

// allow sets the 405's Allow header, which chi sets only in its own handler.
// It matches the path chi routed on: RawPath when set.
func allow(w http.ResponseWriter, r *http.Request) {
	routes := chi.RouteContext(r.Context()).Routes
	path := r.URL.Path
	if r.URL.RawPath != "" {
		path = r.URL.RawPath
	}
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if routes.Match(chi.NewRouteContext(), m, path) {
			w.Header().Add("Allow", m)
		}
	}
}

// parsers is every kind /ui/validate and the forms accept (CD-209, CD-212).
func parsers() map[string]components.ParseFunc {
	out := components.NUMParsers()
	maps.Copy(out, gstinweb.Parsers())
	maps.Copy(out, itemweb.Parsers())
	return out
}

// DocDeps is the generic document screens' Deps as routes built them.
func (a *App) DocDeps() docscreen.Deps { return a.docDeps() }

// docDeps is what the generic document screens read from (CD-78, CD-200).
func (a *App) docDeps() docscreen.Deps {
	return docscreen.Deps{
		DB:       a.db,
		Engine:   a.engine,
		Auth:     a.auth,
		Approval: a.approvals,
		Lister:   lister{svc: &list.Service{DB: a.db, Auth: a.auth}, db: a.db, engine: a.engine},
		Extras:   extras{app: a},
		Today: func() (string, string) {
			today := types.Today(a.cfg.Clock)
			return types.FormatDate(today), types.FormatDate(types.AddDays(today, -1))
		},
		InboxURL:  inboxPath,
		RecordURL: recordURL,
		ListURL:   listURL,
		Parse:     parsers(),
		// A posted link shows its target's label only to a reader (CD-233).
		LinkRead: mstkit.Env{Auth: a.auth}.CanLinkTargetTx,
		// A document link shows its number only to a reader (CD-321).
		DocLinkRead: a.auth.CheckLinkTx,
		Modules:     a.docModules,
	}
}

// expenseWeb is what the Expense screens and module routes need.
func (a *App) expenseWeb() expenseweb.Deps {
	return expenseweb.Deps{Service: a.expense, Engine: a.engine, Auth: a.auth, Files: a.files, Employee: a.masters.Employee}
}

// pickerURL is a master's picker search (CD-194, CD-195).
func pickerURL(dt *doctype.DocType) string { return "/pick/" + url.PathEscape(dt.Name) }

// masterPath is a master's list path, from its registry name.
func masterPath(name string) string {
	return masterPrefix + strings.ReplaceAll(strings.ToLower(name), " ", "-")
}

// listURL is a DocType's list: masters live under /m/ (CD-200).
func listURL(name string) string {
	if dt, ok := doctype.Lookup(name); ok && dt.IsMaster() {
		return masterPath(name)
	}
	return docPrefix + name
}

// recordURL is one record's page.
func recordURL(name string, id int64) string {
	return listURL(name) + "/" + strconv.FormatInt(id, 10)
}

// hsts tells browsers to use HTTPS only, on the public host.
func (a *App) hsts(next http.Handler) http.Handler {
	if !a.cfg.Secure {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", hstsValue)
		next.ServeHTTP(w, r)
	})
}

// firstRun closes /setup on the public host, where nobody on the internet may
// claim an install; elsewhere every request but /setup, the static files and
// /healthz goes to /setup until a user exists (CD-228, CD-231).
func (a *App) firstRun(next http.Handler) http.Handler {
	// The closed /setup runs ahead of the routes, so it takes the shell and
	// the problem page itself (E6b); the static files never pay for the shell.
	closed := ui.ShellMiddleware(a.shell)(web.ProblemPages(problemPage)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.srv.WriteError(w, r, auth.ErrNotFound)
	})))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case a.cfg.Secure:
			if path == setupPath {
				closed.ServeHTTP(w, r)
				return
			}
		case path == setupPath, path == healthPath, strings.HasPrefix(path, staticPrefix):
		case !a.installed.Load():
			done, err := a.auth.SetupDone(r.Context())
			if err != nil {
				a.srv.WriteError(w, r, err)
				return
			}
			if !done {
				http.Redirect(w, r, setupPath, http.StatusSeeOther)
				return
			}
			a.installed.Store(true)
		}
		next.ServeHTTP(w, r)
	})
}

// noStore keeps signed-in responses out of every cache, so Back after sign-out
// on a shared computer shows nothing (CD-225). A handler that sets its own
// header replaces this one.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.PrincipalFrom(r.Context()); ok {
			w.Header().Set("Cache-Control", noStoreValue)
		}
		next.ServeHTTP(w, r)
	})
}

// healthz answers 200 when the database answers (CD-198).
func (a *App) healthz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var tables int
		return tx.SQL().QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&tables)
	})
	if err != nil {
		a.logger.ErrorContext(ctx, "health check failed", "err", err)
		web.Error(w, r, http.StatusServiceUnavailable, "The database isn't answering.")
		return
	}
	_, _ = w.Write([]byte("ok"))
}

// home is the signed-in `/`: SafeNext and the header brand link land here. An
// Administrator with no company finishes setup first; everyone else, and
// everyone after that, goes to the inbox, which opens for every signed-in user
// (CD-231).
func (a *App) home(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	if slices.Contains(p.Roles, auth.RoleAdministrator) {
		exists, err := a.companyExists(r.Context())
		if err != nil {
			a.srv.WriteError(w, r, err)
			return
		}
		if !exists {
			http.Redirect(w, r, companyPath, http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, inboxPath, http.StatusSeeOther)
}

// exportCSV serves GET /doc/{doctype}/export.csv: no read is the read message,
// read without export is the export message, which Export gives (CD-145).
func (a *App) exportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dt, ok := doctype.Lookup(chi.URLParam(r, "doctype"))
	if !ok {
		a.srv.WriteError(w, r, auth.ErrNotFound)
		return
	}
	p, _ := auth.PrincipalFrom(ctx)
	if err := a.auth.Can(ctx, p, dt.Name, auth.RightRead, nil); err != nil {
		a.srv.WriteError(w, r, err)
		return
	}
	// The list's Hide answer leaves the hidden fields' columns, filters and
	// sort out of the export too (CD-334 (3), (4)).
	hidden := a.docDeps().ListHidden(r, dt)
	q, err := listQuery(dt, docscreen.ListedQuery(dt, docscreen.ParseListQuery(r.URL.Query()), hidden))
	if err != nil {
		a.srv.WriteError(w, r, err)
		return
	}
	ex := csvio.NewExporter(a.db, a.auth).Without(func(f doctype.Field) bool { return hidden(f.Name) })
	var buf bytes.Buffer
	if _, err := ex.Export(ctx, &buf, p, dt, q); err != nil {
		a.srv.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+dt.Name+`.csv"`)
	_, _ = w.Write(buf.Bytes()) // the client is gone; nothing can tell it
}

// shell is what the app shell shows around this request's page (CD-194): a
// guest gets the brand alone. A failed nav or inbox read must not fail the
// page, so it is logged and the shell goes without.
func (a *App) shell(r *http.Request) ui.Shell {
	ctx := r.Context()
	s := ui.ShellFrom(ctx)
	s.Path = r.URL.Path
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return s
	}
	s.SignedIn, s.User, s.CSRF = true, p.Name, authweb.CSRFToken(ctx)
	nav, err := a.nav(r, p)
	if err != nil {
		a.logger.ErrorContext(ctx, "shell: navigation", "user", p.ID, "err", err)
	}
	s.Nav = nav
	s.New, err = a.newItems(r, p)
	if err != nil {
		a.logger.ErrorContext(ctx, "shell: new items", "user", p.ID, "err", err)
	}
	inbox, err := a.approvals.Inbox(ctx, p, types.Page{Number: 1, Size: approvalPageSize})
	if err != nil {
		a.logger.ErrorContext(ctx, "shell: inbox count", "user", p.ID, "err", err)
	}
	s.InboxCount = inbox.Total
	return s
}

// approvalPageSize is a page size the inbox accepts; the count is its Total.
const approvalPageSize = 25

// Navigation groups, in sidebar order (UI v2 §5.2, N18). groupDocuments
// holds only a DocType docNav doesn't place: the test-only ones.
const (
	groupWork      = "Work"
	groupPurchase  = "Purchase"
	groupGRN       = "Goods receipt"
	groupExpenses  = "Expenses"
	groupDocuments = "Documents"
	groupMasters   = "Masters"
	groupAdmin     = "Admin"
)

// purchaseHomePath is the Purchase home, the nav entry for anyone who may read
// purchase.Order or purchase.Request (MOD-PO P1 Do 4).
const purchaseHomePath = "/modules/purchase"

// navPlace is a DocType's group, icon and nav-only label in the sidebar
// (CD-349 (2)); more puts it under the group's More disclosure. newLabel,
// when set, lists it in the "Create new" sheet and /new (UI v2 §5.2, N2).
type navPlace struct {
	name, group, icon, label, newLabel string
	more                               bool
}

// docNav places every DocType the sidebar lists, in sidebar order (UI v2
// §5.2; DL §4.6). TestNav_EveryDocTypeHasIcon fails on a missing one.
var docNav = []navPlace{
	{purchase.DocType, groupPurchase, "shopping-cart", "Purchase orders", "Purchase order", false},
	{purchase.RequestDocType, groupPurchase, "clipboard-list", "Purchase requests", "Purchase request", false},
	{goodsreceipt.DocType, groupGRN, "truck", "Goods receipts", "Goods receipt", false},
	{expense.ClaimDocType, groupExpenses, "receipt-indian-rupee", "Expense claims", "Expense claim", false},
	{expense.AdvanceDocType, groupExpenses, "hand-coins", "Advances", "Advance request", false},
	{party.DocType, groupMasters, "handshake", "Parties", "Party", false},
	{item.DocType, groupMasters, "package", "Items", "", false},
	{project.DocType, groupMasters, "folder-kanban", "Projects", "", false},
	{site.DocType, groupMasters, "map-pin", "Sites", "", false},
	{employee.DocType, groupMasters, "id-card", "Employees", "", false},
	{company.DocType, groupMasters, "store", "Branches", "", true},
	{company.CompanyDocType, groupMasters, "building-2", "Companies", "", true},
	{expensetype.DocType, groupMasters, "tags", "Expense types", "", true},
	{paymentterms.DocType, groupMasters, "calendar-clock", "Payment terms", "", true},
	{taxcode.DocType, groupMasters, "percent", "Tax codes", "", true},
	{uom.DocType, groupMasters, "ruler", "Units", "", true},
}

// docNavIndex is name's index in docNav, or -1.
func docNavIndex(name string) int {
	return slices.IndexFunc(docNav, func(p navPlace) bool { return p.name == name })
}

// nav is p's navigation: the two approval pages, the Purchase home, one item
// per DocType p may read in docNav's order (any other DocType after them, in
// Documents or Masters), then the administrator's pages. The rights are read
// in one transaction, through CanTx.
func (a *App) nav(r *http.Request, p auth.Principal) ([]ui.NavItem, error) {
	ctx := r.Context()
	items := []ui.NavItem{
		{Label: "Approvals", Href: inboxPath, Icon: "inbox", Group: groupWork},
		{Label: "My requests", Href: inboxPath + "/mine", Icon: "file-clock", Group: groupWork},
	}
	placed := make([]*ui.NavItem, len(docNav))
	var docs, masters []ui.NavItem
	purchases := false
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		for _, dt := range doctype.All() {
			href := listURL(dt.Name)
			if dt.IsMaster() && !a.screens[href] {
				continue // its screens aren't built yet
			}
			err := a.auth.CanTx(ctx, tx, p, dt.Name, auth.RightRead, nil)
			if errors.Is(err, auth.ErrForbidden) {
				continue
			}
			if err != nil {
				return err
			}
			purchases = purchases || dt.Name == purchase.DocType || dt.Name == purchase.RequestDocType
			it := ui.NavItem{Label: dt.PluralLabel, Href: href}
			switch i := docNavIndex(dt.Name); {
			case i >= 0:
				it.Label, it.Group, it.Icon, it.More = docNav[i].label, docNav[i].group, docNav[i].icon, docNav[i].more
				placed[i] = &it
			case dt.IsMaster():
				it.Group, it.Icon = groupMasters, "file"
				masters = append(masters, it)
			default:
				it.Group, it.Icon = groupDocuments, "file-text"
				docs = append(docs, it)
			}
		}
		return nil
	})
	if purchases {
		items = append(items, ui.NavItem{Label: "Purchase home", Href: purchaseHomePath, Icon: "layout-dashboard", Group: groupPurchase})
	}
	for i, it := range placed {
		if it != nil && docNav[i].group != groupMasters {
			items = append(items, *it)
		}
	}
	items = append(items, docs...)
	for i, it := range placed {
		if it != nil && docNav[i].group == groupMasters {
			items = append(items, *it)
		}
	}
	items = append(items, masters...)
	if slices.Contains(p.Roles, auth.RoleAdministrator) {
		items = append(items,
			ui.NavItem{Label: "Users", Href: "/users", Icon: "users", Group: groupAdmin},
			ui.NavItem{Label: "Roles", Href: "/roles", Icon: "shield", Group: groupAdmin},
			ui.NavItem{Label: "Approval rules", Href: "/admin/approval-rules", Icon: "list-checks", Group: groupAdmin},
			ui.NavItem{Label: "Numbering", Href: "/settings/numbering", Icon: "hash", Group: groupAdmin},
			ui.NavItem{Label: "Business settings", Href: "/settings/business", Icon: "settings", Group: groupAdmin},
		)
	}
	return items, err
}

// newItems is what p may create, for the "Create new" sheet and /new: each
// docNav place with a newLabel whose DocType p holds create on, in docNav's
// order.
func (a *App) newItems(r *http.Request, p auth.Principal) ([]ui.NavItem, error) {
	ctx := r.Context()
	var items []ui.NavItem
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		for _, place := range docNav {
			if place.newLabel == "" {
				continue
			}
			err := a.auth.CanTx(ctx, tx, p, place.name, auth.RightCreate, nil)
			if errors.Is(err, auth.ErrForbidden) {
				continue
			}
			if err != nil {
				return err
			}
			items = append(items, ui.NavItem{Label: place.newLabel, Href: listURL(place.name) + "/new", Icon: place.icon})
		}
		return nil
	})
	return items, err
}
