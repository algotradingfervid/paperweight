package allmasters

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var importedMasters = []string{
	party.DocType, address.DocType, address.ContactDocType, uom.DocType, taxcode.DocType,
	item.DocType, expensetype.DocType, project.DocType, site.DocType, employee.DocType,
}

func allImporters() map[string]rowFunc {
	all := partyImporters()
	for _, group := range []map[string]rowFunc{catalogueImporters(), orgImporters()} {
		for name, fn := range group {
			all[name] = fn
		}
	}
	return all
}

// A repeat call neither panics nor drops a DocType: csvio already holds each
// of the ten, so registering any of them again panics.
func TestRegisterImporters_AllTenAndRepeatable(t *testing.T) {
	RegisterImporters(&Services{})
	RegisterImporters(&Services{})
	if got := allImporters(); len(got) != len(importedMasters) {
		t.Errorf("%d importers, want %d", len(got), len(importedMasters))
	}
	for _, name := range importedMasters {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: csvio has no importer for it", name)
				}
			}()
			csvio.RegisterImporter(name, nil)
		}()
	}
}

// Routes makes its own Services the importers' (CD-210), so WP3 has nothing
// to call.
func TestRoutes_RegistersImporters(t *testing.T) {
	s := New(msttest.New(t).Kit)
	s.Routes(chi.NewRouter(), WebDeps{Sessions: scs.New()})
	if got := importServices.Load(); got != s {
		t.Errorf("importers use services %p; want %p", got, s)
	}
}

// Tasks 20 and 21 take a DocType off this list as its RowFunc is built.
func TestImporters_StubsSayNotImplemented(t *testing.T) {
	all := allImporters()
	for _, name := range importedMasters {
		_, _, err := all[name](nil, context.Background(), nil, auth.Principal{}, nil)
		if err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Errorf("%s: err = %v, want not implemented", name, err)
		}
	}
}

func TestWithServices_ReadsLatestServices(t *testing.T) {
	var got *Services
	fn := withServices(func(s *Services, _ context.Context, _ *db.Tx, _ auth.Principal, _ map[string]string) (int64, []doctype.FieldError, error) {
		got = s
		return 7, []doctype.FieldError{{Field: "code"}}, errors.New("boom")
	})
	first, second := &Services{}, &Services{}
	RegisterImporters(first)
	id, errs, err := fn(context.Background(), nil, auth.Principal{}, nil)
	if got != first || id != 7 || len(errs) != 1 || err == nil {
		t.Errorf("fn = %d, %v, %v; services %p, want %p", id, errs, err, got, first)
	}
	RegisterImporters(second)
	if _, _, _ = fn(context.Background(), nil, auth.Principal{}, nil); got != second {
		t.Errorf("services %p after a second RegisterImporters, want %p", got, second)
	}
}

func TestWithServices_PassesEveryArgument(t *testing.T) {
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, "import")
	tx := &db.Tx{}
	p := auth.Principal{ID: 41}
	row := map[string]string{"code": "SHREE"}
	called := false
	fn := withServices(func(_ *Services, gotCtx context.Context, gotTx *db.Tx, gotP auth.Principal, gotRow map[string]string) (int64, []doctype.FieldError, error) {
		called = true
		if gotCtx.Value(marker{}) != "import" {
			t.Error("ctx lost its value")
		}
		if gotTx != tx {
			t.Errorf("tx = %p, want %p", gotTx, tx)
		}
		if gotP.ID != p.ID {
			t.Errorf("principal ID = %d, want %d", gotP.ID, p.ID)
		}
		if !maps.Equal(gotRow, row) {
			t.Errorf("row = %q, want %q", gotRow, row)
		}
		return 0, nil, nil
	})
	if _, _, err := fn(ctx, tx, p, row); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("the rowFunc was not called")
	}
}

