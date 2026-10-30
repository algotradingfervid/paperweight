package internal_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"go/build"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filesfake"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/types"
)

// fixedNow is the canonical test clock instant (00-fixtures.md: 2026-10-24 10:30 IST).
var fixedNow = time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)

// Priya Shah (00-fixtures.md): user 4, Purchase User, employee 4.
var priya = auth.Principal{ID: 4, Name: "Priya Shah", Roles: []string{"Purchase User", "Employee"}}

// forbiddenReadOrder relies on fixture not being linked into this test binary: fixture.Order is then unregistered and authfake falls back to the name as the label (CD-157).
const forbiddenReadOrder = "You don't have permission to read fixture.Order. Ask your administrator for a role that allows it."

func TestContracts_FakesSatisfyInterfaces(t *testing.T) {
	var _ auth.Service = authfake.New()
	var _ auth.Directory = authfake.NewDirectory()
	var _ audit.Writer = auditfake.New()
	var _ *auditfake.Recorder = auditfake.New() //nolint:staticcheck // CD-86: the explicit type pins the exported name Recorder
	var _ files.Service = filesfake.NewWith(clock.NewFixed(fixedNow), authfake.New(), docfake.New())
	var _ doc.Engine = docfake.New()
	var _ doctype.InfoSource = docfake.New()
	var _ db.DBTX = (*sql.Tx)(nil)

	aliases := []struct {
		name      string
		got, want reflect.Type
	}{
		{"auth.UserID = types.UserID", reflect.TypeOf(auth.UserID(0)), reflect.TypeOf(types.UserID(0))},
		{"list.Page = types.Page", reflect.TypeOf(list.Page{}), reflect.TypeOf(types.Page{})},
		{"doc.DocType = doctype.DocType", reflect.TypeOf(doc.DocType{}), reflect.TypeOf(doctype.DocType{})},
		{"doc.Field = doctype.Field", reflect.TypeOf(doc.Field{}), reflect.TypeOf(doctype.Field{})},
	}
	for _, a := range aliases {
		if a.got != a.want {
			t.Errorf("%s: got type %v, want the same type as %v (it must be an alias, not a new type)", a.name, a.got, a.want)
		}
	}
}

func TestContracts_FieldListsMatchSpec(t *testing.T) {
	f := doctype.Field{ListPriority: 1, MaxLen: 140, Options: []string{"Cash", "Bank"}, Hint: "h", Step: "lines"} // CD-42
	if f.ListPriority != 1 || f.MaxLen != 140 || !slices.Equal(f.Options, []string{"Cash", "Bank"}) || f.Hint != "h" || f.Step != "lines" {
		t.Errorf("doctype.Field literal round trip = %+v", f)
	}
	info := doctype.Info{Party: "Shree Cement Traders"} // CD-55
	if info.Party != "Shree Cement Traders" {
		t.Errorf("doctype.Info.Party = %q", info.Party)
	}
	ref := audit.Ref{DocType: "fixture.Order", DocID: 1, Number: "PO/26-27/00001", Action: "approve"} // CD-69
	if ref.DocType != "fixture.Order" || ref.DocID != 1 || ref.Number != "PO/26-27/00001" || ref.Action != "approve" {
		t.Errorf("audit.Ref literal round trip = %+v", ref)
	}
	res := doc.Result{Warnings: []string{"w"}} // CD-68
	if !slices.Equal(res.Warnings, []string{"w"}) {
		t.Errorf("doc.Result.Warnings = %v", res.Warnings)
	}
	rec := doc.Record{Info: doctype.Info{ID: 1}, Version: 2, Values: map[string]string{"qty": "1200.000"}, Lines: []map[string]string{{"rate": "62.50"}}} // CD-43
	if rec.Info.ID != 1 || rec.Version != 2 || rec.Values["qty"] != "1200.000" || len(rec.Lines) != 1 || rec.Lines[0]["rate"] != "62.50" {
		t.Errorf("doc.Record literal round trip = %+v", rec)
	}
	lh := print.LetterheadInfo{LegalName: "L", LogoURL: "u", BranchName: "B", Address: []string{"a"}, StateName: "Maharashtra", StateCode: "27", GSTIN: "27AAFCV2449G1Z7"} // CD-58
	if lh.LegalName != "L" || lh.LogoURL != "u" || lh.BranchName != "B" || !slices.Equal(lh.Address, []string{"a"}) || lh.StateName != "Maharashtra" || lh.StateCode != "27" || lh.GSTIN != "27AAFCV2449G1Z7" {
		t.Errorf("print.LetterheadInfo literal round trip = %+v", lh)
	}

	if doctype.ActionApprove != "approve" {
		t.Errorf("doctype.ActionApprove = %q, want %q", doctype.ActionApprove, "approve")
	}
	if doctype.StatePending != "pending_approval" {
		t.Errorf("doctype.StatePending = %q, want %q", doctype.StatePending, "pending_approval")
	}

	// Every §5 Action and State value has a constant (plan Task 5, "Error sentinels").
	actions := map[doctype.Action]string{
		doctype.ActionSave: "save", doctype.ActionSubmit: "submit", doctype.ActionWithdraw: "withdraw",
		doctype.ActionApprove: "approve", doctype.ActionReject: "reject", doctype.ActionRevise: "revise",
		doctype.ActionClose: "close", doctype.ActionCancel: "cancel", doctype.ActionAmend: "amend",
		doctype.ActionDelete: "delete",
	}
	if len(actions) != 10 {
		t.Errorf("doctype.Action* constants are not 10 distinct values: %v", actions)
	}
	for got, want := range actions {
		if string(got) != want {
			t.Errorf("doctype action constant = %q, want %q", got, want)
		}
	}
	states := map[doctype.State]string{
		doctype.StateDraft: "draft", doctype.StatePending: "pending_approval", doctype.StateApproved: "approved",
		doctype.StateClosed: "closed", doctype.StateRejected: "rejected", doctype.StateCancelled: "cancelled",
	}
	if len(states) != 6 {
		t.Errorf("doctype.State* constants are not 6 distinct values: %v", states)
	}
	for got, want := range states {
		if string(got) != want {
			t.Errorf("doctype state constant = %q, want %q", got, want)
		}
	}
}

