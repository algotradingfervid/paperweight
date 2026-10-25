package webtest

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// probeSlugs maps the probe's URL slugs to the authtest DocTypes.
var probeSlugs = []struct{ slug, doctype string }{
	{"po", authtest.DTOrder}, {"grn", authtest.DTReceipt}, {"expense", authtest.DTClaim},
	{"item", authtest.DTItem}, {"party", authtest.DTParty}, {"site", authtest.DTSite}, {"project", authtest.DTProject},
}

// probeActions are the lifecycle actions behind POST /{s}/{id}/{action}.
var probeActions = []doctype.Action{
	doctype.ActionSubmit, doctype.ActionWithdraw, doctype.ActionApprove, doctype.ActionReject,
	doctype.ActionRevise, doctype.ActionClose, doctype.ActionCancel, doctype.ActionAmend, doctype.ActionDelete,
}

// errBadInput answers a form or CSV the probe can't read.
var errBadInput = &auth.Problem{Code: http.StatusBadRequest, Msg: "The form could not be read. Reload the page and try again."}

// WithProbe mounts the probe router: a reference implementation of every
// channel 01-auth exercises, built only from AUTH's contract (Can, Scope,
// CheckValues, CheckLink, UsersWithRight, RightFor) and the guards. WP3
// replaces it with the real router.
func WithProbe() Option { return func(o *options) { o.probe = true } }

// probe serves one DocType.
type probe struct {
	w    *authtest.World
	srv  *authweb.Server
	dt   *doctype.DocType
	slug string
}

func mountProbe(t testing.TB, r chi.Router, srv *authweb.Server, w *authtest.World) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, attachmentTable)
		return err
	}); err != nil {
		t.Fatalf("webtest.WithProbe: %v", err)
	}
	var probes []*probe
	for _, s := range probeSlugs {
		dt, ok := doctype.Lookup(s.doctype)
		if !ok {
			t.Fatalf("webtest.WithProbe: DocType %s is not registered", s.doctype)
		}
		p := &probe{w: w, srv: srv, dt: dt, slug: s.slug}
		probes = append(probes, p)
		p.mount(r)
	}
	r.Method(http.MethodGet, "/search", srv.Self(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		search(rw, req, srv, probes)
	})))
	r.Method(http.MethodGet, "/files/{id}", srv.Self(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		file(rw, req, srv, w)
	})))
}

func (p *probe) mount(r chi.Router) {
	base, name := "/"+p.slug, p.dt.Name
	route := func(method, path string, right auth.Right, h http.HandlerFunc) {
		r.Method(method, path, p.srv.Right(name, right, h))
	}
	route(http.MethodGet, base+"/list", auth.RightRead, p.list)
	route(http.MethodGet, base+"/count", auth.RightRead, p.count)
	route(http.MethodGet, base+"/picker", auth.RightRead, p.picker)
	route(http.MethodGet, base+"/new", auth.RightCreate, p.newForm)
	route(http.MethodPost, base, auth.RightCreate, p.create)
	route(http.MethodGet, base+"/{id}", auth.RightRead, p.show)
	route(http.MethodGet, "/api"+base+"/{id}", auth.RightRead, p.showJSON)
	route(http.MethodGet, "/api"+base, auth.RightRead, p.listJSON)
	route(http.MethodGet, base+"/{id}/approvers", auth.RightRead, p.approvers)
	route(http.MethodPost, base+"/{id}/save", auth.RightWrite, p.save)
	for _, a := range probeActions {
		right, err := auth.RightFor(a, false)
		if err != nil {
			panic(err)
		}
		route(http.MethodPost, base+"/{id}/"+string(a), right, p.action(a, right))
	}
	if p.dt.LinesTable != "" {
		route(http.MethodPost, base+"/{id}/lines", auth.RightWrite, p.addLine)
		route(http.MethodPost, base+"/{id}/lines/{line}/delete", auth.RightWrite, p.deleteLine)
	}
	route(http.MethodGet, base+"/{id}/print", auth.RightPrint, p.print)
	route(http.MethodGet, base+"/{id}/pdf", auth.RightPrint, p.pdf)
	// Guarded by read, then Scope(export): a user without read gets the
	// read message (AUTH-064), one with read but no export the export one.
	route(http.MethodGet, base+"/export.csv", auth.RightRead, p.export)
	route(http.MethodPost, base+"/import", auth.RightImport, p.importCSV)
}

