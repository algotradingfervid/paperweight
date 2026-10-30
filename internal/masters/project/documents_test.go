package project_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

const (
	msgCompleted = "Project PRJ-METRO is Completed. Reopen it or pick another project."
	msgCancelled = "Project PRJ-METRO is Cancelled. Reopen it or pick another project."
	msgOnHold    = "Project PRJ-METRO is On Hold."
)

// orders is the seeded fixture world, for orders on PRJ-METRO.
type orders struct {
	env *msttest.Env
	m   msttest.Masters
}

func newOrders(t *testing.T) *orders {
	t.Helper()
	env := msttest.New(t)
	return &orders{env: env, m: msttest.SeedMasters(t, env)}
}

// save saves order id (0 = new) on PRJ-METRO as Priya, who is restricted to it.
func (o *orders) save(id int64) (doc.Result, error) {
	h := mstfixture.Header{
		VendorID: o.m.Parties["SHREE"], AddressID: o.m.Addresses["Mumbai"], BranchID: o.m.Branches["PUNE"],
		ProjectID: o.m.Projects[prjCode], Date: date(2026, time.October, 24),
	}
	lines := []mstfixture.Line{{ItemID: o.m.Items["MS-ANGLE-50"], UoMID: o.m.UoMs["KGS"], TaxCodeID: o.m.TaxCodes["GST18"], Qty: 1000, Rate: 62_5000}}
	return mstfixture.Save(context.Background(), o.env, o.env.Priya, id, h, lines)
}

func (o *orders) draft(t *testing.T) int64 {
	t.Helper()
	res, err := o.save(0)
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	return res.ID
}

func (o *orders) setStatus(t *testing.T, status project.Status) {
	t.Helper()
	ctx := context.Background()
	id := o.m.Projects[prjCode]
	var p project.Project
	if err := o.env.DB.Read(ctx, func(tx *db.Tx) (err error) {
		p, err = project.New(o.env.Kit).Get(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("get %s: %v", prjCode, err)
	}
	in := project.Input{Code: p.Code, Name: p.Name, BranchID: p.BranchID, Status: status}
	if _, err := project.New(o.env.Kit).Update(ctx, o.env.Kavya, id, p.Version, in); err != nil {
		t.Fatalf("set %s %s: %v", prjCode, status, err)
	}
}

func wantProjectRefused(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("%s: err = %v (%T); want a *doc.Error %q", what, err, err, msg)
	}
	want := []doc.FieldError{{Field: "project_id", Message: msg}}
	if de.Kind != doc.KindInvalid || !reflect.DeepEqual(de.Fields, want) {
		t.Errorf("%s: Kind %q, Fields %+v; want %q, %+v", what, de.Kind, de.Fields, doc.KindInvalid, want)
	}
}

// TestProject_ClosedBlocksNewDocs: a Completed or Cancelled project refuses new
// and existing drafts with its own text, an approved order keeps showing it,
// and reopening it to On Hold saves with a warning, not an error.
func TestProject_ClosedBlocksNewDocs(t *testing.T) { // MST-102, CD-68
	for _, c := range []struct {
		status project.Status
		msg    string
	}{{project.Completed, msgCompleted}, {project.Cancelled, msgCancelled}} {
		t.Run(string(c.status), func(t *testing.T) {
			o := newOrders(t)
			ctx := context.Background()
			approved := o.draft(t)
			number := mstfixture.Approve(t, o.env, approved)
			draft := o.draft(t)

			o.setStatus(t, c.status)

			_, err := o.save(0)
			wantProjectRefused(t, "new draft", err, c.msg)
			_, err = o.save(draft)
			wantProjectRefused(t, "existing draft", err, c.msg)

			if err := o.env.DB.Read(ctx, func(tx *db.Tx) error {
				rec, err := o.env.Kit.Engine.Load(ctx, tx, mstfixture.DocType, approved)
				if err != nil {
					return err
				}
				if got, want := rec.Values["project_id"], strconv.FormatInt(o.m.Projects[prjCode], 10); got != want {
					t.Errorf("approved order project_id = %q; want %q", got, want)
				}
				if rec.Info.Number != number {
					t.Errorf("approved order number = %q; want %q", rec.Info.Number, number)
				}
				return nil
			}); err != nil {
				t.Fatalf("load approved order: %v", err)
			}

			o.setStatus(t, project.OnHold)
			res, err := o.save(draft)
			if err != nil {
				t.Fatalf("save on On Hold: %v", err)
			}
			if want := []string{msgOnHold}; !reflect.DeepEqual(res.Warnings, want) {
				t.Errorf("Warnings = %v; want %v", res.Warnings, want)
			}

			o.setStatus(t, project.Open)
			res, err = o.save(draft)
			if err != nil {
				t.Fatalf("save on Open: %v", err)
			}
			if len(res.Warnings) != 0 {
				t.Errorf("Warnings on Open = %v; want none", res.Warnings)
			}
		})
	}
}

// codes lists docType through list.Service as p and returns the codes, sorted.
func (o *orders) codes(t *testing.T, p auth.Principal, docType string) []string {
	t.Helper()
	dt, ok := doctype.Lookup(docType)
	if !ok {
		t.Fatalf("DocType %s is not registered", docType)
	}
	ls := list.Service{DB: o.env.DB, Auth: o.env.Kit.Auth}
	res, err := ls.List(context.Background(), p, dt, list.Query{Page: 1})
	if err != nil {
		t.Fatalf("list %s: %v", docType, err)
	}
	out := make([]string, len(res.Rows))
	for i, row := range res.Rows {
		out[i], _ = row.Values["code"].(string)
	}
	slices.Sort(out)
	return out
}

// TestProject_RestrictionAppliesToMaster: a user restricted on projects or
// sites lists only those records of the master itself; Kavya, who isn't
// restricted, sees them all.
func TestProject_RestrictionAppliesToMaster(t *testing.T) { // CD-64
	o := newOrders(t)
	suresh := testkit.Principal(t, "suresh@vardhan.test")
	for _, c := range []struct {
		name, docType string
		p             auth.Principal
		want          []string
	}{
		{"Priya projects", project.DocType, o.env.Priya, []string{"PRJ-METRO"}},
		{"Kavya projects", project.DocType, o.env.Kavya, []string{"PRJ-METRO", "PRJ-TECHPARK"}},
		{"Suresh sites", site.DocType, suresh, []string{"METRO-SITE", "PUNE-WH"}},
		{"Kavya sites", site.DocType, o.env.Kavya, []string{"METRO-SITE", "PUNE-WH", "TECH-SITE"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := o.codes(t, c.p, c.docType); !reflect.DeepEqual(got, c.want) {
				t.Errorf("listed %v; want %v", got, c.want)
			}
		})
	}
}