func TestContracts_ErrorsImplementHTTPError(t *testing.T) {
	if auth.ErrNotFound != doctype.ErrNotFound {
		t.Errorf("auth.ErrNotFound (%v) is not the same value as doctype.ErrNotFound (%v); CD-54 requires one sentinel", auth.ErrNotFound, doctype.ErrNotFound)
	}
	if doctype.ErrNotFound == nil || doctype.ErrNotFound.Error() != "This record doesn't exist or you don't have access to it." {
		t.Errorf("doctype.ErrNotFound text = %v, want %q", doctype.ErrNotFound, "This record doesn't exist or you don't have access to it.")
	}

	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"doctype.ErrNotFound", doctype.ErrNotFound, 404},
		{"auth.ErrNotFound", auth.ErrNotFound, 404},
		{"auth.ErrForbidden", auth.ErrForbidden, 403},
	}
	for _, c := range cases {
		var he doctype.HTTPError
		if !errors.As(fmt.Errorf("x: %w", c.err), &he) {
			t.Errorf("%s does not implement doctype.HTTPError through a wrap", c.name)
			continue
		}
		if got := he.Status(); got != c.status {
			t.Errorf("%s Status() = %d, want %d", c.name, got, c.status)
		}
		if fe := he.FieldErrors(); fe != nil {
			t.Errorf("%s FieldErrors() = %v, want nil", c.name, fe)
		}
	}
}

func TestContracts_StubsReturnNotImplemented(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{}
	for _, c := range cases {
		if !errors.Is(c.err, errs.ErrNotImplemented) {
			t.Errorf("%s error = %v, want errs.ErrNotImplemented", c.name, c.err)
		}
	}
}

func TestContracts_AuthFakeDeniesByDefault(t *testing.T) {
	ctx := context.Background()
	f := authfake.New()
	p := auth.Principal{ID: 4}

	err := f.Can(ctx, p, "fixture.Order", "read", nil)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Can with no rights error = %v, want auth.ErrForbidden", err)
	}
	if err.Error() != forbiddenReadOrder {
		t.Errorf("Can with no rights text = %q, want %q", err.Error(), forbiddenReadOrder)
	}

	f.Allow(4, "fixture.Order", "read")
	if err := f.Can(ctx, p, "fixture.Order", "read", nil); err != nil {
		t.Errorf("Can after Allow error = %v, want nil", err)
	}
	pred, err := f.Scope(ctx, p, "fixture.Order", "read")
	if err != nil {
		t.Fatalf("Scope after Allow error = %v, want nil", err)
	}
	if !reflect.DeepEqual(pred, auth.Predicate{SQL: ""}) {
		t.Errorf("Scope after Allow = %+v, want auth.Predicate{SQL: \"\"}", pred)
	}
}

