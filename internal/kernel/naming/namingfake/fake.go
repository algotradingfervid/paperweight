// Package namingfake is an in-memory naming.Service and naming.Admin for tests.
package namingfake

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

var (
	_ naming.Service = (*Fake)(nil)
	_ naming.Admin   = (*Fake)(nil)
)

// Call is one recorded Service call; Method is the Go method name.
type Call struct {
	Method  string
	DocType string
	DocID   int64
	Result  string
}

// Fake numbers every doctype "TEST/00001", "TEST/00002", …
type Fake struct {
	mu       sync.Mutex
	last     map[string]int64
	rejected map[string]error
	previews map[string]string
	series   map[string]naming.SeriesInfo
	calls    []Call
}

func New() *Fake {
	return &Fake{
		last:     map[string]int64{},
		rejected: map[string]error{},
		previews: map[string]string{},
		series:   map[string]naming.SeriesInfo{},
	}
}

// RejectTemplate makes ValidateTemplate, PreviewTemplate and SetTemplate return err for tpl.
func (f *Fake) RejectTemplate(tpl string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected[tpl] = err
}

// SetPreview sets what PreviewTemplate returns for tpl.
func (f *Fake) SetPreview(tpl, out string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews[tpl] = out
}

// Calls returns the recorded Service calls in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func number(n int64) string { return fmt.Sprintf("TEST/%05d", n) }

func (f *Fake) Assign(ctx context.Context, tx *db.Tx, doctype string, docID int64, d types.Date) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[doctype]++
	out := number(f.last[doctype])
	f.calls = append(f.calls, Call{Method: "Assign", DocType: doctype, DocID: docID, Result: out})
	return out, nil
}

func (f *Fake) AssignAmendment(ctx context.Context, tx *db.Tx, doctype string, docID int64, root string, n int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := root + "-" + strconv.Itoa(n)
	f.calls = append(f.calls, Call{Method: "AssignAmendment", DocType: doctype, DocID: docID, Result: out})
	return out, nil
}

// Preview returns the next number without consuming it.
func (f *Fake) Preview(ctx context.Context, doctype string, d types.Date) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := number(f.last[doctype] + 1)
	f.calls = append(f.calls, Call{Method: "Preview", DocType: doctype, Result: out})
	return out, nil
}

func (f *Fake) ValidateTemplate(tpl string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rejected[tpl]
}

// List returns the stored series sorted by DocType.
func (f *Fake) List(ctx context.Context) ([]naming.SeriesInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]naming.SeriesInfo, 0, len(f.series))
	for _, dt := range slices.Sorted(maps.Keys(f.series)) {
		s := f.series[dt]
		s.Counters = maps.Clone(s.Counters)
		out = append(out, s)
	}
	return out, nil
}

func (f *Fake) SetTemplate(ctx context.Context, p auth.Principal, doctype, tpl string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.rejected[tpl]; err != nil {
		return err
	}
	s := f.seriesLocked(doctype)
	s.Template = tpl
	f.series[doctype] = s
	return nil
}

func (f *Fake) SetCounter(ctx context.Context, p auth.Principal, doctype, prefix string, last int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.seriesLocked(doctype)
	s.Counters[prefix] = last
	f.series[doctype] = s
	return nil
}

// EnsureSeries creates the series if it doesn't exist; an existing one is kept.
func (f *Fake) EnsureSeries(ctx context.Context, doctype, label, defaultTpl string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.series[doctype]; !ok {
		f.series[doctype] = naming.SeriesInfo{DocType: doctype, Label: label, Template: defaultTpl, Counters: map[string]int64{}}
	}
	return nil
}

func (f *Fake) seriesLocked(doctype string) naming.SeriesInfo {
	s, ok := f.series[doctype]
	if !ok {
		s = naming.SeriesInfo{DocType: doctype}
	}
	if s.Counters == nil {
		s.Counters = map[string]int64{}
	}
	return s
}

// PreviewTemplate returns the RejectTemplate error, else the SetPreview value, else tpl unchanged (CD-57).
func (f *Fake) PreviewTemplate(ctx context.Context, tpl string, d types.Date) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.rejected[tpl]; err != nil {
		return "", err
	}
	if out, ok := f.previews[tpl]; ok {
		return out, nil
	}
	return tpl, nil
}
