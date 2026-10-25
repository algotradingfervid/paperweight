package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	t14Admin  = "admin@vardhan.test"
	t14Suresh = "suresh@vardhan.test"
	t14Clerk  = "clerk@vardhan.test"
	t14Viewer = "viewer@vardhan.test"
)

// t14Deny is the fixture's 403 text: "You don't have permission to {right}
// {DocType}. Ask your administrator for a role that allows it."
func t14Deny(verb, label string) string {
	return "You don't have permission to " + verb + " " + label + ". Ask your administrator for a role that allows it."
}

// t14Probe builds a harness with the probe router (Task 14).
func t14Probe(t *testing.T, w *authtest.World) *webtest.Harness {
	t.Helper()
	return webtest.New(t, w, webtest.WithProbe())
}

// t14PO adds a Purchase Order with the plan's default probe data (PRJ-METRO /
// METRO-SITE, created by Priya, draft) for every field d leaves zero.
func t14PO(t *testing.T, h *webtest.Harness, d webtest.Doc) int64 {
	t.Helper()
	if d.ProjectID == 0 {
		d.ProjectID = h.W.Projects["PRJ-METRO"]
	}
	if d.SiteID == 0 {
		d.SiteID = h.W.Sites["METRO-SITE"]
	}
	if d.CreatedBy == 0 {
		d.CreatedBy = h.W.Users[t10Priya]
	}
	if d.State == "" {
		d.State = string(doctype.StateDraft)
	}
	return h.AddDoc(t, authtest.DTOrder, d)
}

// t14Str reads one text value (query must coalesce NULLs).
func t14Str(t *testing.T, w *authtest.World, query string, args ...any) string {
	t.Helper()
	ctx := context.Background()
	var s string
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&s)
	}); err != nil {
		t.Fatalf("query %q %v: %v", query, args, err)
	}
	return s
}

// t14Int reads one integer value.
func t14Int(t *testing.T, w *authtest.World, query string, args ...any) int64 {
	t.Helper()
	ctx := context.Background()
	var n int64
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("query %q %v: %v", query, args, err)
	}
	return n
}

func t14State(t *testing.T, w *authtest.World, id int64) string {
	t.Helper()
	return t14Str(t, w, `SELECT coalesce(state, '') FROM probe_order WHERE id = ?`, id)
}

func t14Orders(t *testing.T, w *authtest.World) int64 {
	t.Helper()
	return t14Int(t, w, `SELECT count(*) FROM probe_order`)
}