func TestContracts_AuthFakeSeedingHelpers(t *testing.T) {
	ctx := context.Background()
	f := authfake.New()
	p := auth.Principal{ID: 4}
	f.Grant("Purchase User", "fixture.Order", "read", auth.ScopeOwn)
	f.AddRole(4, "Purchase User")

	if err := f.Can(ctx, p, "fixture.Order", "read", &auth.RecordRef{DocType: "fixture.Order", ID: 1, CreatedBy: 4}); err != nil {
		t.Errorf("Can on own record error = %v, want nil", err)
	}
	if err := f.Can(ctx, p, "fixture.Order", "read", &auth.RecordRef{DocType: "fixture.Order", ID: 2, CreatedBy: 3}); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Can on another user's record with scope own error = %v, want auth.ErrNotFound", err)
	}
	ok, err := f.HasRole(ctx, 4, "Purchase User")
	if err != nil || !ok {
		t.Errorf("HasRole(4, Purchase User) = (%v, %v), want (true, nil)", ok, err)
	}

	f.Restrict(4, []int64{10}, nil)
	if err := f.Can(ctx, p, "fixture.Order", "read", &auth.RecordRef{DocType: "fixture.Order", ID: 3, CreatedBy: 4, ProjectIDs: []int64{11}}); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Can on record outside Restrict projects error = %v, want auth.ErrNotFound", err)
	}
	if err := f.Can(ctx, p, "fixture.Order", "read", &auth.RecordRef{DocType: "fixture.Order", ID: 4, CreatedBy: 4, ProjectIDs: []int64{10}}); err != nil {
		t.Errorf("Can on record inside Restrict projects error = %v, want nil", err)
	}

	f.Hide("fixture.Order", 9)
	if err := f.CheckLink(ctx, p, "fixture.Order", 9); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("CheckLink on hidden record error = %v, want auth.ErrForbidden", err)
	}
}

func TestContracts_AuthFakeRound4Helpers(t *testing.T) {
	ctx := context.Background()

	t.Run("remove_role", func(t *testing.T) {
		f := authfake.New()
		p := auth.Principal{ID: 4}
		f.Grant("Purchase User", "fixture.Order", "read", auth.ScopeAll)
		f.AddRole(4, "Purchase User")
		if err := f.Can(ctx, p, "fixture.Order", "read", nil); err != nil {
			t.Fatalf("Can before RemoveRole error = %v, want nil", err)
		}
		f.RemoveRole(4, "Purchase User")
		ok, err := f.HasRole(ctx, 4, "Purchase User")
		if ok || err != nil {
			t.Errorf("HasRole after RemoveRole = (%v, %v), want (false, nil)", ok, err)
		}
		if err := f.Can(ctx, p, "fixture.Order", "read", nil); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("Can after RemoveRole error = %v, want auth.ErrForbidden", err)
		}
		f.RemoveRole(4, "Stores") // never added: must not panic
	})

	t.Run("wildcard_grant", func(t *testing.T) {
		f := authfake.New()
		f.Grant("Administrator", "*", "delete", auth.ScopeAll)
		f.AddRole(1, "Administrator")
		if err := f.Can(ctx, auth.Principal{ID: 1}, "fixture.Receipt", "delete", nil); err != nil {
			t.Errorf("Can with \"*\" grant error = %v, want nil", err)
		}
	})

	t.Run("users_with_right_skips_disabled", func(t *testing.T) {
		f := authfake.New()
		f.Grant("Purchase User", "fixture.Order", "read", auth.ScopeAll)
		f.AddUser(auth.UserInfo{ID: 8, Enabled: false})
		f.AddUser(auth.UserInfo{ID: 4, Enabled: true})
		f.AddRole(8, "Purchase User")
		f.AddRole(4, "Purchase User")
		got, err := f.UsersWithRight(ctx, "fixture.Order", "read", nil)
		if err != nil || !reflect.DeepEqual(got, []auth.UserID{4}) {
			t.Errorf("UsersWithRight = (%v, %v), want ([4], nil)", got, err)
		}
		if err := f.DisableUser(ctx, nil, 4, "Left the company"); err != nil {
			t.Fatalf("DisableUser(4) error = %v, want nil", err)
		}
		got, err = f.UsersWithRight(ctx, "fixture.Order", "read", nil)
		if err != nil || len(got) != 0 {
			t.Errorf("UsersWithRight after DisableUser = (%v, %v), want ([], nil)", got, err)
		}
	})

	t.Run("docfake_set_result", func(t *testing.T) {
		d := docfake.New()
		want := doc.Result{ID: 1, State: doctype.StatePending}
		d.SetResult("fixture.Order", 1, doctype.ActionSubmit, want, nil)
		got, err := d.Do(ctx, priya, "fixture.Order", 1, doctype.ActionSubmit, doc.ActionInput{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Do(submit) = (%+v, %v), want (%+v, nil)", got, err, want)
		}
	})
}

