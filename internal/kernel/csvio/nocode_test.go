package csvio

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-318: a Link to a master without a code column (Address) exports the
// master's display text, or the raw id when it has none, and only when the
// viewer may read the target (CD-233).

const (
	nameNCPlace  = "csviotest.NoCodePlace"
	nameNCVisit  = "csviotest.NoCodeVisit"
	ncPlaceTable = "numt_csvio_ncplace"
	ncVisitTable = "numt_csvio_ncvisit"
)

// ncLookup is a master lookup over a table with no code column.
type ncLookup struct{}

func (ncLookup) Lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	var mi doctype.MasterInfo
	err := tx.SQL().QueryRowContext(ctx, "SELECT name, disabled FROM "+ncPlaceTable+" WHERE id = ?", id).Scan(&mi.Name, &mi.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	return mi, err == nil, err
}

func init() {
	doctype.Register(doctype.DocType{
		Name: nameNCPlace, Label: "Place", PluralLabel: "Places", Table: ncPlaceTable, TestOnly: true,
		Fields: []doctype.Field{
			{Name: "name", Label: "Name", Type: doctype.Text},
			{Name: "disabled", Label: "Disabled", Type: doctype.Check},
		},
		Master: ncLookup{},
	})
	doctype.Register(doctype.DocType{
		Name: nameNCVisit, Label: "Visit", PluralLabel: "Visits", Table: ncVisitTable, TestOnly: true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text},
			{Name: "place_id", Label: "Place", Type: doctype.Link, Link: nameNCPlace},
		},
		DefaultSort: []doctype.SortKey{{Field: "code"}},
	})
}

func ncSetup(t *testing.T) *db.DB {
	t.Helper()
	d := listtest.Setup(t)
	ctx := t.Context()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		for _, q := range []string{
			"CREATE TABLE " + ncPlaceTable + " (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT '', disabled INTEGER NOT NULL DEFAULT 0)",
			"CREATE TABLE " + ncVisitTable + " (id INTEGER PRIMARY KEY, code TEXT, place_id INTEGER, created_at TEXT, created_by INTEGER, updated_at TEXT, updated_by INTEGER)",
		} {
			if _, err := tx.SQL().ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return d
}

func ncExport(t *testing.T, d *db.DB) string {
	t.Helper()
	dt, _ := doctype.Lookup(nameNCVisit)
	var buf bytes.Buffer
	if _, err := NewExporter(d, listtest.NewAuth()).Export(t.Context(), &buf, testkit.Principal(t, "admin@vardhan.test"), dt, list.Query{}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.String()
}

// useHiddenPlaces installs a link check that refuses the places in hidden
// with refusal, until the test ends.
func useHiddenPlaces(t *testing.T, refusal error, hidden ...int64) {
	t.Helper()
	old := linkCheck
	t.Cleanup(func() { linkCheck = old })
	SetLinkCheck(func(_ context.Context, _ *db.Tx, _ auth.Service, _ auth.Principal, target *doctype.DocType, id int64) error {
		for _, h := range hidden {
			if target.Name == nameNCPlace && id == h {
				return refusal
			}
		}
		return nil
	})
}

func TestExport_NoCodeMasterName(t *testing.T) {
	d := ncSetup(t)
	useHiddenPlaces(t, nil)
	listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Thane"})
	mumbai := listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Mumbai"})
	listtest.Insert(t, d, ncVisitTable, map[string]any{"code": "V1", "place_id": mumbai})
	if got, want := ncExport(t, d), string(utf8BOM)+"Code,Place\r\nV1,Mumbai\r\n"; got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
}

func TestExport_NoCodeMasterNoNameIsID(t *testing.T) {
	d := ncSetup(t)
	useHiddenPlaces(t, nil)
	listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Thane"})
	blank := listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": ""})
	listtest.Insert(t, d, ncVisitTable, map[string]any{"code": "V1", "place_id": blank})
	if got, want := ncExport(t, d), string(utf8BOM)+"Code,Place\r\nV1,2\r\n"; got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
}

// A place the link check refuses (no right, hidden row, or gone) is blank,
// and a place that doesn't exist is blank too.
func TestExport_NoCodeMasterHiddenBlank(t *testing.T) {
	for name, refusal := range map[string]error{"not found": auth.ErrNotFound, "no right": &auth.DeniedError{}, "link": &auth.LinkError{Label: "Place"}} {
		t.Run(name, func(t *testing.T) {
			d := ncSetup(t)
			shown := listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Mumbai"})
			hidden := listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Secret Yard"})
			useHiddenPlaces(t, refusal, hidden)
			listtest.Insert(t, d, ncVisitTable, map[string]any{"code": "V1", "place_id": shown})
			listtest.Insert(t, d, ncVisitTable, map[string]any{"code": "V2", "place_id": hidden})
			listtest.Insert(t, d, ncVisitTable, map[string]any{"code": "V3", "place_id": 999})
			got := ncExport(t, d)
			if want := string(utf8BOM) + "Code,Place\r\nV1,Mumbai\r\nV2,\r\nV3,\r\n"; got != want {
				t.Errorf("export = %q, want %q", got, want)
			}
			if strings.Contains(got, "Secret") || strings.Contains(got, ","+strconv.FormatInt(hidden, 10)+"\r") {
				t.Errorf("export names the hidden place: %q", got)
			}
		})
	}
}

// Any other link check error ends the export.
func TestExport_NoCodeMasterCheckError(t *testing.T) {
	d := ncSetup(t)
	place := listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Mumbai"})
	boom := errors.New("boom")
	useHiddenPlaces(t, boom, place)
	listtest.Insert(t, d, ncVisitTable, map[string]any{"code": "V1", "place_id": place})
	dt, _ := doctype.Lookup(nameNCVisit)
	_, err := NewExporter(d, listtest.NewAuth()).Export(t.Context(), &bytes.Buffer{}, testkit.Principal(t, "admin@vardhan.test"), dt, list.Query{})
	if !errors.Is(err, boom) {
		t.Errorf("Export = %v, want %v", err, boom)
	}
}

// One export asks the link check once per place, however many rows link to
// it.
func TestExport_NoCodeMasterCheckedOncePerPlace(t *testing.T) {
	d := ncSetup(t)
	mumbai := listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Mumbai"})
	thane := listtest.Insert(t, d, ncPlaceTable, map[string]any{"name": "Thane"})
	for i, place := range []int64{mumbai, thane, mumbai, mumbai, thane} {
		listtest.Insert(t, d, ncVisitTable, map[string]any{"code": "V" + strconv.Itoa(i+1), "place_id": place})
	}
	asked := map[int64]int{}
	old := linkCheck
	t.Cleanup(func() { linkCheck = old })
	SetLinkCheck(func(_ context.Context, _ *db.Tx, _ auth.Service, _ auth.Principal, _ *doctype.DocType, id int64) error {
		asked[id]++
		return nil
	})
	want := string(utf8BOM) + "Code,Place\r\nV1,Mumbai\r\nV2,Thane\r\nV3,Mumbai\r\nV4,Mumbai\r\nV5,Thane\r\n"
	if got := ncExport(t, d); got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
	if asked[mumbai] != 1 || asked[thane] != 1 {
		t.Errorf("link checks = %v, want one per place", asked)
	}
}
