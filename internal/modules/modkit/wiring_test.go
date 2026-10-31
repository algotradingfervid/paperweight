package modkit_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

type wiringDeps struct{ name string }

func getIn(t *testing.T, d *db.DB, w *modkit.Wiring[wiringDeps]) (wiringDeps, error) {
	t.Helper()
	var (
		got    wiringDeps
		getErr error
	)
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		got, getErr = w.Get(tx)
		return nil
	}); err != nil {
		t.Fatalf("Read error = %v", err)
	}
	return got, getErr
}

// CD-284: two worlds wired in parallel each see their own value.
func TestWiring_PerDatabase(t *testing.T) {
	var w modkit.Wiring[wiringDeps]
	dbs := []*db.DB{testkit.NewDB(t), testkit.NewDB(t)}
	var wg sync.WaitGroup
	for i, d := range dbs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Wire(d, wiringDeps{name: fmt.Sprintf("world-%d", i)})
		}()
	}
	wg.Wait()

	errs := make([]error, len(dbs))
	gots := make([]wiringDeps, len(dbs))
	for i, d := range dbs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = d.Read(context.Background(), func(tx *db.Tx) error {
				var err error
				gots[i], err = w.Get(tx)
				return err
			})
		}()
	}
	wg.Wait()
	for i := range dbs {
		want := wiringDeps{name: fmt.Sprintf("world-%d", i)}
		if errs[i] != nil || gots[i] != want {
			t.Errorf("db %d: Get = %+v, %v; want %+v, nil", i, gots[i], errs[i], want)
		}
	}
}

// CD-284: Get with nothing wired for the database is a plain internal error.
func TestWiring_GetBeforeWire(t *testing.T) {
	var w modkit.Wiring[wiringDeps]
	wired, unwired := testkit.NewDB(t), testkit.NewDB(t)
	w.Wire(wired, wiringDeps{name: "wired"})
	got, err := getIn(t, unwired, &w)
	if err == nil {
		t.Fatal("Get before Wire: error = nil, want an error")
	}
	if got != (wiringDeps{}) {
		t.Errorf("Get before Wire = %+v, want the zero value", got)
	}

	// A Tx with no database (hand-built) gets the error, never a panic.
	got, err = w.Get(&db.Tx{})
	if err == nil {
		t.Error("Get(&db.Tx{}): error = nil, want an error")
	}
	if got != (wiringDeps{}) {
		t.Errorf("Get(&db.Tx{}) = %+v, want the zero value", got)
	}
}

// CD-284: a second Wire for the same database replaces the value.
func TestWiring_Replace(t *testing.T) {
	var w modkit.Wiring[wiringDeps]
	d := testkit.NewDB(t)
	w.Wire(d, wiringDeps{name: "first"})
	w.Wire(d, wiringDeps{name: "second"})
	got, err := getIn(t, d, &w)
	if err != nil || got.name != "second" {
		t.Errorf("Get after replace = %+v, %v; want second, nil", got, err)
	}
}