// q quotes table.column like auth's predicates (CD-16).
func q(table, column string) string { return `"` + table + `"."` + column + `"` }

func principal(r *http.Request) auth.Principal {
	pr, _ := auth.PrincipalFrom(r.Context())
	return pr
}

// numberCol is the column searched and shown: code on masters.
func (p *probe) numberCol() string {
	if p.dt.Master != nil {
		return "code"
	}
	return "number"
}

// display is the shown text: the code, the number or "Draft {id}".
func (p *probe) display() string {
	if p.dt.Master != nil {
		return q(p.dt.Table, "code")
	}
	return "coalesce(" + q(p.dt.Table, "number") + ", 'Draft ' || " + q(p.dt.Table, "id") + ")"
}

type row struct {
	ID     int64  `json:"id"`
	Number string `json:"number"`
	amount int64
}

// rows selects p's rows matching every clause, scoped by pred.
func (p *probe) rows(ctx context.Context, pred auth.Predicate, clauses []string, args []any, order string) ([]row, error) {
	t := p.dt.Table
	if pred.SQL != "" {
		clauses = append([]string{"(" + pred.SQL + ")"}, clauses...)
		args = append(append([]any(nil), pred.Args...), args...)
	}
	amount := "0"
	if p.dt.Master == nil {
		amount = q(t, "amount")
	}
	query := `SELECT ` + q(t, "id") + `, ` + p.display() + `, ` + amount + ` FROM "` + t + `"`
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, " AND ")
	}
	if order == "" {
		order = q(t, "id")
	}
	query += ` ORDER BY ` + order + `, ` + q(t, "id")
	var out []row
	err := p.w.DB.Read(ctx, func(tx *db.Tx) error {
		rs, err := tx.SQL().QueryContext(ctx, query, args...) //nolint:gosec // G202: columns come from the DocType and a sort whitelist; values are ? arguments
		if err != nil {
			return err
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var rw row
			if err := rs.Scan(&rw.ID, &rw.Number, &rw.amount); err != nil {
				return err
			}
			out = append(out, rw)
		}
		return rs.Err()
	})
	return out, err
}