func TestContracts_AuthDirectoryFake(t *testing.T) {
	ctx := context.Background()
	d := authfake.NewDirectory()
	d.SetManager(3, 2)
	d.SetManager(4, 3)
	d.SetManager(7, 4)
	d.Link(4, 4)
	d.Link(7, 7)

	emp, ok, err := d.EmployeeForUser(ctx, nil, 4)
	if emp != 4 || !ok || err != nil {
		t.Errorf("EmployeeForUser(4) = (%d, %v, %v), want (4, true, nil)", emp, ok, err)
	}
	chain, err := d.ManagerChain(ctx, nil, 7)
	if err != nil || !slices.Equal(chain, []int64{4, 3, 2}) {
		t.Errorf("ManagerChain(7) = (%v, %v), want ([4 3 2], nil)", chain, err)
	}
	reports, err := d.DirectReports(ctx, nil, 3)
	if err != nil || !slices.Equal(reports, []int64{4}) {
		t.Errorf("DirectReports(3) = (%v, %v), want ([4], nil)", reports, err)
	}
	u, ok, err := d.UserForEmployee(ctx, nil, 4)
	if u != 4 || !ok || err != nil {
		t.Errorf("UserForEmployee(4) while active = (%d, %v, %v), want (4, true, nil)", u, ok, err)
	}

	d.SetActive(4, false)
	reports, err = d.DirectReports(ctx, nil, 3)
	if err != nil || len(reports) != 0 {
		t.Errorf("DirectReports(3) after employee 4 left = (%v, %v), want ([], nil)", reports, err)
	}
	u, ok, err = d.UserForEmployee(ctx, nil, 4)
	if u != 0 || ok || err != nil {
		t.Errorf("UserForEmployee(4) after leaving = (%d, %v, %v), want (0, false, nil)", u, ok, err)
	}
}

func TestContracts_NamingLabel(t *testing.T) {
	if got := naming.Label("", 42); got != "Draft 42" {
		t.Errorf("naming.Label(\"\", 42) = %q, want %q", got, "Draft 42")
	}
	if got := naming.Label("PO/26-27/00001", 42); got != "PO/26-27/00001" {
		t.Errorf("naming.Label(\"PO/26-27/00001\", 42) = %q, want %q", got, "PO/26-27/00001")
	}
}

func TestContracts_PrincipalContextRoundTrip(t *testing.T) {
	got, ok := auth.PrincipalFrom(auth.WithPrincipal(context.Background(), priya))
	if !ok || !reflect.DeepEqual(got, priya) {
		t.Errorf("PrincipalFrom(WithPrincipal(ctx, p)) = (%+v, %v), want (%+v, true)", got, ok, priya)
	}
	got, ok = auth.PrincipalFrom(context.Background())
	if ok || !reflect.DeepEqual(got, auth.Principal{}) {
		t.Errorf("PrincipalFrom(empty ctx) = (%+v, %v), want (zero, false)", got, ok)
	}
}

