// Package docfake is an in-memory doc.Engine (and doctype.InfoSource) for tests.
package docfake

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

var (
	_ doc.Engine         = (*Fake)(nil)
	_ doctype.InfoSource = (*Fake)(nil)
)

// Call is one recorded Do call.
type Call struct {
	DocType string
	ID      int64
	Action  doctype.Action
	Input   doc.ActionInput
	By      auth.UserID
}

type docKey struct {
	doctype string
	id      int64
}

type rowKey struct {
	table string
	id    int64
}

type resultKey struct {
	docKey
	action doctype.Action
}

type outcome struct {
	result doc.Result
	err    error
}

type Fake struct {
	mu       sync.Mutex
	infos    map[docKey]doctype.Info
	records  map[docKey]doc.Record
	problems map[docKey][]doctype.FieldError
	refs     map[rowKey][]doc.Reference
	results  map[resultKey]outcome
	calls    []Call
}

func New() *Fake {
	return &Fake{
		infos:    map[docKey]doctype.Info{},
		records:  map[docKey]doc.Record{},
		problems: map[docKey][]doctype.FieldError{},
		refs:     map[rowKey][]doc.Reference{},
		results:  map[resultKey]outcome{},
	}
}

func (f *Fake) SetInfo(info doctype.Info) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infos[docKey{info.DocType, info.ID}] = copyInfo(info)
}

// SetState updates the stored Info's State.
func (f *Fake) SetState(dt string, id int64, s doctype.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := docKey{dt, id}
	info, ok := f.infos[k]
	if !ok {
		info = doctype.Info{DocType: dt, ID: id}
	}
	info.State = s
	f.infos[k] = info
}

func (f *Fake) AddReference(table string, id int64, ref doc.Reference) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := rowKey{table, id}
	f.refs[k] = append(f.refs[k], ref)
}

// SetReferenced adds a reference whose Display is firstRef.
func (f *Fake) SetReferenced(table string, id int64, firstRef string) {
	f.AddReference(table, id, doc.Reference{Display: firstRef})
}

// SetResult sets what Do returns for (doctype, id, action) (CD-86).
func (f *Fake) SetResult(dt string, id int64, a doctype.Action, r doc.Result, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[resultKey{docKey{dt, id}, a}] = outcome{r, err}
}

// SetRecord stores r for Load and r.Info as with SetInfo.
func (f *Fake) SetRecord(r doc.Record) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := docKey{r.Info.DocType, r.Info.ID}
	f.records[k] = copyRecord(r)
	f.infos[k] = copyInfo(r.Info)
}

// SetProblems sets what Problems returns for draft (dt, id) (CD-205).
func (f *Fake) SetProblems(dt string, id int64, problems []doctype.FieldError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.problems[docKey{dt, id}] = slices.Clone(problems)
}

// Calls returns the recorded Do calls in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.calls)
	for i := range out {
		out[i].Input = copyInput(out[i].Input)
	}
	return out
}

func copyInfo(info doctype.Info) doctype.Info {
	info.ProjectIDs = slices.Clone(info.ProjectIDs)
	info.SiteIDs = slices.Clone(info.SiteIDs)
	return info
}

func copyLines(lines []map[string]string) []map[string]string {
	if lines == nil {
		return nil
	}
	out := make([]map[string]string, len(lines))
	for i, l := range lines {
		out[i] = maps.Clone(l)
	}
	return out
}

func copyInput(in doc.ActionInput) doc.ActionInput {
	in.Values = maps.Clone(in.Values)
	in.Lines = copyLines(in.Lines)
	in.Only = slices.Clone(in.Only)
	return in
}

func copyRecord(r doc.Record) doc.Record {
	r.Info = copyInfo(r.Info)
	r.Values = maps.Clone(r.Values)
	r.Lines = copyLines(r.Lines)
	return r
}

// Do records the call and returns the SetResult value, otherwise errs.ErrNotImplemented.
func (f *Fake) Do(ctx context.Context, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{DocType: dt, ID: id, Action: a, Input: copyInput(in), By: p.ID})
	o, ok := f.results[resultKey{docKey{dt, id}, a}]
	if !ok {
		return doc.Result{}, errs.ErrNotImplemented
	}
	o.result.Warnings = slices.Clone(o.result.Warnings)
	o.result.Problems = slices.Clone(o.result.Problems)
	return o.result, o.err
}

// IsReferenced returns the first reference's Display.
func (f *Fake) IsReferenced(ctx context.Context, tx *db.Tx, table string, id int64) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	refs := f.refs[rowKey{table, id}]
	if len(refs) == 0 {
		return "", false, nil
	}
	return refs[0].Display, true, nil
}

// References returns at most limit references in insertion order.
func (f *Fake) References(ctx context.Context, tx *db.Tx, table string, id int64, limit int) ([]doc.Reference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	refs := f.refs[rowKey{table, id}]
	return slices.Clone(refs[:max(0, min(limit, len(refs)))]), nil
}

// Load returns a copy of the stored Record with the latest Info; a missing
// one is the engine's doc.Gone error.
func (f *Fake) Load(ctx context.Context, tx *db.Tx, dt string, id int64) (doc.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := docKey{dt, id}
	r, ok := f.records[k]
	if !ok {
		return doc.Record{}, doc.Gone()
	}
	r = copyRecord(r)
	r.Info = copyInfo(f.infos[k])
	return r, nil
}

// Problems returns the SetProblems value of a stored draft and computes
// nothing (CD-186): none past draft, as the engine, and a missing record is
// the engine's doc.Gone error.
func (f *Fake) Problems(ctx context.Context, tx *db.Tx, dt string, id int64) ([]doctype.FieldError, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := docKey{dt, id}
	if _, ok := f.records[k]; !ok {
		return nil, doc.Gone()
	}
	if f.infos[k].State != doctype.StateDraft {
		return nil, nil
	}
	return slices.Clone(f.problems[k]), nil
}

// Info returns the stored Info; a missing one is the engine's doc.Gone error.
func (f *Fake) Info(ctx context.Context, tx *db.Tx, dt string, id int64) (doctype.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.infos[docKey{dt, id}]
	if !ok {
		return doctype.Info{}, doc.Gone()
	}
	return copyInfo(info), nil
}