// dimFilter matches rows whose fields (header or "lines.<col>") hold the
// master record with code; "" when dt has no such fields.
func (p *probe) dimFilter(fields []string, master, code string) (string, []any) {
	t := p.dt.Table
	in := ` IN (SELECT id FROM ` + master + ` WHERE code = ?)`
	var parts []string
	var args []any
	for _, f := range fields {
		if c, ok := strings.CutPrefix(f, "lines."); ok {
			parts = append(parts, `EXISTS (SELECT 1 FROM "`+p.dt.LinesTable+`" WHERE `+q(p.dt.LinesTable, "parent_id")+` = `+q(t, "id")+` AND `+q(p.dt.LinesTable, c)+in+`)`)
		} else {
			parts = append(parts, q(t, f)+in)
		}
		args = append(args, code)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// filters builds the user filters site, project (codes) and created_by.
func (p *probe) filters(r *http.Request) ([]string, []any, error) {
	var clauses []string
	var args []any
	add := func(c string, a []any) {
		if c != "" {
			clauses = append(clauses, c)
			args = append(args, a...)
		}
	}
	if code := r.URL.Query().Get("site"); code != "" {
		add(p.dimFilter(p.dt.SiteFields, "probe_site", code))
	}
	if code := r.URL.Query().Get("project"); code != "" {
		add(p.dimFilter(p.dt.ProjectFields, "probe_project", code))
	}
	if v := r.URL.Query().Get("created_by"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, nil, errBadInput
		}
		add(q(p.dt.Table, p.dt.OwnerField)+" = ?", []any{id})
	}
	return clauses, args, nil
}

// sortColumn whitelists the sort keys number, site and created_at.
func (p *probe) sortColumn(key string) string {
	switch {
	case key == "number":
		return q(p.dt.Table, p.numberCol())
	case p.dt.Master != nil:
		return ""
	case key == "site":
		return q(p.dt.Table, "site_id")
	case key == "created_at":
		return q(p.dt.Table, "created_at")
	}
	return ""
}

// scopedRows answers err itself and returns false on failure.
func (p *probe) scopedRows(w http.ResponseWriter, r *http.Request, right auth.Right, clauses []string, args []any, order string) ([]row, bool) {
	pred, err := p.w.Auth.Scope(r.Context(), principal(r), p.dt.Name, right)
	if err == nil {
		var rs []row
		if rs, err = p.rows(r.Context(), pred, clauses, args, order); err == nil {
			return rs, true
		}
	}
	p.srv.WriteError(w, r, err)
	return nil, false
}

// writeText answers body; an HTML body is escaped.
func writeText(w http.ResponseWriter, contentType, body string) {
	if strings.HasPrefix(contentType, "text/html") {
		body = html.EscapeString(body)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(body)) //nolint:gosec // G705: fixed content types, HTML escaped above, nosniff
}

func (p *probe) list(w http.ResponseWriter, r *http.Request) {
	clauses, args, err := p.filters(r)
	if err != nil {
		p.srv.WriteError(w, r, err)
		return
	}
	rs, ok := p.scopedRows(w, r, auth.RightRead, clauses, args, p.sortColumn(r.URL.Query().Get("sort")))
	if !ok {
		return
	}
	var b strings.Builder
	var total int64
	for _, rw := range rs {
		b.WriteString(rw.Number + "\n")
		total += rw.amount
	}
	fmt.Fprintf(&b, "Count: %d\nTotal: %d\n", len(rs), total)
	writeText(w, "text/plain; charset=utf-8", b.String())
}

func (p *probe) count(w http.ResponseWriter, r *http.Request) {
	if rs, ok := p.scopedRows(w, r, auth.RightRead, nil, nil, ""); ok {
		writeText(w, "text/plain; charset=utf-8", fmt.Sprintf("Count: %d\n", len(rs)))
	}
}

func (p *probe) picker(w http.ResponseWriter, r *http.Request) {
	clauses := []string{q(p.dt.Table, p.numberCol()) + " LIKE ?"}
	args := []any{"%" + r.URL.Query().Get("q") + "%"}
	if p.dt.Master != nil {
		clauses = append(clauses, q(p.dt.Table, "enabled")+" = 1")
	}
	if rs, ok := p.scopedRows(w, r, auth.RightRead, clauses, args, ""); ok {
		writeText(w, "text/plain; charset=utf-8", lines(rs))
	}
}

func lines(rs []row) string {
	var b strings.Builder
	for _, rw := range rs {
		b.WriteString(rw.Number + "\n")
	}
	return b.String()
}

func (p *probe) newForm(w http.ResponseWriter, _ *http.Request) {
	writeText(w, "text/html; charset=utf-8", "New "+p.dt.Label)
}

func (p *probe) listJSON(w http.ResponseWriter, r *http.Request) {
	clauses, args, err := p.filters(r)
	if err != nil {
		p.srv.WriteError(w, r, err)
		return
	}
	rs, ok := p.scopedRows(w, r, auth.RightRead, clauses, args, "")
	if !ok {
		return
	}
	if rs == nil {
		rs = []row{}
	}
	writeJSON(w, rs)
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	writeText(w, "application/json", string(b))
}

func search(w http.ResponseWriter, r *http.Request, srv *authweb.Server, probes []*probe) {
	ctx, pr := r.Context(), principal(r)
	var out []row
	for _, p := range probes {
		err := p.w.Auth.Can(ctx, pr, p.dt.Name, auth.RightRead, nil)
		if errors.Is(err, auth.ErrForbidden) {
			continue
		}
		var pred auth.Predicate
		if err == nil {
			pred, err = p.w.Auth.Scope(ctx, pr, p.dt.Name, auth.RightRead)
		}
		var rs []row
		if err == nil {
			rs, err = p.rows(ctx, pred, []string{q(p.dt.Table, p.numberCol()) + " LIKE ?"}, []any{"%" + r.URL.Query().Get("q") + "%"}, "")
		}
		if err != nil {
			srv.WriteError(w, r, err)
			return
		}
		out = append(out, rs...)
	}
	writeText(w, "text/plain; charset=utf-8", lines(out))
}

// record loads {id} and checks Can(right) on it; it answers the error itself
// and returns false on failure.
func (p *probe) record(w http.ResponseWriter, r *http.Request, right auth.Right) (doctype.Info, bool) {
	ctx := r.Context()
	info, err := p.info(ctx, chi.URLParam(r, "id"))
	if err == nil {
		ref := refOf(info)
		err = p.w.Auth.Can(ctx, principal(r), p.dt.Name, right, &ref)
	}
	if err != nil {
		p.srv.WriteError(w, r, err)
		return doctype.Info{}, false
	}
	return info, true
}

func (p *probe) info(ctx context.Context, idText string) (doctype.Info, error) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return doctype.Info{}, auth.ErrNotFound
	}
	var info doctype.Info
	err = p.w.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		info, err = p.w.Info.Info(ctx, tx, p.dt.Name, id)
		return err
	})
	if errors.Is(err, auth.ErrNotFound) {
		return doctype.Info{}, auth.ErrNotFound
	}
	return info, err
}