func TestContracts_AuditFakeRecords(t *testing.T) {
	ctx := context.Background()
	r := auditfake.New()
	e1 := audit.Entry{
		DocType: "fixture.Order", DocID: 1, Action: "save", By: 4, At: fixedNow,
		Changes: []audit.Change{{Field: "qty", Before: "", After: "1200.000", Op: "set"}},
	}
	e2 := audit.Entry{
		DocType: "fixture.Order", DocID: 1, Action: "submit", From: "draft", To: "pending_approval", By: 4, At: fixedNow,
		Changes: []audit.Change{{Field: "rate", Before: "62.50", After: "96.00", LineNo: 2, LineID: 7, Op: "set"}},
	}
	if err := r.Append(ctx, nil, e1); err != nil {
		t.Fatalf("Append(e1) error = %v", err)
	}
	if err := r.Append(ctx, nil, e2); err != nil {
		t.Fatalf("Append(e2) error = %v", err)
	}
	got := r.Entries()
	if !reflect.DeepEqual(got, []audit.Entry{e1, e2}) {
		t.Fatalf("Entries() = %+v, want [e1 e2] in order", got)
	}
	if got[0].Changes[0].Op != "set" {
		t.Errorf("Entries()[0].Changes[0].Op = %q, want %q", got[0].Changes[0].Op, "set")
	}

	boom := errors.New("disk full")
	r.FailNext(boom)
	if err := r.Append(ctx, nil, audit.Entry{DocType: "fixture.Order", DocID: 2, Action: "save"}); !errors.Is(err, boom) {
		t.Errorf("Append after FailNext error = %v, want %v", err, boom)
	}
	if n := len(r.Entries()); n != 2 {
		t.Errorf("Entries() after failed append has %d entries, want 2", n)
	}
}

func TestContracts_DocFakeInfoAndReferences(t *testing.T) {
	ctx := context.Background()
	f := docfake.New()
	want := doctype.Info{DocType: "fixture.Order", DocLabel: "Purchase Order", ID: 1, State: doctype.StateApproved, Number: "PO/26-27/00001", Display: "PO/26-27/00001", CreatedBy: 4}
	f.SetInfo(want)
	got, err := f.Info(ctx, nil, "fixture.Order", 1)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Info(fixture.Order, 1) = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	if _, err := f.Info(ctx, nil, "fixture.Order", 99); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Info for unknown id error = %v, want doctype.ErrNotFound", err)
	}

	refs := []doc.Reference{
		{DocType: "fixture.Order", Field: "party", ID: 1, Display: "PO/26-27/00001", State: doctype.StateApproved},
		{DocType: "fixture.Order", Field: "party", ID: 2, Display: "PO/26-27/00002", State: doctype.StateApproved},
		{DocType: "fixture.Receipt", Field: "party", ID: 1, Display: "GR/26-27/00001", State: doctype.StateDraft},
	}
	for _, r := range refs {
		f.AddReference("parties", 5, r)
	}
	gotRefs, err := f.References(ctx, nil, "parties", 5, 2)
	if err != nil || !reflect.DeepEqual(gotRefs, refs[:2]) {
		t.Errorf("References(limit 2) = (%+v, %v), want the first two in insertion order", gotRefs, err)
	}
	first, ok, err := f.IsReferenced(ctx, nil, "parties", 5)
	if first != "PO/26-27/00001" || !ok || err != nil {
		t.Errorf("IsReferenced = (%q, %v, %v), want (\"PO/26-27/00001\", true, nil)", first, ok, err)
	}
	first, ok, err = f.IsReferenced(ctx, nil, "parties", 6)
	if first != "" || ok || err != nil {
		t.Errorf("IsReferenced for an unreferenced row = (%q, %v, %v), want (\"\", false, nil)", first, ok, err)
	}
}

func TestContracts_DocFakeLoad(t *testing.T) {
	ctx := context.Background()
	f := docfake.New()
	want := doc.Record{Info: doctype.Info{DocType: "fixture.Order", ID: 1}, Version: 3, Values: map[string]string{"qty": "1200.000", "date": "2026-10-24"}}
	f.SetRecord(want)

	got, err := f.Load(ctx, nil, "fixture.Order", 1)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	got.Values["qty"] = "0.000"
	again, err := f.Load(ctx, nil, "fixture.Order", 1)
	if err != nil || again.Values["qty"] != "1200.000" {
		t.Errorf("Load after changing the returned map: qty = %q (err %v), want %q", again.Values["qty"], err, "1200.000")
	}

	f.SetState("fixture.Order", 1, doctype.StateApproved)
	after, err := f.Load(ctx, nil, "fixture.Order", 1)
	if err != nil || after.Info.State != doctype.StateApproved {
		t.Errorf("Load after SetState: State = %q (err %v), want %q", after.Info.State, err, doctype.StateApproved)
	}

	if _, err := f.Load(ctx, nil, "fixture.Order", 99); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Load for unknown id error = %v, want doctype.ErrNotFound", err)
	}
}