// Each master's own template maps back whole: every column to its import
// field, nothing shadowed by a field with the same label.
func TestMapHeader_TemplatesMapWhole(t *testing.T) {
	for _, name := range importedMasters {
		dt, ok := doctype.Lookup(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		body, ok := bytes.CutPrefix(csvio.Template(dt), []byte("\xef\xbb\xbf"))
		if !ok {
			t.Fatalf("%s: template does not start with the BOM", name)
		}
		header, err := csv.NewReader(bytes.NewReader(body)).Read()
		if err != nil {
			t.Fatalf("%s: template header: %v", name, err)
		}
		cols, warns, err := csvio.MapHeader(dt, header)
		if err != nil {
			t.Fatalf("%s: MapHeader: %v", name, err)
		}
		if want := len(dt.ImportFields()); len(cols) != want || len(warns) != 0 {
			t.Errorf("%s: %d columns, warnings %q; want %d, none", name, len(cols), warns, want)
		}
	}
}

func TestFieldErrors(t *testing.T) {
	want := []doctype.FieldError{{Field: "pan", Message: "PAN is wrong."}}
	got, err := fieldErrors(&mstkit.ValidationError{Fields: want})
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("validation error: %v, %v; want %v, nil", got, err, want)
	}
	boom := errors.New("boom")
	if got, err := fieldErrors(boom); got != nil || !errors.Is(err, boom) {
		t.Errorf("other error: %v, %v; want nil, boom", got, err)
	}
	if got, err := fieldErrors(nil); got != nil || err != nil {
		t.Errorf("nil: %v, %v; want nil, nil", got, err)
	}
}

func TestCellBool(t *testing.T) {
	for cell, want := range map[string]bool{
		"yes": true, "YES": true, "True": true, "1": true,
		"no": false, "false": false, "0": false, "": false,
	} {
		if got := cellBool(cell); got != want {
			t.Errorf("cellBool(%q) = %v, want %v", cell, got, want)
		}
	}
}

func TestCellOption(t *testing.T) {
	options := []string{"Warehouse", "Site"}
	for cell, want := range map[string]string{"warehouse": "Warehouse", "SITE": "Site", "Depot": "Depot", "": ""} {
		if got := cellOption(cell, options); got != want {
			t.Errorf("cellOption(%q) = %q, want %q", cell, got, want)
		}
	}
}

func TestCellInt64(t *testing.T) {
	for cell, want := range map[string]int64{"": 0, "42": 42} {
		if got, err := cellInt64(cell); err != nil || got != want {
			t.Errorf("cellInt64(%q) = %d, %v; want %d, nil", cell, got, err, want)
		}
	}
	if _, err := cellInt64("SHREE"); err == nil {
		t.Error(`cellInt64("SHREE") = nil error, want one`)
	}
}

func TestApplyDisabled(t *testing.T) {
	env := msttest.New(t)
	svc := New(env.Kit)
	ctx := context.Background()
	for _, c := range []struct {
		code, cell string
		want       bool
	}{{"KEEPON", "no", false}, {"BLANK", "", false}, {"TURNOFF", "Yes", true}} {
		var id int64
		err := env.DB.Write(ctx, func(tx *db.Tx) error {
			saved, err := svc.Party.CreateTx(ctx, tx, env.Kavya, party.Input{Code: c.code, Name: "Party " + c.code, IsVendor: true})
			if err != nil {
				return err
			}
			id = saved.ID
			return applyDisabled(ctx, tx, env.Kavya, saved, map[string]string{"disabled": c.cell}, svc.Party.SetDisabledTx)
		})
		if err != nil {
			t.Fatalf("%s: %v", c.code, err)
		}
		err = env.DB.Read(ctx, func(tx *db.Tx) error {
			p, err := svc.Party.Get(ctx, tx, id)
			if err == nil && p.Disabled != c.want {
				t.Errorf("%s: Disabled = %v, want %v", c.code, p.Disabled, c.want)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