func (p *probe) show(w http.ResponseWriter, r *http.Request) {
	if info, ok := p.record(w, r, auth.RightRead); ok {
		writeText(w, "text/html; charset=utf-8", p.dt.Label+" "+info.Display)
	}
}

func (p *probe) showJSON(w http.ResponseWriter, r *http.Request) {
	if info, ok := p.record(w, r, auth.RightRead); ok {
		writeJSON(w, map[string]string{"number": info.Display})
	}
}

func (p *probe) approvers(w http.ResponseWriter, r *http.Request) {
	info, ok := p.record(w, r, auth.RightRead)
	if !ok {
		return
	}
	ref := refOf(info)
	ids, err := p.w.Auth.UsersWithRight(r.Context(), p.dt.Name, auth.RightApprove, &ref)
	if err != nil {
		p.srv.WriteError(w, r, err)
		return
	}
	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "%d\n", id)
	}
	writeText(w, "text/plain; charset=utf-8", b.String())
}

// formIDs parses the posted IDs of key; empty values are skipped.
func formIDs(r *http.Request, key string) ([]int64, error) {
	var out []int64
	for _, v := range r.PostForm[key] {
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, errBadInput
		}
		out = append(out, id)
	}
	return out, nil
}

// formID is the first posted ID of key, or 0.
func formID(r *http.Request, key string) (int64, error) {
	ids, err := formIDs(r, key)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

func (p *probe) writeTx(ctx context.Context, fn func(q db.DBTX, tx *db.Tx) error) error {
	return p.w.DB.Write(ctx, func(tx *db.Tx) error { return fn(tx.SQL(), tx) })
}

func (p *probe) create(w http.ResponseWriter, r *http.Request) {
	id, err := p.insert(r)
	if err != nil {
		p.srv.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/%d", p.slug, id), http.StatusSeeOther) //nolint:gosec // G710: a probe slug constant and an int64
}

// insert checks the posted links and stores the new record. The owner
// fields are the server's (the guard strips posted ones); read-only fields
// (number, amount) are ignored (CD-79).
func (p *probe) insert(r *http.Request) (int64, error) {
	ctx, pr, dt := r.Context(), principal(r), p.dt
	if err := r.ParseForm(); err != nil {
		return 0, errBadInput
	}
	var id int64
	if dt.Master != nil {
		err := p.writeTx(ctx, func(q db.DBTX, _ *db.Tx) error {
			return q.QueryRowContext(ctx,
				`INSERT INTO `+dt.Table+` (code, name, created_by) VALUES (?, ?, ?) RETURNING id`, //nolint:gosec // G202: table is a probe constant
				r.PostFormValue("code"), r.PostFormValue("name"), int64(pr.ID)).Scan(&id)
		})
		return id, err
	}
	posted := map[string][]int64{}
	for _, key := range []string{"employee_id", "project_id", "site_id", "line_site_id", "line_project_id", "purchase_order_id"} {
		ids, err := formIDs(r, key)
		if err != nil {
			return 0, err
		}
		posted[key] = ids
	}
	values := map[string][]int64{}
	for _, f := range append(append([]string(nil), dt.ProjectFields...), dt.SiteFields...) {
		values[f] = posted[strings.TrimPrefix(f, "lines.")]
	}
	if err := p.w.Auth.CheckValues(ctx, pr, dt.Name, values); err != nil {
		return 0, err
	}
	if po := posted["purchase_order_id"]; len(po) > 0 {
		if err := p.w.Auth.CheckLink(ctx, pr, authtest.DTOrder, po[0]); err != nil {
			return 0, err
		}
	}
	first := func(key string) int64 {
		if ids := posted[key]; len(ids) > 0 {
			return ids[0]
		}
		return 0
	}
	var delivery any
	if v := r.PostFormValue("delivery_date"); v != "" {
		delivery = v
	}
	err := p.writeTx(ctx, func(q db.DBTX, tx *db.Tx) error {
		emp := first("employee_id")
		if emp == 0 {
			var err error
			if emp, _, err = p.w.Dir.EmployeeForUser(ctx, tx, pr.ID); err != nil {
				return err
			}
		}
		if err := q.QueryRowContext(ctx,
			`INSERT INTO `+dt.Table+` (state, created_by, created_at, employee_id, project_id, site_id, delivery_date) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, //nolint:gosec // G202: table is a probe constant
			string(doctype.StateDraft), int64(pr.ID), auth.FormatTime(p.w.Clock.Now()),
			nullID(emp), nullID(first("project_id")), nullID(first("site_id")), delivery).Scan(&id); err != nil {
			return err
		}
		return insertLines(ctx, q, dt, id, posted["line_site_id"], posted["line_project_id"])
	})
	return id, err
}

func (p *probe) save(w http.ResponseWriter, r *http.Request) {
	info, ok := p.record(w, r, auth.RightWrite)
	if !ok {
		return
	}
	ctx := r.Context()
	// r.PostForm is filled only by the guard (ParseForm) for urlencoded bodies.
	if v, posted := r.PostForm["delivery_date"]; posted && p.dt.Master == nil {
		if err := p.writeTx(ctx, func(q db.DBTX, _ *db.Tx) error {
			_, err := q.ExecContext(ctx, `UPDATE `+p.dt.Table+` SET delivery_date = ? WHERE id = ?`, v[0], info.ID) //nolint:gosec // G202: table is a probe constant
			return err
		}); err != nil {
			p.srv.WriteError(w, r, err)
			return
		}
	}
	writeText(w, "text/html; charset=utf-8", "Saved "+info.Display)
}

// nextState is the state an action moves to in the probe; others keep it.
var nextState = map[doctype.Action]doctype.State{
	doctype.ActionSubmit:  doctype.StatePending,
	doctype.ActionApprove: doctype.StateApproved,
	doctype.ActionCancel:  doctype.StateCancelled,
}

func (p *probe) action(a doctype.Action, right auth.Right) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, ok := p.record(w, r, right)
		if !ok {
			return
		}
		ctx, t, id := r.Context(), p.dt.Table, info.ID
		loc := fmt.Sprintf("/%s/%d", p.slug, id)
		err := p.writeTx(ctx, func(q db.DBTX, _ *db.Tx) error {
			var err error
			switch a {
			case doctype.ActionAmend:
				var amended int64
				if err := q.QueryRowContext(ctx,
					`INSERT INTO `+t+` (state, created_by, created_at, employee_id, project_id, site_id, amount, delivery_date) SELECT ?, ?, ?, employee_id, project_id, site_id, amount, delivery_date FROM `+t+` WHERE id = ? RETURNING id`, //nolint:gosec // G202: table is a probe constant
					string(doctype.StateDraft), int64(principal(r).ID), auth.FormatTime(p.w.Clock.Now()), id).Scan(&amended); err != nil {
					return err
				}
				_, err = q.ExecContext(ctx,
					`INSERT INTO `+p.dt.LinesTable+` (parent_id, line_project_id, line_site_id) SELECT ?, line_project_id, line_site_id FROM `+p.dt.LinesTable+` WHERE parent_id = ? ORDER BY id`, //nolint:gosec // G202: table is a probe constant
					amended, id)
				loc = fmt.Sprintf("/%s/%d", p.slug, amended)
			case doctype.ActionDelete:
				if p.dt.LinesTable != "" {
					if _, err := q.ExecContext(ctx, `DELETE FROM `+p.dt.LinesTable+` WHERE parent_id = ?`, id); err != nil { //nolint:gosec // G202: table is a probe constant
						return err
					}
				}
				_, err = q.ExecContext(ctx, `DELETE FROM `+t+` WHERE id = ?`, id) //nolint:gosec // G202: table is a probe constant
				loc = "/" + p.slug + "/list"
			default:
				if s, ok := nextState[a]; ok {
					_, err = q.ExecContext(ctx, `UPDATE `+t+` SET state = ? WHERE id = ?`, string(s), id) //nolint:gosec // G202: table is a probe constant
				}
			}
			return err
		})
		if err != nil {
			p.srv.WriteError(w, r, err)
			return
		}
		http.Redirect(w, r, loc, http.StatusSeeOther)
	}
}

func (p *probe) addLine(w http.ResponseWriter, r *http.Request) {
	info, ok := p.record(w, r, auth.RightWrite)
	if !ok {
		return
	}
	ctx := r.Context()
	// formID reads r.PostForm, which only the guard (ParseForm) fills for urlencoded bodies.
	site, err := formID(r, "line_site_id")
	var project int64
	if err == nil {
		project, err = formID(r, "line_project_id")
	}
	if err == nil {
		posted := map[string][]int64{}
		for k, v := range map[string]int64{"line_site_id": site, "line_project_id": project} {
			if v != 0 {
				posted[k] = []int64{v}
			}
		}
		values := map[string][]int64{}
		for _, f := range append(append([]string(nil), p.dt.ProjectFields...), p.dt.SiteFields...) {
			values[f] = posted[strings.TrimPrefix(f, "lines.")]
		}
		err = p.w.Auth.CheckValues(ctx, principal(r), p.dt.Name, values)
	}
	if err == nil {
		err = p.writeTx(ctx, func(q db.DBTX, _ *db.Tx) error {
			return insertLines(ctx, q, p.dt, info.ID, []int64{site}, []int64{project})
		})
	}
	if err != nil {
		p.srv.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/%d", p.slug, info.ID), http.StatusSeeOther)
}

func (p *probe) deleteLine(w http.ResponseWriter, r *http.Request) {
	info, ok := p.record(w, r, auth.RightWrite)
	if !ok {
		return
	}
	ctx := r.Context()
	line, err := strconv.ParseInt(chi.URLParam(r, "line"), 10, 64)
	if err != nil {
		p.srv.WriteError(w, r, auth.ErrNotFound)
		return
	}
	if err := p.writeTx(ctx, func(q db.DBTX, _ *db.Tx) error {
		_, err := q.ExecContext(ctx, `DELETE FROM `+p.dt.LinesTable+` WHERE id = ? AND parent_id = ?`, line, info.ID) //nolint:gosec // G202: table is a probe constant
		return err
	}); err != nil {
		p.srv.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/%d", p.slug, info.ID), http.StatusSeeOther)
}

func (p *probe) print(w http.ResponseWriter, r *http.Request) {
	if info, ok := p.record(w, r, auth.RightPrint); ok {
		writeText(w, "text/html; charset=utf-8", "Print "+p.dt.Label+" "+info.Display)
	}
}

func (p *probe) pdf(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.record(w, r, auth.RightPrint); ok {
		writeText(w, "application/pdf", "%PDF-1.7 probe")
	}
}

func (p *probe) export(w http.ResponseWriter, r *http.Request) {
	if rs, ok := p.scopedRows(w, r, auth.RightExport, nil, nil, ""); ok {
		writeText(w, "text/csv; charset=utf-8", "number\n"+lines(rs))
	}
}

// importCSV checks create per data row (row 1 is the header) and answers 400
// with every row's message, else inserts the rows (code, name).
func (p *probe) importCSV(w http.ResponseWriter, r *http.Request) {
	ctx, pr := r.Context(), principal(r)
	recs, err := csv.NewReader(strings.NewReader(r.PostFormValue("csv"))).ReadAll()
	if err != nil || len(recs) == 0 {
		p.srv.WriteError(w, r, errBadInput)
		return
	}
	var msgs []string
	for i := range recs[1:] {
		err := p.w.Auth.Can(ctx, pr, p.dt.Name, auth.RightCreate, nil)
		switch {
		case errors.Is(err, auth.ErrForbidden):
			msgs = append(msgs, auth.ImportRowMessage(i+2, auth.RightCreate, p.dt.Label))
		case err != nil:
			p.srv.WriteError(w, r, err)
			return
		}
	}
	if len(msgs) > 0 {
		p.srv.WriteError(w, r, &auth.Problem{Code: http.StatusBadRequest, Msg: strings.Join(msgs, "\n")})
		return
	}
	if err := p.writeTx(ctx, func(q db.DBTX, _ *db.Tx) error {
		for _, rec := range recs[1:] {
			name := rec[0]
			if len(rec) > 1 {
				name = rec[1]
			}
			if _, err := q.ExecContext(ctx,
				`INSERT INTO `+p.dt.Table+` (code, name, created_by) VALUES (?, ?, ?)`, //nolint:gosec // G202: table is a probe constant
				rec[0], name, int64(pr.ID)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		p.srv.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, "/"+p.slug+"/list", http.StatusSeeOther)
}

// file serves an attachment to whoever can read its parent record.
func file(w http.ResponseWriter, r *http.Request, srv *authweb.Server, wd *authtest.World) {
	ctx := r.Context()
	var dtName string
	var parent int64
	var body []byte
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err == nil {
		err = wd.DB.Read(ctx, func(tx *db.Tx) error {
			return tx.SQL().QueryRowContext(ctx,
				`SELECT doctype, parent_id, body FROM probe_attachment WHERE id = ?`, id).Scan(&dtName, &parent, &body)
		})
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, strconv.ErrSyntax) || errors.Is(err, strconv.ErrRange) {
			err = auth.ErrNotFound
		}
		srv.WriteError(w, r, err)
		return
	}
	var info doctype.Info
	err = wd.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		info, err = wd.Info.Info(ctx, tx, dtName, parent)
		return err
	})
	if err == nil {
		ref := refOf(info)
		err = wd.Auth.Can(ctx, principal(r), dtName, auth.RightRead, &ref)
	}
	if err != nil {
		srv.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}