func TestContracts_FilesFakeSpoolAttachList(t *testing.T) {
	ctx := context.Background()
	c := clock.NewFixed(fixedNow)
	a, docs := authfake.New(), docfake.New()
	f := filesfake.NewWith(c, a, docs)
	a.Allow(priya.ID, "fixture.Claim", "read", "write")
	docs.SetInfo(doctype.Info{DocType: "fixture.Claim", ID: 1, State: doctype.StateDraft, Display: "Draft 1", CreatedBy: 4})
	docs.SetInfo(doctype.Info{DocType: "fixture.Claim", ID: 2, State: doctype.StateDraft, Display: "Draft 2", CreatedBy: 3})
	data := filestest.JPEG(2_300_001) // about 2.3 MB
	parent := auth.RecordRef{DocType: "fixture.Claim", ID: 1, CreatedBy: 4}

	sp := f.Spool("receipt.jpg", data)
	id, err := f.Attach(ctx, nil, priya, parent, "receipt.jpg", sp)
	if err != nil {
		t.Fatalf("Attach error = %v", err)
	}
	metas, err := f.List(ctx, priya, parent)
	if err != nil || len(metas) != 1 {
		t.Fatalf("List = (%+v, %v), want one Meta", metas, err)
	}
	m := metas[0]
	if m.ID != id || m.Name != "receipt.jpg" || m.Size != int64(len(data)) || m.UploadedBy != 4 || !m.UploadedAt.Equal(c.Now()) {
		t.Errorf("List()[0] = %+v, want ID %d, Name receipt.jpg, Size %d, UploadedBy 4, UploadedAt %v", m, id, len(data), c.Now())
	}

	rc, _, err := f.Open(ctx, priya, id)
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("Open returned %d bytes (err %v), want the %d spooled bytes", len(got), err, len(data))
	}

	to := auth.RecordRef{DocType: "fixture.Claim", ID: 2, CreatedBy: 3}
	if err := f.CopyAll(ctx, nil, parent, to); err != nil {
		t.Fatalf("CopyAll error = %v", err)
	}
	copies, err := f.List(ctx, priya, to)
	if err != nil || len(copies) != 1 {
		t.Fatalf("List(to) = (%+v, %v), want one Meta", copies, err)
	}
	if copies[0].UploadedBy != 3 || copies[0].Size != int64(len(data)) {
		t.Errorf("copy = %+v, want UploadedBy 3 and Size %d", copies[0], len(data))
	}
}

// registryRun makes the wp0test. names unique per run, so -count=N does not hit the duplicate panic.
var registryRun atomic.Int64

func TestContracts_DoctypeRegistry(t *testing.T) {
	n := registryRun.Add(1)
	zeta := fmt.Sprintf("wp0test.Zeta%d", n)
	alpha := fmt.Sprintf("wp0test.Alpha%d", n)
	doctype.Register(doctype.DocType{Name: zeta, Label: "Zeta", PluralLabel: "Zetas", Table: "wp0test_zeta"})
	doctype.Register(doctype.DocType{Name: alpha, Label: "Alpha", PluralLabel: "Alphas", Table: "wp0test_alpha"})

	all := doctype.All()
	if !sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Name < all[j].Name }) {
		t.Errorf("All() is not sorted by Name")
	}
	var ours []string
	for _, dt := range all {
		if dt.Name == alpha || dt.Name == zeta {
			ours = append(ours, dt.Name)
		}
	}
	if !slices.Equal(ours, []string{alpha, zeta}) {
		t.Errorf("All() order of registered names = %v, want [%s %s]", ours, alpha, zeta)
	}

	got, ok := doctype.Lookup(alpha)
	if !ok || got == nil || got.Label != "Alpha" {
		t.Fatalf("Lookup(%s) = (%+v, %v), want the registered DocType", alpha, got, ok)
	}
	if _, ok := doctype.Lookup("wp0test.Missing"); ok {
		t.Errorf("Lookup of an unregistered name ok = true, want false")
	}
	viaDoc, ok := doc.Lookup(alpha)
	if !ok || viaDoc != got {
		t.Errorf("doc.Lookup(%s) = %p, want the same pointer as doctype.Lookup (%p)", alpha, viaDoc, got)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("Register of a duplicate name did not panic")
			}
		}()
		doctype.Register(doctype.DocType{Name: alpha, Label: "Alpha again", PluralLabel: "Alphas", Table: "wp0test_alpha"})
	}()
}

// ───────── Import rules (Review Focus 5, CD-04, CD-59, CD-85, CD-93) ─────────

const modulePath = "github.com/vardhan/internaltools"

