package print

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// View renders the body of one document's print page (spec §5).
type View = func(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error)

// ErrNoTemplate is returned by Render when a DocType has no registered view.
var ErrNoTemplate = errors.New("print: no template")

// Registry maps DocType names to their print views.
type Registry struct {
	mu    sync.RWMutex
	views map[string]View
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{views: map[string]View{}}
}

// Default is the registry Register adds to.
var Default = NewRegistry()

// Register adds view for doctype to Default.
func Register(doctype string, view View) { Default.Register(doctype, view) }

// Register adds view for doctype. It panics on an empty name, a nil view or a
// DocType that already has one.
func (r *Registry) Register(doctype string, view View) {
	if doctype == "" {
		panic("print: Register with empty DocType name")
	}
	if view == nil {
		panic("print: Register " + doctype + " with nil view")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.views[doctype]; dup {
		panic("print: Register called twice for " + doctype)
	}
	r.views[doctype] = view
}

// Lookup returns the view registered for doctype.
func (r *Registry) Lookup(doctype string) (View, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.views[doctype]
	return v, ok
}

// Missing reports every name in printable that has no view, or nil.
func (r *Registry) Missing(printable []string) error {
	var errs []error
	for _, name := range printable {
		if _, ok := r.Lookup(name); !ok {
			errs = append(errs, fmt.Errorf("No print template for DocType %s.", name)) //nolint:staticcheck // ST1005: user-facing sentence fixed by the plan (FILE-066)
		}
	}
	return errors.Join(errs...)
}

// PrintableNames returns the names of the printable DocTypes in dts, in order.
func PrintableNames(dts []*doctype.DocType) []string {
	var names []string
	for _, dt := range dts {
		if dt.Printable {
			names = append(names, dt.Name)
		}
	}
	return names
}