func t14SetDeliveryDate(t *testing.T, w *authtest.World, id int64, date string) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE probe_order SET delivery_date = ? WHERE id = ?`, date, id)
		return err
	}); err != nil {
		t.Fatalf("set delivery_date: %v", err)
	}
}

func t14DeliveryDate(t *testing.T, w *authtest.World, id int64) string {
	t.Helper()
	return t14Str(t, w, `SELECT coalesce(delivery_date, '') FROM probe_order WHERE id = ?`, id)
}

// t14Want403 checks a 403 whose body carries msg and none of leaks.
func t14Want403(t *testing.T, what string, r webtest.Response, msg string, leaks ...string) {
	t.Helper()
	wantStatus(t, what, r, http.StatusForbidden)
	if !strings.Contains(r.Body, msg) {
		t.Fatalf("%s: body %q, want %q", what, r.Body, msg)
	}
	for _, l := range leaks {
		if strings.Contains(r.Body, l) {
			t.Fatalf("%s: body leaks %q: %q", what, l, r.Body)
		}
	}
}

// t14Want404 checks a 404 with AUTH's not-found text.
func t14Want404(t *testing.T, what string, r webtest.Response) {
	t.Helper()
	wantStatus(t, what, r, http.StatusNotFound)
	if !strings.Contains(r.Body, "This record doesn't exist or you don't have access to it.") {
		t.Fatalf("%s: body %q, want the not-found text", what, r.Body)
	}
}

func t14ID(id int64) string { return strconv.FormatInt(id, 10) }

// t14CreatedID parses the 303 Location "/po/{id}" of a create.
func t14CreatedID(t *testing.T, what string, r webtest.Response) int64 {
	t.Helper()
	wantStatus(t, what, r, http.StatusSeeOther)
	loc := r.Header.Get("Location")
	rest, ok := strings.CutPrefix(loc, "/po/")
	id, err := strconv.ParseInt(rest, 10, 64)
	if !ok || err != nil || id <= 0 {
		t.Fatalf("%s: Location = %q, want /po/{id}", what, loc)
	}
	return id
}

// AUTH-064
func TestRights_NoRoleNoAccess(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00001", State: string(doctype.StateApproved)})
	h.AddDoc(t, authtest.DTReceipt, webtest.Doc{
		Number: "GR/26-27/00001", State: string(doctype.StateApproved),
		CreatedBy: w.Users[t14Suresh], SiteID: w.Sites["PUNE-WH"],
	})

	deepak := signedIn(t, h, t10Deepak)
	for _, path := range []string{"/po/list", "/po/" + t14ID(po), "/po/export.csv"} {
		t14Want403(t, "deepak GET "+path, deepak.Get(path), t14Deny("read", "Purchase Order"), "PO/26-27/00001")
	}
	t14Want403(t, "deepak GET /grn/list", deepak.Get("/grn/list"), t14Deny("read", "GRN"), "GR/26-27/00001")

	// The PO and GRN menu items aren't shown.
	readable, err := w.Auth.ReadableDocTypes(context.Background(), w.P(t, t10Deepak))
	if err != nil {
		t.Fatalf("ReadableDocTypes(deepak): %v", err)
	}
	for _, dt := range []string{authtest.DTOrder, authtest.DTReceipt} {
		if slices.Contains(readable, dt) {
			t.Errorf("ReadableDocTypes(deepak) = %q, must not contain %s", readable, dt)
		}
	}
}

// AUTH-070
func TestRights_AdminCannotBypassLifecycle(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	id := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00003", State: string(doctype.StateApproved)})

	ref := h.Ref(t, authtest.DTOrder, id)
	want := auth.RecordRef{
		DocType: authtest.DTOrder, ID: id, CreatedBy: w.Users[t10Priya],
		ProjectIDs: []int64{w.Projects["PRJ-METRO"]}, SiteIDs: []int64{w.Sites["METRO-SITE"]},
	}
	if !reflect.DeepEqual(ref, want) {
		t.Fatalf("Ref(PO/26-27/00003) = %+v, want %+v", ref, want)
	}

	// AUTH grants; the lifecycle is DOC's. RecordRef carries no state and no
	// bypass flag, so AUTH can't skip the state machine.
	admin := w.P(t, t14Admin)
	ctx := context.Background()
	for _, r := range []auth.Right{auth.RightWrite, auth.RightDelete} {
		if err := w.Auth.Can(ctx, admin, authtest.DTOrder, r, &ref); err != nil {
			t.Errorf("Can(admin, %s, %s, approved PO) = %v, want nil", authtest.DTOrder, r, err)
		}
	}
	rt := reflect.TypeOf(auth.RecordRef{})
	for i := range rt.NumField() {
		if name := rt.Field(i).Name; strings.Contains(name, "State") || strings.Contains(name, "Bypass") {
			t.Errorf("auth.RecordRef has field %s; AUTH must not carry state or a bypass", name)
		}
	}
	// WP3: extend with doc.Engine refusing "Approved documents can't be edited. Cancel and amend instead." (CD-40)
}

// AUTH-071
func TestRights_Read(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00001", State: string(doctype.StateApproved)})
	ravi := signedIn(t, h, t10Ravi)
	paths := []string{"/po/list", "/po/" + t14ID(po)}

	for _, path := range paths {
		r := ravi.Get(path)
		wantStatus(t, "ravi GET "+path, r, http.StatusOK)
		if !strings.Contains(r.Body, "PO/26-27/00001") {
			t.Fatalf("ravi GET %s: body %q lacks PO/26-27/00001", path, r.Body)
		}
	}

	if err := w.Auth.SetRights(context.Background(), w.P(t, t14Admin), "Purchase Manager", authtest.DTOrder, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatalf("SetRights(Purchase Manager, %s, {}): %v", authtest.DTOrder, err)
	}
	for _, path := range paths {
		t14Want403(t, "ravi GET "+path+" after read removed", ravi.Get(path),
			"You don't have permission to read Purchase Order. Ask your administrator for a role that allows it.", "PO/26-27/00001")
	}
}

// AUTH-072
func TestRights_Create(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	form := url.Values{
		"project_id": {t14ID(w.Projects["PRJ-METRO"])},
		"site_id":    {t14ID(w.Sites["METRO-SITE"])},
	}
	deny := t14Deny("create", "Purchase Order")

	ravi := signedIn(t, h, t10Ravi)
	t14Want403(t, "ravi GET /po/new", ravi.Get("/po/new"), deny)
	t14Want403(t, "ravi POST /po", ravi.Post("/po", form), deny)
	if n := t14Orders(t, w); n != 0 {
		t.Fatalf("after ravi's POST /po: %d probe_order rows, want 0", n)
	}

	priya := signedIn(t, h, t10Priya)
	wantStatus(t, "priya GET /po/new", priya.Get("/po/new"), http.StatusOK)
	id := t14CreatedID(t, "priya POST /po", priya.Post("/po", form))
	if got := t14Int(t, w, `SELECT coalesce(created_by, 0) FROM probe_order WHERE id = ?`, id); got != int64(w.Users[t10Priya]) {
		t.Fatalf("new PO created_by = %d, want priya %d", got, w.Users[t10Priya])
	}
}

// AUTH-073
func TestRights_Write(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	draft := t14PO(t, h, webtest.Doc{}) // Draft 4 in the case
	t14SetDeliveryDate(t, w, draft, "2026-11-01")
	rejected := t14PO(t, h, webtest.Doc{State: string(doctype.StateRejected)}) // Draft 13
	deny := t14Deny("change", "Purchase Order")

	ravi := signedIn(t, h, t10Ravi)
	t14Want403(t, "ravi save draft", ravi.Post("/po/"+t14ID(draft)+"/save", url.Values{"delivery_date": {"2026-11-15"}}), deny)
	if got := t14DeliveryDate(t, w, draft); got != "2026-11-01" {
		t.Fatalf("delivery_date after ravi's save = %q, want 2026-11-01", got)
	}

	// Revise needs write (spec R4).
	t14Want403(t, "ravi revise rejected", ravi.Post("/po/"+t14ID(rejected)+"/revise", url.Values{}), deny)
	if got := t14State(t, w, rejected); got != string(doctype.StateRejected) {
		t.Fatalf("state after ravi's revise = %q, want rejected", got)
	}
}

// AUTH-074
func TestRights_Submit(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetRights(t, "Purchase Clerk", authtest.DTOrder, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll,
	})
	w.AddUser(t, t14Clerk, "Purchase Clerk", "Purchase Clerk")
	h := t14Probe(t, w)
	draft := t14PO(t, h, webtest.Doc{})                                                                  // Draft 5
	pending := t14PO(t, h, webtest.Doc{State: string(doctype.StatePending)})                             // Draft 9
	approved := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00009", State: string(doctype.StateApproved)}) // PO/26-27/00009

	clerk := signedIn(t, h, t14Clerk)
	wantStatus(t, "clerk save draft", clerk.Post("/po/"+t14ID(draft)+"/save", url.Values{"delivery_date": {"2026-11-20"}}), http.StatusOK)
	if got := t14DeliveryDate(t, w, draft); got != "2026-11-20" {
		t.Fatalf("delivery_date after clerk's save = %q, want 2026-11-20", got)
	}

	deny := t14Deny("submit", "Purchase Order")
	for _, tc := range []struct {
		action string
		id     int64
		state  doctype.State
	}{
		{string(doctype.ActionSubmit), draft, doctype.StateDraft},
		{string(doctype.ActionWithdraw), pending, doctype.StatePending},
		{string(doctype.ActionClose), approved, doctype.StateApproved},
	} {
		t14Want403(t, "clerk "+tc.action, clerk.Post("/po/"+t14ID(tc.id)+"/"+tc.action, url.Values{}), deny)
		if got := t14State(t, w, tc.id); got != string(tc.state) {
			t.Fatalf("state after clerk's %s = %q, want %q", tc.action, got, tc.state)
		}
	}

	priya := signedIn(t, h, t10Priya)
	wantStatus(t, "priya withdraw pending", priya.Post("/po/"+t14ID(pending)+"/withdraw", url.Values{}), http.StatusSeeOther)
}

// AUTH-075
func TestRights_Approve(t *testing.T) {
	// AUTH's share: the probe's approve and reject call Can(approve, rec),
	// which answers in the app's order (hidden → 404 first, then the right).
	// On the real router DOC calls APR's CheckApprover instead (CD-52), so WP3
	// expects 403 with APR's eligibility message there, not AUTH's.
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po := t14PO(t, h, webtest.Doc{State: string(doctype.StatePending)}) // Draft 6

	priya := signedIn(t, h, t10Priya)
	deny := t14Deny("approve", "Purchase Order")
	for _, action := range []doctype.Action{doctype.ActionApprove, doctype.ActionReject} {
		t14Want403(t, "priya "+string(action), priya.Post("/po/"+t14ID(po)+"/"+string(action), url.Values{}), deny)
		if got := t14State(t, w, po); got != string(doctype.StatePending) {
			t.Fatalf("state after priya's %s = %q, want pending_approval", action, got)
		}
	}

	ravi := signedIn(t, h, t10Ravi)
	wantStatus(t, "ravi approve", ravi.Post("/po/"+t14ID(po)+"/approve", url.Values{}), http.StatusSeeOther)
	if got := t14State(t, w, po); got != string(doctype.StateApproved) {
		t.Fatalf("state after ravi's approve = %q, want approved", got)
	}
}

// AUTH-076
func TestRights_Cancel(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00007", State: string(doctype.StateApproved)})

	priya := signedIn(t, h, t10Priya)
	t14Want403(t, "priya cancel", priya.Post("/po/"+t14ID(po)+"/cancel", url.Values{}), t14Deny("cancel", "Purchase Order"))
	if got := t14State(t, w, po); got != string(doctype.StateApproved) {
		t.Fatalf("state after priya's cancel = %q, want approved", got)
	}

	ravi := signedIn(t, h, t10Ravi)
	wantStatus(t, "ravi cancel", ravi.Post("/po/"+t14ID(po)+"/cancel", url.Values{}), http.StatusSeeOther)
	if got := t14State(t, w, po); got != string(doctype.StateCancelled) {
		t.Fatalf("state after ravi's cancel = %q, want cancelled", got)
	}
}

// AUTH-077
func TestRights_Amend(t *testing.T) {
	// DOC owns the doc.Engine assertion that the amender becomes created_by
	// (CD-67); AUTH checks the right and its probe.
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00007", State: string(doctype.StateCancelled)})

	priya := signedIn(t, h, t10Priya)
	t14Want403(t, "priya amend", priya.Post("/po/"+t14ID(po)+"/amend", url.Values{}), t14Deny("amend", "Purchase Order"))
	if n := t14Orders(t, w); n != 1 {
		t.Fatalf("after priya's amend: %d probe_order rows, want 1", n)
	}

	ravi := signedIn(t, h, t10Ravi)
	wantStatus(t, "ravi amend", ravi.Post("/po/"+t14ID(po)+"/amend", url.Values{}), http.StatusSeeOther)
	if n := t14Orders(t, w); n != 2 {
		t.Fatalf("after ravi's amend: %d probe_order rows, want 2", n)
	}
	amended := t14Int(t, w, `SELECT id FROM probe_order WHERE id <> ?`, po)
	if got := t14State(t, w, amended); got != string(doctype.StateDraft) {
		t.Errorf("amended PO state = %q, want draft", got)
	}
	if got := t14Int(t, w, `SELECT coalesce(created_by, 0) FROM probe_order WHERE id = ?`, amended); got != int64(w.Users[t10Ravi]) {
		t.Errorf("amended PO created_by = %d, want ravi %d", got, w.Users[t10Ravi])
	}
	// Shown as Draft {id}: it gets PO/26-27/00007-1 only when approved.
	if got := t14Str(t, w, `SELECT coalesce(number, '') FROM probe_order WHERE id = ?`, amended); got != "" {
		t.Errorf("amended PO number = %q, want none (Draft %d)", got, amended)
	}
	if got := t14State(t, w, po); got != string(doctype.StateCancelled) {
		t.Errorf("original PO state = %q, want cancelled", got)
	}
}

// AUTH-078
func TestRights_Delete(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	a := t14PO(t, h, webtest.Doc{CreatedBy: w.Users[t10Ravi]})
	b := t14PO(t, h, webtest.Doc{CreatedBy: w.Users[t10Priya]}) // Draft 8

	ravi := signedIn(t, h, t10Ravi)
	t14Want403(t, "ravi delete A", ravi.Post("/po/"+t14ID(a)+"/delete", url.Values{}), t14Deny("delete", "Purchase Order"))
	if n := t14Int(t, w, `SELECT count(*) FROM probe_order WHERE id = ?`, a); n != 1 {
		t.Fatalf("PO A rows after ravi's delete = %d, want 1", n)
	}

	priya := signedIn(t, h, t10Priya)
	wantStatus(t, "priya delete B", priya.Post("/po/"+t14ID(b)+"/delete", url.Values{}), http.StatusSeeOther)
	if n := t14Int(t, w, `SELECT count(*) FROM probe_order WHERE id = ?`, b); n != 0 {
		t.Fatalf("PO B rows after priya's delete = %d, want 0", n)
	}
}

// AUTH-079
func TestRights_Print(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetRights(t, "Purchase Viewer", authtest.DTOrder, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll})
	w.AddUser(t, t14Viewer, "Purchase Viewer", "Purchase Viewer")
	h := t14Probe(t, w)
	po1 := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00001", State: string(doctype.StateApproved)})
	po2 := t14PO(t, h, webtest.Doc{
		Number: "PO/26-27/00002", State: string(doctype.StateApproved),
		ProjectID: w.Projects["PRJ-TECHPARK"], SiteID: w.Sites["TECH-SITE"],
	})

	viewer := signedIn(t, h, t14Viewer)
	deny := t14Deny("print", "Purchase Order")
	for _, suffix := range []string{"/print", "/pdf"} {
		r := viewer.Get("/po/" + t14ID(po1) + suffix)
		t14Want403(t, "viewer GET "+suffix, r, deny, "%PDF")
		if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "application/pdf") {
			t.Fatalf("viewer GET %s: Content-Type %q on a 403", suffix, ct)
		}
	}

	priya := signedIn(t, h, t10Priya)
	r := priya.Get("/po/" + t14ID(po1) + "/print")
	wantStatus(t, "priya GET print", r, http.StatusOK)
	if !strings.Contains(r.Body, "PO/26-27/00001") {
		t.Fatalf("priya print body %q lacks PO/26-27/00001", r.Body)
	}
	r = priya.Get("/po/" + t14ID(po1) + "/pdf")
	wantStatus(t, "priya GET pdf", r, http.StatusOK)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
		t.Fatalf("priya GET pdf: Content-Type %q, want application/pdf", ct)
	}
	if !strings.HasPrefix(r.Body, "%PDF-1.7 probe") {
		t.Fatalf("priya GET pdf: body %q, want %%PDF-1.7 probe", r.Body)
	}

	// Outside Priya's Project: hidden, so 404 before the print right.
	for _, suffix := range []string{"/print", "/pdf"} {
		r := priya.Get("/po/" + t14ID(po2) + suffix)
		t14Want404(t, "priya GET PRJ-TECHPARK PO "+suffix, r)
		if strings.Contains(r.Body, "PO/26-27/00002") {
			t.Fatalf("priya GET PRJ-TECHPARK PO %s: body leaks its number: %q", suffix, r.Body)
		}
	}
}

// AUTH-080
func TestRights_Export(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	t14PO(t, h, webtest.Doc{Number: "PO/26-27/00001", State: string(doctype.StateApproved)})

	suresh := signedIn(t, h, t14Suresh)
	t14Want403(t, "suresh GET /po/export.csv", suresh.Get("/po/export.csv"), t14Deny("export", "Purchase Order"), "PO/26-27/00001")

	priya := signedIn(t, h, t10Priya)
	r := priya.Get("/po/export.csv")
	wantStatus(t, "priya GET /po/export.csv", r, http.StatusOK)
	if !strings.Contains(r.Body, "PO/26-27/00001") {
		t.Fatalf("priya export body %q lacks PO/26-27/00001", r.Body)
	}
}

// AUTH-081
func TestRights_Import(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetRights(t, "Purchase User", authtest.DTItem, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll,
	})
	h := t14Probe(t, w)
	priya := signedIn(t, h, t10Priya)
	form := url.Values{"csv": {"code,name\nBOLT-M12,Hex Bolt M12x60\n"}}
	items := func() int64 { return t14Int(t, w, `SELECT count(*) FROM probe_item`) }

	t14Want403(t, "priya import without import", priya.Post("/item/import", form),
		"You don't have permission to import Item. Ask your administrator for a role that allows it.")
	if n := items(); n != 0 {
		t.Fatalf("probe_item rows after denied import = %d, want 0", n)
	}

	w.SetRights(t, "Purchase User", authtest.DTItem, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightImport: auth.ScopeAll,
	})
	r := priya.Post("/item/import", form)
	wantStatus(t, "priya import without create", r, http.StatusBadRequest)
	if !strings.Contains(r.Body, "Row 2: you don't have permission to create Item.") {
		t.Fatalf("import body %q, want the row 2 create message", r.Body)
	}
	if n := items(); n != 0 {
		t.Fatalf("probe_item rows after rejected import = %d, want 0", n)
	}
}

// AUTH-082
func TestRights_LinesFollowParent(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po1 := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00001", State: string(doctype.StateDraft)})
	po2 := t14PO(t, h, webtest.Doc{
		Number: "PO/26-27/00002", State: string(doctype.StateDraft),
		ProjectID: w.Projects["PRJ-TECHPARK"], SiteID: w.Sites["TECH-SITE"],
	})
	line := url.Values{"line_site_id": {t14ID(w.Sites["METRO-SITE"])}}
	lines := func() int64 { return t14Int(t, w, `SELECT count(*) FROM probe_order_line`) }

	ravi := signedIn(t, h, t10Ravi)
	deny := t14Deny("change", "Purchase Order")
	t14Want403(t, "ravi add line", ravi.Post("/po/"+t14ID(po1)+"/lines", line), deny)
	t14Want403(t, "ravi delete line", ravi.Post("/po/"+t14ID(po1)+"/lines/7/delete", url.Values{}), deny)
	if n := lines(); n != 0 {
		t.Fatalf("probe_order_line rows after ravi's posts = %d, want 0", n)
	}

	// Priya has write, but PO#2 is outside her Project: line endpoints 404.
	priya := signedIn(t, h, t10Priya)
	t14Want404(t, "priya add line to hidden PO", priya.Post("/po/"+t14ID(po2)+"/lines", line))
	t14Want404(t, "priya delete line of hidden PO", priya.Post("/po/"+t14ID(po2)+"/lines/7/delete", url.Values{}))
	if n := lines(); n != 0 {
		t.Fatalf("probe_order_line rows after priya's posts = %d, want 0", n)
	}
}

// AUTH-083
func TestRights_OwnerNotClientSettable(t *testing.T) {
	// AUTH's share: the guard strips the fields (Task 10) and the probe sets
	// them; DOC owns the engine's created_by assertions (DOC-003, CD-67).
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	priya := signedIn(t, h, t10Priya)
	raviID := t14ID(int64(w.Users[t10Ravi]))

	id := t14CreatedID(t, "priya POST /po", priya.Post("/po", url.Values{
		"project_id": {t14ID(w.Projects["PRJ-METRO"])},
		"site_id":    {t14ID(w.Sites["METRO-SITE"])},
		"created_by": {raviID},
		"created_at": {"2026-04-01T00:00:00Z"},
	}))
	createdBy := func() int64 {
		return t14Int(t, w, `SELECT coalesce(created_by, 0) FROM probe_order WHERE id = ?`, id)
	}
	if got := createdBy(); got != int64(w.Users[t10Priya]) {
		t.Fatalf("created_by = %d, want priya %d", got, w.Users[t10Priya])
	}
	if got, want := t14Str(t, w, `SELECT coalesce(created_at, '') FROM probe_order WHERE id = ?`, id), "2026-10-24T05:00:00.000Z"; got != want || got != auth.FormatTime(authtest.T0) {
		t.Fatalf("created_at = %q, want %q (server time)", got, want)
	}

	r := priya.Post(fmt.Sprintf("/po/%d/save", id), url.Values{"created_by": {raviID}, "delivery_date": {"2026-11-15"}})
	wantStatus(t, "priya save posting created_by", r, http.StatusOK)
	if got := t14DeliveryDate(t, w, id); got != "2026-11-15" {
		t.Fatalf("delivery_date after save = %q, want 2026-11-15 (the save ran)", got)
	}
	if got := createdBy(); got != int64(w.Users[t10Priya]) {
		t.Fatalf("created_by after save = %d, want priya %d (unchanged)", got, w.Users[t10Priya])
	}
}