// kernelTable is the §4.2 table: what each kernel package may import besides
// internal/types, internal/platform/... and internal/kernel/doctype.
var kernelTable = map[string][]string{
	"audit":    nil,
	"auth":     {"audit"},
	"naming":   {"auth", "audit"},
	"approval": {"auth", "audit"},
	"files":    {"auth", "audit"},
	"print":    {"auth", "files"},
	"gstcalc":  nil,
	"list":     {"auth", "naming"},
	"csvio":    {"auth", "list", "audit"},
	"doc":      {"auth", "naming", "approval", "audit", "files"},
}

// helperExtra lists the extra imports a named test helper may make (CD-168:
// APR's engine driver runs the real DOC engine, naming and fixtures).
var helperExtra = map[string][]string{
	"internal/kernel/approval/approvaltest": {"internal/kernel/doc", "internal/kernel/naming", "internal/fixture"},
}

// areaPackages lists the kernel packages owned by the same work package (CD-93: FILE owns files and print).
func areaPackages(pkg string) []string {
	if pkg == "files" || pkg == "print" {
		return []string{"files", "print"}
	}
	return []string{pkg}
}

// isFakePackage reports whether imp is internal/kernel/<x>/<x>fake.
func isFakePackage(imp string) bool {
	parts := strings.Split(strings.TrimPrefix(imp, "internal/kernel/"), "/")
	return strings.HasPrefix(imp, "internal/kernel/") && len(parts) == 2 && parts[1] == parts[0]+"fake"
}

func isHelperName(name, parent string) bool {
	return name == parent+"fake" || name == parent+"test"
}

func underOrEqual(imp, dir string) bool {
	return imp == dir || strings.HasPrefix(imp, dir+"/")
}

// baseRule: what the kernel package directory internal/kernel/<top> itself may import.
func baseRule(top, imp string) (bool, string) {
	if top == "doctype" {
		return imp == "internal/types" || imp == "internal/platform/db",
			"internal/kernel/doctype imports only internal/types and internal/platform/db (§4.2 rule 3)"
	}
	row, known := kernelTable[top]
	if !known {
		return false, "internal/kernel/" + top + " is not in the §4.2 table"
	}
	if imp == "internal/types" || strings.HasPrefix(imp, "internal/platform/") || imp == "internal/kernel/doctype" {
		return true, ""
	}
	for _, other := range row {
		if imp == "internal/kernel/"+other {
			return true, ""
		}
	}
	return false, fmt.Sprintf("internal/kernel/%s may import only internal/types, internal/platform/..., internal/kernel/doctype and its §4.2 row %v", top, row)
}

// importRule decides whether pkg may import imp (both module-relative) and names the rule when it may not.
func importRule(pkg, imp string) (bool, string) {
	if pkg == "internal/types" {
		return !strings.HasPrefix(imp, "internal/"), "internal/types imports no internal/ package (§4.2)"
	}
	if !strings.HasPrefix(pkg, "internal/kernel/") {
		return false, pkg + " is not a kernel package"
	}
	parts := strings.Split(strings.TrimPrefix(pkg, "internal/kernel/"), "/")
	top := parts[0]
	parent := "internal/kernel/" + top
	if ok, why := baseRule(top, imp); ok || len(parts) == 1 {
		if !ok && len(parts) == 1 && imp == parent+"/dbq" { // §4.4: a package's own generated sqlc code
			return true, ""
		}
		return ok, why
	}
	// Subpackages (CD-85). Only reached when the parent's own rule rejects imp.
	last := parts[len(parts)-1]
	switch {
	case len(parts) >= 2 && isHelperName(parts[1], top):
		if imp == parent || underOrEqual(imp, "internal/testkit") || isFakePackage(imp) || underOrEqual(imp, parent+"/"+parts[1]) {
			return true, ""
		}
		if slices.Contains(helperExtra[pkg], imp) {
			return true, ""
		}
		return false, fmt.Sprintf("%s (a fake/test helper) may import what %s may, plus %s, internal/testkit and any internal/kernel/*/*fake (CD-85)", pkg, parent, parent)
	case parts[1] == "web":
		if imp == parent || underOrEqual(imp, "internal/ui") || underOrEqual(imp, parent+"/web") {
			return true, ""
		}
		for _, a := range areaPackages(top) {
			for _, h := range []string{"fake", "test"} {
				if underOrEqual(imp, "internal/kernel/"+a+"/"+a+h) {
					return true, ""
				}
			}
		}
		if strings.HasSuffix(last, "test") && (underOrEqual(imp, "internal/testkit") || isFakePackage(imp)) {
			return true, "" // a test helper inside the web tree (CD-85 helper rule)
		}
		if strings.HasSuffix(last, "test") && (underOrEqual(imp, "internal/kernel/files/web") || underOrEqual(imp, "internal/kernel/print/web")) {
			return true, "" // an e2e test server mounts both area routers (CD-138)
		}
		return false, fmt.Sprintf("%s (a screen package) may import what %s may, plus %s, internal/ui, internal/platform/web and its own area's fake/test helpers (CD-85, CD-93)", pkg, parent, parent)
	default:
		if imp == parent { // a non-helper, non-web subpackage may import its own parent (CD-153)
			return true, ""
		}
		_, why := baseRule(top, imp)
		return false, pkg + " follows its parent's rule: " + why
	}
}

func importAllowed(pkg, imp string) bool {
	ok, _ := importRule(pkg, imp)
	return ok
}

// Review Focus 5.
func TestContracts_KernelImportsFollowTable(t *testing.T) {
	t.Run("real_tree", func(t *testing.T) {
		// The test runs in internal/, so module-relative paths are "internal/" + the walked path.
		seen := map[string]bool{}
		check := func(dir string) {
			bp, err := build.ImportDir(dir, 0)
			var noGo *build.NoGoError
			if errors.As(err, &noGo) {
				return
			}
			if err != nil {
				t.Errorf("go/build cannot read %s: %v", dir, err)
				return
			}
			pkg := "internal/" + filepath.ToSlash(dir)
			seen[pkg] = true
			for _, imp := range bp.Imports {
				if !strings.HasPrefix(imp, modulePath+"/") {
					continue
				}
				rel := strings.TrimPrefix(imp, modulePath+"/")
				if ok, why := importRule(pkg, rel); !ok {
					t.Errorf("%s imports %s, which breaks the rule: %s", pkg, rel, why)
				}
			}
		}
		check("types")
		err := filepath.WalkDir("kernel", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				check(path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking internal/kernel: %v", err)
		}
		want := []string{"internal/types", "internal/kernel/doctype"}
		for name := range kernelTable {
			want = append(want, "internal/kernel/"+name)
		}
		for _, p := range want {
			if !seen[p] {
				t.Errorf("package %s was not found; WP0 must create every §5 package", p)
			}
		}
	})

	cases := []struct {
		name, pkg, imp string
		want           bool
	}{
		{"approval_web_imports_ui", "internal/kernel/approval/web", "internal/ui", true},
		{"approval_web_views_imports_ui", "internal/kernel/approval/web/views", "internal/ui", true},
		{"authtest_imports_testkit", "internal/kernel/auth/authtest", "internal/testkit", true},
		{"docfake_imports_authfake", "internal/kernel/doc/docfake", "internal/kernel/auth/authfake", true},
		{"audit_imports_auth", "internal/kernel/audit", "internal/kernel/auth", false},
		{"auth_cli_imports_auth", "internal/kernel/auth/cli", "internal/kernel/auth", true},
		{"auth_cli_imports_doc", "internal/kernel/auth/cli", "internal/kernel/doc", false},
		{"auth_cli_imports_auth_dbq", "internal/kernel/auth/cli", "internal/kernel/auth/dbq", false},
		{"list_imports_doc", "internal/kernel/list", "internal/kernel/doc", false},
		{"doctype_imports_auth", "internal/kernel/doctype", "internal/kernel/auth", false},
		{"auth_imports_ui", "internal/kernel/auth", "internal/ui", false},
		{"approval_web_imports_filesfake", "internal/kernel/approval/web", "internal/kernel/files/filesfake", false},
		{"approvaltest_imports_doc", "internal/kernel/approval/approvaltest", "internal/kernel/doc", true},
		{"approvaltest_imports_naming", "internal/kernel/approval/approvaltest", "internal/kernel/naming", true},
		{"approvaltest_imports_fixture", "internal/kernel/approval/approvaltest", "internal/fixture", true},
		{"approvaltest_imports_files", "internal/kernel/approval/approvaltest", "internal/kernel/files", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := importAllowed(c.pkg, c.imp); got != c.want {
				t.Errorf("importAllowed(%q, %q) = %v, want %v", c.pkg, c.imp, got, c.want)
			}
		})
	}
}
